import Foundation
import Observation
import SwiftUI
import TogoKit
import TogoTerminal

enum DiffMode: String, CaseIterable, Sendable {
    case unified, split
}

/// One diff on screen: a file's changes, or every file a commit touched. Loads, parses and
/// tokenizes away from the main actor, steps between hunks and files, and stages, unstages or
/// discards the file shown.
///
/// Writes carry what the user saw. The status is read before the diff, so a file that changes
/// while the diff loads is refused by the helper as stale instead of being acted on unseen.
@MainActor @Observable final class DiffModel {
    enum State: Equatable {
        case loading, ready, binary, empty, discarded
        case failed(String)
    }

    enum Action: Hashable, Sendable {
        case stage, unstage, discard
    }

    /// A sentence about the last write, with "Try again" when trying again can work.
    struct Message: Equatable {
        var text: String
        var retry: Bool
    }

    /// The question asked before a discard, naming the file and what happens to it.
    struct Confirmation: Equatable, Identifiable {
        var path: String
        var title: String
        var message: String
        var button: String
        /// What the question named, as the user saw it: the discard sends exactly this.
        let selection: ReviewSelection
        var id: String { path }
    }

    struct CommentDraft: Equatable, Identifiable {
        var row: Int
        var quote: String
        /// `path:line`, as it is sent.
        var location: String
        var id: Int { row }
    }

    /// What a discard does to the file, from its status letters.
    private enum DiscardKind: Equatable {
        case restore, delete
        case undoRename(from: String)
    }

    private enum Phase: Equatable {
        case loading, loaded, discarded
        case failed(String)
    }

    let subject: ReviewSubject
    private let services: any ReviewServices
    private let sendToSession: ((String) -> Void)?
    /// The machine's name, for refusals that send the user to it.
    private let machine: String

    private(set) var source: DiffSource
    var mode: DiffMode = .split
    var wrap = false
    /// Whether the screen draws two columns. The hunk at the top stays at the top.
    var showsSplit = false {
        didSet { if showsSplit != oldValue { keepHunkAcrossLayouts(wasSplit: oldValue) } }
    }
    private(set) var files: [PreparedDiffFile] = []
    private(set) var fileIndex = 0
    private(set) var scrollTarget: Int?
    private(set) var message: Message?
    /// Why nothing can be changed any more: the session or machine is gone.
    var ended: String?
    private(set) var pendingDiscard: Confirmation?
    private(set) var busy = false
    /// Counts writes that went through, so the review can refresh.
    private(set) var completedWrites = 0
    /// Bumped on a stage or unstage that went through, for the done haptic.
    private(set) var successes = 0
    /// Bumped on a discard that went through, for the warning haptic.
    private(set) var discards = 0
    private(set) var canGoToNextHunk = false
    private(set) var canGoToPreviousHunk = false

    private var phase = Phase.loading
    private var status: GitStatus?
    /// The row at the top of the visible area. Scrolling reports it often, so it is not observed.
    @ObservationIgnored private var topRow = 0
    private var discardKind = DiscardKind.restore
    private var lastAction: Action?
    private var generation = 0
    private var started = false

    init(
        subject: ReviewSubject, services: any ReviewServices, source: DiffSource, sendToSession: ((String) -> Void)?,
        machine: String = ""
    ) {
        self.subject = subject
        self.services = services
        self.source = source
        self.sendToSession = sendToSession
        self.machine = machine
    }

    /// Two columns need a regular-width window at least 700 points wide: iPad, or an unfolded Duo.
    nonisolated static func splitAvailable(horizontalSizeClass: UserInterfaceSizeClass?, width: CGFloat) -> Bool {
        horizontalSizeClass == .regular && width >= 700
    }

    // MARK: What is shown

    var state: State {
        switch phase {
        case .loading: return .loading
        case .failed(let message): return .failed(message)
        case .discarded: return .discarded
        case .loaded:
            guard let current else { return .empty }
            if current.file.isBinary { return .binary }
            return current.isEmpty ? .empty : .ready
        }
    }

    private var current: PreparedDiffFile? {
        files.indices.contains(fileIndex) ? files[fileIndex] : nil
    }

