import Foundation
import Observation
import TogoKit

/// One group of rows in the changes list.
struct ChangeSection: Identifiable, Hashable {
    enum Kind: String, CaseIterable, Hashable, Sendable {
        case staged, changed, untracked, conflicted
    }

    var kind: Kind
    var rows: [ChangeRow]

    var id: Kind { kind }
    var title: String { ChangesCopy.sectionTitle(kind) }
}

/// One file, or one folder of untracked files, as the list shows it.
struct ChangeRow: Identifiable, Hashable {
    enum Change: Hashable {
        case modified, added, deleted, typeChanged, untracked, conflict
        case renamed(from: String)
        case copied(from: String)
        case untrackedFolder(Int)
    }

    var section: ChangeSection.Kind
    /// The file's path, or the folder's with a trailing `/`.
    var path: String
    /// Every file a write on this row acts on.
    var paths: [String]
    var change: Change

    var id: String { "\(section.rawValue):\(path)" }
    var isFolder: Bool { path.hasSuffix("/") }

    var name: String {
        let trimmed = isFolder ? String(path.dropLast()) : path
        let last = (trimmed as NSString).lastPathComponent
        return isFolder ? last + "/" : last
    }

    /// The containing folder, empty at the top of the repository.
    var folder: String {
        let trimmed = isFolder ? String(path.dropLast()) : path
        return (trimmed as NSString).deletingLastPathComponent
    }

    /// The name and folder as the list shows them, hidden characters marked.
    var displayName: String { DisplayText.clean(name) }
    var displayFolder: String { DisplayText.clean(folder) }

    var words: String { ChangesCopy.words(change) }

    var accessibilityLabel: String {
        ChangesCopy.rowLabel(name: displayName, folder: displayFolder, words: words, section: section)
    }

    var route: ReviewRoute {
        switch section {
        case .staged: .diff(.staged(path: path))
        case .changed, .conflicted: .diff(.worktree(path: path))
        case .untracked:
            isFolder ? .folder(path: String(path.dropLast()), ref: nil, base: .repository) : .diff(.untracked(path: path))
        }
    }
}

/// The changes in one session's repository and the writes the user makes on them. Every write
/// names the files the list showed, with what they looked like, so a file the agent touched in
/// between is refused and the list is read again.
@MainActor @Observable final class ChangesModel {
    enum Phase: Equatable {
        case loading, loaded, notRepo
        case failed(String)
    }

    /// A sentence over the list about the last thing that went wrong.
    struct Notice: Equatable {
        var message: String
        var canRetry: Bool
    }

    let subject: ReviewSubject
    /// The machine's name, as the user knows it.
    let machine: String
    private let services: any ReviewServices

    private(set) var phase: Phase = .loading
    private(set) var status: GitStatus?
    private(set) var sections: [ChangeSection] = []
    private(set) var upstream: String?
    /// The commit an amend replaces: the latest one the user could have seen.
    private(set) var lastCommit: GitCommit?
    private(set) var notice: Notice?
    var pendingConfirmation: Confirmation?
    /// A write or commit is in flight. Counted, so one that ends does not clear another's.
    var isWorking: Bool { working > 0 }
    private var working = 0
    /// Bumped on a stage, unstage or commit that went through, for the success haptic.
    private(set) var successes = 0
    /// Bumped when the user confirms a discard, for the warning haptic.
    private(set) var discards = 0
    private var retryAction: (() async -> Void)?

    var isCommitSheetOpen = false
    var amend = false
    var message = ""
    private(set) var commitError: String?

    init(subject: ReviewSubject, services: any ReviewServices, machine: String) {
        self.subject = subject
        self.services = services
        self.machine = machine
    }

    /// The machine refused a write because Togo does not manage the session.
    private(set) var refusedUnmanaged = false
    /// Why the review can no longer change anything: its session or machine is gone.
    var ended: String?

    var canWrite: Bool { subject.canWrite && !refusedUnmanaged && ended == nil }

