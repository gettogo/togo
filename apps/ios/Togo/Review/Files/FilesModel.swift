import Foundation
import Observation
import TogoKit
import UIKit

/// Words on the file browser and viewer, under `review.files.`.
enum FilesCopy {
    static let hidden = String(localized: "review.files.hidden", defaultValue: "Show hidden files")
    static let backToWorkingTree = String(localized: "review.files.backToWorkingTree", defaultValue: "Back to working tree")
    static let loading = String(localized: "review.files.loading", defaultValue: "Opening the folder")
    static let failed = String(localized: "review.files.failed", defaultValue: "Couldn't open this folder")
    static let tryAgain = String(localized: "review.files.tryAgain", defaultValue: "Try again")
    static let emptyFolder = String(localized: "review.files.emptyFolder", defaultValue: "This folder is empty")
    static let onlyHidden = String(
        localized: "review.files.onlyHidden", defaultValue: "Everything in it is hidden. Turn on Show hidden files to see it."
    )
    static let emptyMessage = String(localized: "review.files.emptyMessage", defaultValue: "Nothing has been added here yet.")
    static let loadingFile = String(localized: "review.files.loadingFile", defaultValue: "Opening the file")
    static let failedFile = String(localized: "review.files.failedFile", defaultValue: "Couldn't open this file")
    static let emptyFile = String(localized: "review.files.emptyFile", defaultValue: "This file is empty")
    static let emptyFileMessage = String(localized: "review.files.emptyFileMessage", defaultValue: "There is nothing in it to show.")
    static let truncated = String(localized: "review.files.truncated", defaultValue: "Showing the first 512 KB.")
    static let wrap = String(localized: "review.files.wrap", defaultValue: "Wrap lines")
    static let options = String(localized: "review.files.options", defaultValue: "Options")
    static let findPrompt = String(localized: "review.files.findPrompt", defaultValue: "Find in file")
    static let closeFind = String(localized: "review.files.closeFind", defaultValue: "Close find")
    static let noMatches = String(localized: "review.files.noMatches", defaultValue: "No matches")
    static let previousMatch = String(localized: "review.files.previousMatch", defaultValue: "Previous match")
    static let nextMatch = String(localized: "review.files.nextMatch", defaultValue: "Next match")
    static let link = String(localized: "review.files.link", defaultValue: "Link")
    static let linkHint = String(localized: "review.files.linkHint", defaultValue: "Links aren't followed from here.")
    static let zoomHint = String(localized: "review.files.zoomHint", defaultValue: "Pinch to zoom. Double tap to zoom in or out.")
    static let image = String(localized: "review.files.image", defaultValue: "Image")
    static let folder = String(localized: "review.files.folder", defaultValue: "Folder")
    static let file = String(localized: "review.files.file", defaultValue: "File")

    static func atRef(_ short: String) -> String {
        String(localized: "review.files.atRef", defaultValue: "At \(short)")
    }

    static func binary(size: Int) -> String {
        let text = ByteCountFormatter.string(fromByteCount: Int64(size), countStyle: .file)
        return String(localized: "review.files.binary", defaultValue: "Binary file, \(text), not shown")
    }

    static func position(_ index: Int, of count: Int) -> String {
        String(localized: "review.files.position", defaultValue: "\(index) of \(count)")
    }

    static func size(_ bytes: Int) -> String {
        ByteCountFormatter.string(fromByteCount: Int64(bytes), countStyle: .file)
    }
}

/// A folder of the working tree, or of a commit when `ref` is set, as a tree: folders open in
/// place (`FilesTree.swift`). Folders come first, then files, each by name; names starting with
/// a dot stay behind a toggle. In the working tree, changed files and the folders holding them
/// are marked. A search looks through every file of the repository.
@MainActor @Observable final class FilesModel {
    enum State: Equatable {
        case loading, ready
        case failed(String)
    }

    struct Item: Identifiable, Hashable {
        enum Kind: Hashable, Sendable { case folder, file, link }

        var name: String
        var path: String
        var kind: Kind
        var size: Int?
        /// Nil for a link: it is listed and never followed.
        var route: ReviewRoute?
        var id: String { path }
    }

    let subject: ReviewSubject
    let path: String
    let ref: String?
    let base: ReviewPathBase
    private let services: any ReviewServices

    private(set) var state: State = .loading
    private(set) var entries: [DirEntry] = []
    var showsHidden = false

    /// Folders opened in place, by path.
    var expanded: Set<String> = []
    /// What each opened folder holds, by path; the root's is `entries`.
    var folders: [String: FolderListing] = [:]
    /// The working tree's changes, by path as this tree names them.
    var marks = ChangeMarks()
    let search: FileSearch

    init(subject: ReviewSubject, services: any ReviewServices, path: String, ref: String?, base: ReviewPathBase = .session) {
        self.subject = subject
        self.services = services
        self.path = path
        self.ref = ref
        self.base = base
        search = FileSearch(subject: subject, services: services, ref: ref)
        connectSearch()
    }