    func rows(split: Bool) -> [CodeRow] {
        guard let current else { return [] }
        return split ? current.split : current.unified
    }

    /// The path the source names, which writes act on.
    private var sourcePath: String? {
        switch source {
        case .worktree(let path), .staged(let path), .untracked(let path): path
        case .commit(_, let path): path
        }
    }

    /// The file shown, relative to the repository root.
    var path: String {
        current.map { $0.file.newPath ?? $0.file.oldPath ?? "" } ?? sourcePath ?? ""
    }

    var fileName: String { path.split(separator: "/").last.map(String.init) ?? path }

    var folder: String {
        guard let slash = path.lastIndex(of: "/") else { return "" }
        return String(path[..<slash])
    }

    /// The names as the screen shows them, hidden characters marked.
    var displayName: String { DisplayText.clean(fileName) }
    var displayFolder: String { DisplayText.clean(folder) }
    var displayPath: String { DisplayText.clean(path) }

    var fileNames: [String] { files.map { DisplayText.clean($0.file.id) } }

    /// Where "Open file" goes for a binary image; nil when there is nothing to open.
    var openRoute: ReviewRoute? {
        guard state == .binary, let file = current?.file, file.change != .deleted else { return nil }
        let name = fileName.lowercased()
        guard let dot = name.lastIndex(of: "."), Self.imageExtensions.contains(String(name[name.index(after: dot)...])) else {
            return nil
        }
        if case .commit(let hash, _) = source { return .file(path: path, ref: hash, base: .repository) }
        return .file(path: path, ref: nil, base: .repository)
    }

    private static let imageExtensions: Set<String> = [
        "png", "jpg", "jpeg", "gif", "heic", "heif", "webp", "bmp", "tif", "tiff", "ico",
    ]

    // MARK: Loading

    /// Loads once; later appearances keep what is on screen.
    func loadIfNeeded() async {
        guard !started else { return }
        await load()
    }

    func load() async {
        await load(status: nil)
    }

    private func load(status known: GitStatus?) async {
        started = true
        generation += 1
        let generation = generation
        phase = .loading
        do {
            var status = known
            // The status comes first: a file edited after this read is refused as stale.
            if status == nil, subject.canWrite, writable {
                status = try? await services.status(subject)
            }
            let prepared = try await fetch()
            guard generation == self.generation else { return }
            self.status = status
            files = prepared
            fileIndex = min(fileIndex, max(prepared.count - 1, 0))
            phase = .loaded
            resetHunks()
        } catch {
            guard generation == self.generation else { return }
            phase = .failed(error.localizedDescription)
        }
    }

    private func fetch() async throws -> [PreparedDiffFile] {
        switch source {
        case .untracked(let path):
            let result = try await services.read(subject, path: path, ref: nil, base: .repository)
            return await Task.detached(priority: .userInitiated) {
                DiffPreparation.prepareAdded(path: path, result)
            }.value
        case .worktree, .staged, .commit:
            let diff = try await services.diff(subject, source)
            return await Task.detached(priority: .userInitiated) { DiffPreparation.prepare(diff) }.value
        }
    }

    // MARK: Files and hunks

    func selectFile(_ index: Int) {
        guard files.indices.contains(index), index != fileIndex else { return }
        fileIndex = index
        resetHunks()
    }

    private var hunks: [Int] { hunks(split: showsSplit) }

    private func hunks(split: Bool) -> [Int] {
        guard let current else { return [] }
        return split ? current.splitHunks : current.unifiedHunks
    }

    /// The user scrolled; the next and previous changes are counted from here.
    func scrolled(toTopRow row: Int) {
        topRow = row
        // The view is no longer where the last jump put it, so the same jump can be asked again.
        if scrollTarget != nil { scrollTarget = nil }
        refreshHunkButtons()
    }

    /// The first change below the top of the view.
    func nextHunk() {
        guard state == .ready, let target = hunks.first(where: { $0 > topRow }) else { return }
        jump(to: target)
    }

    /// The last change above the top of the view.
    func previousHunk() {
        guard state == .ready, let target = hunks.last(where: { $0 < topRow }) else { return }
        jump(to: target)
    }