    /// The commit sheet has a message the user typed.
    var hasDraft: Bool { !trimmed(message).isEmpty }

    /// Why the actions are missing, shown once over the list.
    var readOnlyNote: String? {
        if let ended { return ReviewEntryCopy.ended(ended) }
        guard !canWrite else { return nil }
        return subject.managed && !refusedUnmanaged ? ChangesCopy.helperTooOld(machine) : ChangesCopy.readOnly
    }

    /// "Stage All" and "Unstage All" only act on a list that shows every change.
    var canActOnAll: Bool { canWrite && status?.isTruncated == false }

    var truncationNote: String? { status?.isTruncated == true ? ChangesCopy.truncated : nil }

    var summary: String {
        let files = status?.files ?? []
        return ChangesCopy.summary(files: files.count, staged: files.filter(Self.isStaged).count)
    }

    var branch: String? {
        guard let status, status.repo else { return nil }
        return status.branch.map(DisplayText.clean)
    }

    var branchLine: String? {
        guard let status, status.repo else { return nil }
        guard status.branch != nil else { return ChangesCopy.detached }
        return ChangesCopy.branchLine(ahead: status.ahead, behind: status.behind, upstream: upstream)
    }

    var isEmpty: Bool { status?.files.isEmpty ?? true }

    // MARK: Loading

    /// Reads the status again. A list already on screen stays up while it loads, and a notice
    /// already shown stays: it explains why the list changed.
    func refresh() async {
        let next: GitStatus
        do {
            next = try await services.status(subject)
        } catch {
            // A screen that went away stopped the read; nothing went wrong.
            if Task.isCancelled || error is CancellationError { return }
            if status == nil {
                phase = .failed(ServiceErrors.message(error))
            } else {
                notice = Notice(message: ServiceErrors.message(error), canRetry: false)
            }
            return
        }
        show(next)
        guard next.repo else { return }
        if let branches = try? await services.branches(subject) {
            upstream = branches.branches.first { $0.current }?.upstream
        }
        if let log = try? await services.log(subject, ref: nil, limit: 1) {
            lastCommit = log.first
        }
    }

    private func show(_ next: GitStatus) {
        status = next
        phase = next.repo ? .loaded : .notRepo
        sections = Self.sections(for: next)
    }

    // MARK: Writes

    func stage(_ paths: [String]) async {
        await write(paths, retry: { [weak self] in await self?.stage(paths) }) { s, selection in
            try await self.services.stage(s, paths: selection.paths, seen: selection.seen)
        }
    }

    func unstage(_ paths: [String]) async {
        await write(paths, retry: { [weak self] in await self?.unstage(paths) }) { s, selection in
            try await self.services.unstage(s, paths: selection.paths, seen: selection.seen)
        }
    }

    /// Stages every file in `kind`'s section, or unstages every staged file. Refused while the
    /// list leaves files out.
    func stageAll(_ kind: ChangeSection.Kind) async {
        guard canWrite, let status else { return }
        let selection: ReviewSelection
        do {
            selection = try ReviewSelection.all(in: status) { Self.kind(of: $0, isIn: kind) }
        } catch {
            notice = Notice(message: ServiceErrors.message(error), canRetry: false)
            return
        }
        if kind == .staged {
            await unstage(selection.paths)
        } else {
            await stage(selection.paths)
        }
    }

    /// Builds the question for discarding `paths`. Nothing is sent until `confirm()`.
    func requestDiscard(_ paths: [String]) {
        guard canWrite, let status else { return }
        let selection: ReviewSelection
        do {
            selection = try ReviewSelection(status, paths: paths)
        } catch {
            notice = Notice(message: ServiceErrors.message(error), canRetry: false)
            if case .notShown? = error as? ReviewSelection.Failure { Task { await refresh() } }
            return
        }
        let folder = sections.first { $0.kind == .untracked }?.rows.first {
            $0.isFolder && Set($0.paths) == Set(paths)
        }?.path
        pendingConfirmation = ChangesCopy.confirmation(selection, status: status, folder: folder)
    }