    /// For the tree's own reads, in `FilesTree.swift`.
    var reviewServices: any ReviewServices { services }

    var title: String {
        // At the root it is the tab's own title, the same on every tab; a folder names itself.
        path.isEmpty ? subject.displayTitle : DisplayText.clean(String(path.split(separator: "/").last ?? ""))
    }

    var breadcrumb: String { Self.breadcrumb(path) }

    /// "At a1b2c3d" while looking at a commit.
    var refBanner: String? { ref.map { FilesCopy.atRef(String($0.prefix(7))) } }
    var workingTreeRoute: ReviewRoute { .folder(path: "", ref: nil) }

    var hiddenCount: Int { entries.filter { $0.name.hasPrefix(".") }.count }

    var items: [Item] { items(of: entries, in: path) }

    /// `entries` of `folder` as rows: hidden names left out unless asked for, folders first.
    func items(of entries: [DirEntry], in folder: String) -> [Item] {
        let shown = showsHidden ? entries : entries.filter { !$0.name.hasPrefix(".") }
        return shown.map { item($0, in: folder) }.sorted { a, b in
            if (a.kind == .folder) != (b.kind == .folder) { return a.kind == .folder }
            return a.name.localizedStandardCompare(b.name) == .orderedAscending
        }
    }

    func loadIfNeeded() async {
        guard state == .loading, entries.isEmpty else { return }
        await load()
    }

    /// Reads the root again, the folders open in it, and what changed.
    func load() async {
        do {
            entries = try await services.listDir(subject, path: path, ref: ref, base: base).entries
            state = .ready
        } catch {
            state = .failed(ServiceErrors.message(error))
            return
        }
        await reloadOpenFolders()
        await loadMarks()
    }

    private func item(_ entry: DirEntry, in folder: String) -> Item {
        let full = Self.join(folder, entry.name)
        let kind: Item.Kind
        switch entry.kind {
        case .symlink?: kind = .link
        case .dir?: kind = .folder
        case .file?: kind = .file
        default: kind = entry.dir ? .folder : .file
        }
        let route: ReviewRoute? = switch kind {
        case .folder: .folder(path: full, ref: ref, base: base)
        case .file: .file(path: full, ref: ref, base: base)
        case .link: nil
        }
        return Item(name: DisplayText.clean(entry.name), path: full, kind: kind, size: entry.size, route: route)
    }

    nonisolated static func join(_ folder: String, _ name: String) -> String {
        folder.isEmpty ? name : folder + "/" + name
    }

    /// The first folder, then the last two, with the middle left out: "src/…/handlers/items".
    nonisolated static func breadcrumb(_ path: String) -> String {
        let parts = DisplayText.clean(path).split(separator: "/")
        guard parts.count > 4 else { return parts.joined(separator: "/") }
        return "\(parts[0])/…/\(parts[parts.count - 2])/\(parts[parts.count - 1])"
    }
}

/// One file, read-only: highlighted text with line numbers and find, an image, or a sentence
/// saying why it is not shown.
@MainActor @Observable final class FileViewerModel {
    enum State: Equatable {
        case loading, ready
        case failed(String)
    }

    enum Content: Equatable, Sendable {
        case text([CodeRow])
        case image(FileImage)
        case binary(size: Int)
        case empty
    }

    struct Match: Equatable {
        var row: Int
        /// UTF-16 offsets into the row's text.
        var range: Range<Int>
    }

    nonisolated static let imageLimit = 512 * 1024
    nonisolated static let imageExtensions: Set<String> = ["png", "jpg", "jpeg", "gif", "heic", "webp"]
    nonisolated static let matchLimit = 5_000

    let subject: ReviewSubject
    let path: String
    let ref: String?
    let base: ReviewPathBase
    private let services: any ReviewServices

    private(set) var state: State = .loading
    private(set) var content: Content?
    private(set) var truncatedNotice: String?
    var wrap = false

    private(set) var matches: [Match] = []
    private(set) var currentMatch: Int?
    private(set) var scrollTarget: Int?
    private(set) var displayRows: [CodeRow] = []
    /// The search stopped at `matchLimit`.
    private(set) var capped = false
    private(set) var searching = false
    var query = "" {
        didSet { if query != oldValue { search() } }
    }

    init(subject: ReviewSubject, services: any ReviewServices, path: String, ref: String?, base: ReviewPathBase = .session) {
        self.subject = subject
        self.services = services
        self.path = path
        self.ref = ref
        self.base = base
    }

    var fileName: String { DisplayText.clean(path.split(separator: "/").last.map(String.init) ?? path) }

    /// The path for a loading sentence, safe to show.
    var displayPath: String { DisplayText.clean(path) }

    var breadcrumb: String {
        let folder = path.split(separator: "/").dropLast().joined(separator: "/")
        return FilesModel.breadcrumb(folder)
    }