    private func jump(to row: Int) {
        scrollTarget = row
        topRow = row
        refreshHunkButtons()
    }

    private func keepHunkAcrossLayouts(wasSplit: Bool) {
        let old = hunks(split: wasSplit)
        let new = hunks(split: showsSplit)
        guard let index = old.lastIndex(where: { $0 <= topRow }), new.indices.contains(index) else {
            topRow = 0
            refreshHunkButtons()
            return
        }
        topRow = new[index]
        if scrollTarget != nil { scrollTarget = topRow }
        refreshHunkButtons()
    }

    private func resetHunks() {
        topRow = 0
        scrollTarget = nil
        refreshHunkButtons()
    }

    private func refreshHunkButtons() {
        let hunks = state == .ready ? hunks : []
        let next = hunks.contains { $0 > topRow }
        let previous = hunks.contains { $0 < topRow }
        if next != canGoToNextHunk { canGoToNextHunk = next }
        if previous != canGoToPreviousHunk { canGoToPreviousHunk = previous }
    }

    // MARK: Comments

    var canSendComments: Bool { sendToSession != nil }

    var commentActionTitle: String { canSendComments ? DiffCopy.sendToSession : DiffCopy.openSessionToSend }

    /// The line under a long press, or nil for headers and notices.
    func commentDraft(row: Int) -> CommentDraft? {
        let rows = rows(split: showsSplit)
        guard rows.indices.contains(row) else { return nil }
        let text: String
        let number: Int
        switch rows[row] {
        case let .line(new, old, line, kind, tokens, _, _):
            guard kind != .noNewline, let shown = new ?? old else { return nil }
            (text, number) = (HiddenCharacters.raw(line, tokens: tokens), shown)
        case let .split(left, right):
            guard let side = [right, left].compactMap({ $0 }).first(where: { $0.number != nil }),
                  side.kind != .noNewline, let shown = side.number else { return nil }
            (text, number) = (HiddenCharacters.raw(side.text, tokens: side.tokens), shown)
        case .hunk, .notice:
            return nil
        }
        return CommentDraft(row: row, quote: text, location: "\(path):\(number)")
    }

    /// Sends "path:line: text" to the session. Both the path and the text come from outside the
    /// app, so each is cleaned for a terminal and kept on one line.
    func comment(row: Int, text: String) {
        guard let draft = commentDraft(row: row) else { return }
        comment(draft, text: text)
    }

    /// Comments on the line the draft quotes, even if the diff has reloaded since.
    func comment(_ draft: CommentDraft, text: String) {
        let flat = SessionText.clean(text)
        guard let sendToSession, !flat.isEmpty else { return }
        sendToSession("\(SessionText.clean(draft.location)): \(flat)")
    }

    // MARK: Writes

    private var writable: Bool {
        switch source {
        case .worktree, .staged, .untracked: true
        case .commit: false
        }
    }

    /// What can be done to the file shown, in toolbar order.
    var actions: [Action] {
        guard subject.canWrite, ended == nil, phase == .loaded, let status, let path = sourcePath,
              let entry = status.files.first(where: { $0.path == path }) else { return [] }
        switch source {
        case .worktree: return entry.worktree == "." || entry.worktree == "?" ? [] : [.stage, .discard]
        case .untracked: return entry.worktree == "?" ? [.stage, .discard] : []
        case .staged: return entry.index == "." || entry.index == "?" ? [] : [.unstage]
        case .commit: return []
        }
    }

    /// Stages or unstages now; a discard asks first through `pendingDiscard`.
    func perform(_ action: Action) async {
        guard actions.contains(action), !busy else { return }
        if action == .discard {
            pendingDiscard = confirmation()
            return
        }
        await write(action)
    }

    func confirmDiscard() async {
        guard let pendingDiscard else { return }
        await confirm(pendingDiscard)
    }

    /// Discards the file the confirmation names, if it is still the one shown.
    func confirm(_ confirmation: Confirmation) async {
        pendingDiscard = nil
        guard confirmation.path == sourcePath, actions.contains(.discard), !busy else { return }
        await write(.discard, confirmed: confirmation.selection)
    }

    func cancelDiscard() {
        pendingDiscard = nil
    }