    /// Sends the discard the user just answered.
    func confirm() async {
        guard let pendingConfirmation else { return }
        await confirm(pendingConfirmation)
    }

    /// Discards what `confirmation` names. The dialog passes the question it showed: closing it
    /// clears `pendingConfirmation` before the answer's action runs.
    func confirm(_ confirmation: Confirmation) async {
        guard canWrite else { return }
        pendingConfirmation = nil
        discards += 1
        let files = confirmation.files
        working += 1
        defer { working -= 1 }
        do {
            let selection = confirmation.selection
            show(try await services.discard(subject, paths: selection.paths, seen: selection.seen))
            notice = nil
        } catch {
            // Trying again asks again: a discard is never sent without an answer on screen.
            await refused(error, retry: { [weak self] in self?.requestDiscard(files) })
        }
    }

    func retry() async {
        let action = retryAction
        retryAction = nil
        notice = nil
        await action?()
    }

    func dismissNotice() {
        notice = nil
        retryAction = nil
    }

    private func write(
        _ paths: [String], retry: @escaping () async -> Void,
        _ body: (ReviewSubject, ReviewSelection) async throws -> GitStatus
    ) async {
        guard canWrite, let status else { return }
        working += 1
        defer { working -= 1 }
        do {
            let selection = try ReviewSelection(status, paths: paths)
            show(try await body(subject, selection))
            notice = nil
            successes += 1
        } catch {
            await refused(error, retry: retry)
        }
    }

    /// Says why a write did not happen. When the list on screen is out of date, it is read again.
    private func refused(_ error: any Error, retry: (() async -> Void)?) async {
        if case .rpc(let rpc)? = error as? HelperError, rpc.code == RPCErrorCode.notManaged {
            // The read-only note says why, and the actions go.
            refusedUnmanaged = true
            notice = nil
            retryAction = nil
            return
        }
        let conflict = (error as? HelperError)?.gitConflict
        let canRetry = conflict == .locked && retry != nil
        notice = Notice(message: ChangesCopy.refusal(error, machine: machine), canRetry: canRetry)
        retryAction = canRetry ? retry : nil
        let outOfDate = conflict == .stale || conflict == .partial || conflict == .raced
            || (error as? ReviewSelection.Failure).map { if case .notShown = $0 { true } else { false } } == true
        if outOfDate { await refresh() }
    }

    // MARK: Commit

    var stagedCount: Int { status?.files.filter(Self.isStaged).count ?? 0 }

    /// Opens the commit sheet with amend off, however the sheet was left.
    func openCommitSheet() {
        amend = false
        commitError = nil
        isCommitSheetOpen = true
    }

    var canCommit: Bool {
        canWrite && !isWorking && !trimmed(message).isEmpty && (amend ? lastCommit != nil : stagedCount > 0)
    }

    var amendReplaces: String? {
        guard amend, let lastCommit else { return nil }
        return ChangesCopy.replaces(lastCommit)
    }

    /// Shown when the commit being replaced is already on the upstream branch.
    var amendWarning: String? {
        guard amend, upstream != nil, status?.ahead == 0 else { return nil }
        return ChangesCopy.pushed
    }

    /// Commits what is staged. True when a commit was made, and the sheet closes.
    func commit(message: String, amend: Bool) async -> Bool {
        let text = trimmed(message)
        // A second tap or Command-Return before the first commit answers must not commit again.
        guard canWrite, !isWorking, !text.isEmpty else { return false }
        let head = amend ? lastCommit?.hash : nil
        guard amend ? head != nil : stagedCount > 0 else {
            commitError = ChangesCopy.refusal(HelperError.conflict(GitConflict.nothingStaged.rawValue), machine: machine)
            return false
        }
        working += 1
        defer { working -= 1 }
        do {
            _ = try await services.commit(subject, message: text, amend: amend, head: head)
            notice = nil
        } catch let error as HelperError where error.gitConflict == .raced {
            // The commit was made on top of someone else's; say so over the list.
            notice = Notice(message: ChangesCopy.refusal(error, machine: machine), canRetry: false)
        } catch {
            commitError = ChangesCopy.refusal(error, machine: machine)
            if (error as? HelperError)?.gitConflict == .stale { await refresh() }
            return false
        }
        successes += 1
        commitError = nil
        self.message = ""
        self.amend = false
        isCommitSheetOpen = false
        await refresh()
        return true
    }