    var refBanner: String? { ref.map { FilesCopy.atRef(String($0.prefix(7))) } }

    var isImage: Bool {
        if case .image? = content { true } else { false }
    }

    func loadIfNeeded() async {
        guard state == .loading, content == nil else { return }
        await load()
    }

    func load() async {
        do {
            let result = try await services.read(subject, path: path, ref: ref, base: base)
            let path = path
            let prepared = await Task.detached(priority: .userInitiated) { Self.prepare(result, path: path) }.value
            content = prepared
            truncatedNotice = result.truncated && prepared.isText ? FilesCopy.truncated : nil
            if case .text(let rows) = prepared { displayRows = rows }
            state = .ready
            search()
        } catch {
            state = .failed(ServiceErrors.message(error))
        }
    }

    // MARK: Find

    var position: String? {
        guard !query.isEmpty else { return nil }
        guard let currentMatch else { return searching ? nil : FilesCopy.noMatches }
        return FilesCopy.position(currentMatch + 1, of: matches.count) + (capped ? "+" : "")
    }

    func next() { step(1) }
    func previous() { step(-1) }

    private func step(_ delta: Int) {
        guard let currentMatch, !matches.isEmpty else { return }
        let target = (currentMatch + delta + matches.count) % matches.count
        self.currentMatch = target
        scrollTarget = matches[target].row
    }

    private var searchTask: Task<Void, Never>?

    /// Waits for the search for the current query to finish.
    func settle() async {
        await searchTask?.value
    }

    /// Looks for the query off the main actor, a moment after the last key, so typing stays
    /// smooth in a long file.
    private func search() {
        searchTask?.cancel()
        guard case .text(let rows)? = content else { return }
        guard !query.isEmpty else {
            matches = []
            currentMatch = nil
            displayRows = rows
            capped = false
            searching = false
            searchTask = nil
            return
        }
        let query = query
        searching = true
        searchTask = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(120))
            guard !Task.isCancelled else { return }
            let found = await Task.detached(priority: .userInitiated) { Self.find(query, in: rows) }.value
            guard !Task.isCancelled, let self, self.query == query else { return }
            matches = found.matches
            displayRows = found.rows
            currentMatch = found.matches.isEmpty ? nil : 0
            scrollTarget = found.matches.first?.row
            capped = found.capped
            searching = false
        }
    }

    nonisolated private static func find(_ query: String, in rows: [CodeRow]) -> (matches: [Match], rows: [CodeRow], capped: Bool) {
        var matches: [Match] = []
        var marked = rows
        var capped = false
        scan: for (index, row) in rows.enumerated() {
            guard case let .line(number, old, text, kind, tokens, _, clipped) = row else { continue }
            let string = text as NSString
            var found: [Range<Int>] = []
            var from = 0
            while from < string.length {
                let hit = string.range(of: query, options: [.caseInsensitive], range: NSRange(location: from, length: string.length - from))
                if hit.location == NSNotFound { break }
                found.append(hit.location..<(hit.location + hit.length))
                from = hit.location + max(hit.length, 1)
                if matches.count + found.count >= matchLimit { capped = true; break }
            }
            guard !found.isEmpty else { continue }
            matches += found.map { Match(row: index, range: $0) }
            marked[index] = .line(number: number, oldNumber: old, text: text, kind: kind, tokens: tokens, changes: found, clipped: clipped)
            if capped { break scan }
        }
        return (matches, marked, capped)
    }

    // MARK: Decoding

    nonisolated private static func prepare(_ result: FsReadResult, path: String) -> Content {
        let ext = (path as NSString).pathExtension.lowercased()
        if imageExtensions.contains(ext) {
            if result.encoding == .base64, !result.truncated, result.size < imageLimit,
               let data = result.data, let picture = ImageShrinker.preview(data, maxSide: FileImage.maxSide) {
                return .image(FileImage(data: data, picture: picture))
            }
            return .binary(size: result.size)
        }
        guard let file = TextDecoding.file(result) else { return .binary(size: result.size) }
        if result.size == 0 { return .empty }
        let language = Language.detect(path: path, firstLine: file.lines.first)
        let tokens = language == .plain ? [] : Tokenizer.tokenize(file.lines, language: language)
        return .text(HiddenCharacters.reveal(CodeRow.file(file, tokens: tokens)))
    }
}

/// An image file's bytes and the picture decoded from them while the file loads, off the main
/// thread and no larger than a 5× zoom can use, so the viewer draws it at once.
struct FileImage: Equatable, @unchecked Sendable {
    /// Long side in pixels: enough for the deepest zoom on the largest screen.
    nonisolated static let maxSide = 4096

    let data: Data
    /// Never mutated after decoding.
    let picture: UIImage

    nonisolated static func == (a: FileImage, b: FileImage) -> Bool { a.data == b.data }
}

private extension FileViewerModel.Content {
    var isText: Bool {
        if case .text = self { true } else { false }
    }
}