    /// Runs the last write again. A discard asks again first.
    func retry() async {
        guard let lastAction, message?.retry == true else { return }
        message = nil
        if lastAction == .discard {
            pendingDiscard = confirmation()
        } else {
            await write(lastAction)
        }
    }

    func dismissMessage() {
        message = nil
    }

    /// What a discard of the file shown does, by the same rules as the changes list: a file the
    /// last commit lacks is deleted, a staged rename is undone, anything else goes back to the
    /// last commit.
    private var kindOfDiscard: DiscardKind {
        if case .untracked = source { return .delete }
        guard let path = sourcePath, let entry = status?.files.first(where: { $0.path == path }) else { return .restore }
        if let old = ChangesCopy.renamedFrom(entry) { return .undoRename(from: old) }
        if ChangesCopy.neverCommitted(entry) { return .delete }
        return .restore
    }

    /// The menu's name for a discard of the file shown, as the changes list names it.
    var discardActionTitle: String {
        switch kindOfDiscard {
        case .restore: ChangesCopy.discard
        case .delete: ChangesCopy.delete
        case .undoRename: ChangesCopy.undoRename
        }
    }

    /// The question names the file and says what happens to it, in the changes list's words.
    private func confirmation() -> Confirmation? {
        guard let path = sourcePath, let status, let selection = try? ReviewSelection(status, paths: [path]) else {
            return nil
        }
        discardKind = kindOfDiscard
        let shared = ChangesCopy.confirmation(selection, status: status, folder: nil)
        return Confirmation(path: path, title: shared.title, message: shared.message, button: shared.button, selection: selection)
    }

    /// A discard acts on the `confirmed` selection; the other writes on the status shown.
    private func write(_ action: Action, confirmed: ReviewSelection? = nil) async {
        guard let status, let path = sourcePath else { return }
        lastAction = action
        message = nil
        busy = true
        defer { busy = false }
        do {
            let selection = try confirmed ?? ReviewSelection(status, paths: [path])
            let fresh: GitStatus
            switch action {
            case .stage: fresh = try await services.stage(subject, paths: selection.paths, seen: selection.seen)
            case .unstage: fresh = try await services.unstage(subject, paths: selection.paths, seen: selection.seen)
            case .discard: fresh = try await services.discard(subject, paths: selection.paths, seen: selection.seen)
            }
            completedWrites += 1
            if action == .discard { discards += 1 } else { successes += 1 }
            self.status = fresh
            switch action {
            case .stage:
                source = .staged(path: path)
            case .unstage:
                let untracked = fresh.files.first { $0.path == path }?.worktree == "?"
                source = untracked ? .untracked(path: path) : .worktree(path: path)
            case .discard:
                files = []
                resetHunks()
                phase = .discarded
                return
            }
            await load(status: fresh)
        } catch {
            message = message(for: error)
            if Self.refreshes(error) { await load() }
        }
    }

    private func message(for error: any Error) -> Message {
        let conflict = (error as? HelperError)?.gitConflict
        let text = conflict == .caseRename ? ChangesCopy.refusal(error, machine: machine) : error.localizedDescription
        return Message(text: text, retry: conflict == .locked)
    }

    /// Errors after which what is on screen may be out of date.
    private static func refreshes(_ error: any Error) -> Bool {
        if error is ReviewSelection.Failure { return true }
        switch (error as? HelperError)?.gitConflict {
        case .stale, .partial: return true
        default: return false
        }
    }

    // MARK: Copy for the screen

    var emptyTitle: String {
        guard state == .discarded else { return DiffCopy.noChanges }
        switch discardKind {
        case .restore: return DiffCopy.discarded
        case .delete: return DiffCopy.deleted
        case .undoRename: return DiffCopy.renameUndone
        }
    }

    var emptyMessage: String {
        guard state == .discarded else { return DiffCopy.noChangesMessage }
        switch discardKind {
        case .restore: return DiffCopy.discardedMessage(displayPath)
        case .delete: return DiffCopy.deletedMessage(displayPath)
        case .undoRename(let old): return DiffCopy.renameUndoneMessage(DisplayText.clean(old))
        }
    }
}