    private func trimmed(_ text: String) -> String {
        text.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    // MARK: Sections

    static func isConflict(_ file: GitStatus.File) -> Bool {
        file.index == "U" || file.worktree == "U" || (file.index == file.worktree && (file.index == "A" || file.index == "D"))
    }

    static func isStaged(_ file: GitStatus.File) -> Bool {
        !isConflict(file) && file.index != "." && file.index != "?"
    }

    static func isChanged(_ file: GitStatus.File) -> Bool {
        !isConflict(file) && file.worktree != "." && file.worktree != "?"
    }

    static func kind(of file: GitStatus.File, isIn kind: ChangeSection.Kind) -> Bool {
        switch kind {
        case .staged: isStaged(file)
        case .changed: isChanged(file)
        case .untracked: file.index == "?"
        case .conflicted: isConflict(file)
        }
    }

    static func sections(for status: GitStatus) -> [ChangeSection] {
        let files = status.files
        let staged = files.filter(isStaged).map {
            ChangeRow(section: .staged, path: $0.path, paths: [$0.path], change: change($0.index, from: $0.origPath))
        }
        let changed = files.filter(isChanged).map {
            ChangeRow(section: .changed, path: $0.path, paths: [$0.path], change: change($0.worktree, from: $0.origPath))
        }
        let conflicted = files.filter(isConflict).map {
            ChangeRow(section: .conflicted, path: $0.path, paths: [$0.path], change: .conflict)
        }
        let sections = [
            ChangeSection(kind: .staged, rows: staged),
            ChangeSection(kind: .changed, rows: changed),
            ChangeSection(kind: .untracked, rows: untrackedRows(files)),
            ChangeSection(kind: .conflicted, rows: conflicted),
        ]
        return sections.filter { !$0.rows.isEmpty }
    }

    private static func change(_ letter: String, from orig: String?) -> ChangeRow.Change {
        switch letter {
        case "A": .added
        case "D": .deleted
        case "T": .typeChanged
        case "R": .renamed(from: orig ?? "")
        case "C": .copied(from: orig ?? "")
        default: .modified
        }
    }

    /// Untracked files one by one, except that a folder holding two or more of them and no other
    /// listed change shows as one row, the way `git status` shows a new folder.
    private static func untrackedRows(_ files: [GitStatus.File]) -> [ChangeRow] {
        var rows: [ChangeRow] = []
        var grouped: Set<String> = []
        for file in files where file.index == "?" {
            guard !grouped.contains(file.path) else { continue }
            if let folder = newFolder(containing: file.path, in: files) {
                let members = files.filter { $0.path.hasPrefix(folder) }.map(\.path)
                grouped.formUnion(members)
                rows.append(ChangeRow(section: .untracked, path: folder, paths: members, change: .untrackedFolder(members.count)))
            } else {
                rows.append(ChangeRow(section: .untracked, path: file.path, paths: [file.path], change: .untracked))
            }
        }
        return rows
    }

    /// The outermost folder above `path` whose listed changes are all untracked, two or more.
    private static func newFolder(containing path: String, in files: [GitStatus.File]) -> String? {
        let parts = path.split(separator: "/", omittingEmptySubsequences: false)
        guard parts.count > 1 else { return nil }
        for depth in 1..<parts.count {
            let folder = parts.prefix(depth).joined(separator: "/") + "/"
            let inside = files.filter { $0.path.hasPrefix(folder) }
            if inside.count >= 2, inside.allSatisfy({ $0.index == "?" }) { return folder }
        }
        return nil
    }
}
