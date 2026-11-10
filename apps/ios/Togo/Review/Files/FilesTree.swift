import Foundation
import Observation
import TogoKit

/// Words on the file tree and its search, under `review.files.`.
enum FileTreeCopy {
    static let searchPrompt = String(localized: "review.files.searchPrompt", defaultValue: "Search files")
    static let searching = String(localized: "review.files.searching", defaultValue: "Searching")
    static let noResults = String(localized: "review.files.noResults", defaultValue: "No files found")
    static let searchFailed = String(localized: "review.files.searchFailed", defaultValue: "Couldn't search the files")
    static let partial = String(
        localized: "review.files.partial", defaultValue: "Searching only the folders opened so far. Update the helper to search every file."
    )
    static let showChanges = String(localized: "review.files.showChanges", defaultValue: "Show changes")
    static let modified = String(localized: "review.files.modified", defaultValue: "Modified")
    static let added = String(localized: "review.files.added", defaultValue: "Added")
    static let untracked = String(localized: "review.files.untracked", defaultValue: "Untracked")
    static let conflicted = String(localized: "review.files.conflicted", defaultValue: "Conflicted")
    static let holdsChanges = String(localized: "review.files.holdsChanges", defaultValue: "Has changes")
    static let opening = String(localized: "review.files.opening", defaultValue: "Opening")

    static func noResultsMessage(_ query: String) -> String {
        String(localized: "review.files.noResultsMessage", defaultValue: "No file name or path contains “\(query)”.")
    }

    static func moreResults(_ count: Int) -> String {
        String(localized: "review.files.moreResults", defaultValue: "Showing the first \(count). Type more to narrow it down.")
    }
}

/// What an opened folder holds while it is read.
enum FolderListing: Equatable {
    case loading
    case ready([DirEntry])
    case failed(String)
}

// MARK: Tree

extension FilesModel {
    func isExpanded(_ folder: String) -> Bool { expanded.contains(folder) }

    /// Opens or closes a folder in place; the first opening reads it.
    func setExpanded(_ folder: String, _ open: Bool) async {
        guard open else {
            expanded.remove(folder)
            return
        }
        expanded.insert(folder)
        switch folders[folder] {
        case .ready?, .loading?: return
        case .failed?, nil: await read(folder)
        }
    }

    /// The rows inside an opened folder, or nil while it is read or when it failed.
    func children(of folder: String) -> [Item]? {
        guard case .ready(let entries)? = folders[folder] else { return nil }
        return items(of: entries, in: folder)
    }

    func read(_ folder: String) async {
        folders[folder] = .loading
        do {
            let entries = try await reviewServices.listDir(subject, path: folder, ref: ref, base: base).entries
            folders[folder] = .ready(entries)
        } catch {
            folders[folder] = .failed(ServiceErrors.message(error))
        }
    }

    /// After a pull to refresh: the folders still open are read again, the closed ones forgotten.
    func reloadOpenFolders() async {
        folders = folders.filter { expanded.contains($0.key) }
        for folder in expanded.sorted() { await read(folder) }
    }

    /// The working tree's changes, marked on the files and folders that hold them. Commits and
    /// folders outside the repository have none; a failed read leaves the marks off.
    func loadMarks() async {
        guard ref == nil, subject.canRead, let status = try? await reviewServices.status(subject), status.repo else {
            marks = ChangeMarks()
            return
        }
        let prefix: String? = switch base {
        case .repository: ""
        case .session: status.root.flatMap { ChangeMarks.sessionPrefix(cwd: subject.cwd, root: $0) }
        }
        guard let prefix else {
            marks = ChangeMarks()
            return
        }
        marks = ChangeMarks(status, prefix: prefix)
    }

    /// The diff of a marked file.
    func changesRoute(for item: Item) -> ReviewRoute? {
        guard item.kind == .file, let file = marks.files[item.path] else { return nil }
        let path = file.path
        if file.index == "?" { return .diff(.untracked(path: path)) }
        if file.worktree == "." { return .diff(.staged(path: path)) }
        return .diff(.worktree(path: path))
    }
}

/// The changes git reports, by path as the tree names them. A folder is marked while anything
/// under it changed, a deleted file included.
struct ChangeMarks: Equatable {
    enum Mark: Equatable {
        case modified, added, untracked, conflicted

        /// One letter, as editors mark changed files.
        var letter: String {
            switch self {
            case .modified: "M"
            case .added: "A"
            case .untracked: "U"
            case .conflicted: "!"
            }
        }

        var words: String {
            switch self {
            case .modified: FileTreeCopy.modified
            case .added: FileTreeCopy.added
            case .untracked: FileTreeCopy.untracked
            case .conflicted: FileTreeCopy.conflicted
            }
        }
    }

    /// The status entries, keyed by tree path.
    private(set) var files: [String: GitStatus.File] = [:]
    private(set) var folders: Set<String> = []

    init() {}

    /// `prefix` is the tree's root inside the repository ("" at the repository root).
    init(_ status: GitStatus, prefix: String) {
        let lead = prefix.isEmpty ? "" : prefix + "/"
        for file in status.files {
            guard file.path.hasPrefix(lead) else { continue }
            let path = String(file.path.dropFirst(lead.count))
            guard !path.isEmpty else { continue }
            files[path] = file
            var parts = path.split(separator: "/").dropLast()
            while !parts.isEmpty {
                folders.insert(parts.joined(separator: "/"))
                parts = parts.dropLast()
            }
        }
    }

    func mark(_ path: String) -> Mark? {
        guard let file = files[path] else { return nil }
        let pair = file.index + file.worktree
        if file.index == "U" || file.worktree == "U" || pair == "AA" || pair == "DD" { return .conflicted }
        if file.index == "?" { return .untracked }
        if file.index == "A" { return .added }
        return .modified
    }

    func holdsChanges(_ folder: String) -> Bool { folders.contains(folder) }

    /// Where the session's folder sits in the repository: "" at its root, "api" one folder in.
    /// Nil when the folder is not inside `root` as written (a link the helper resolved).
    nonisolated static func sessionPrefix(cwd: String, root: String) -> String? {
        func trimmed(_ path: String) -> String {
            var path = path
            while path.count > 1, path.hasSuffix("/") { path.removeLast() }
            // macOS resolves /var and /tmp to /private/...
            if path.hasPrefix("/private/") { path.removeFirst("/private".count) }
            return path
        }
        let cwd = trimmed(cwd), root = trimmed(root)
        if cwd == root { return "" }
        guard cwd.hasPrefix(root + "/") else { return nil }
        return String(cwd.dropFirst(root.count + 1))
    }
}

// MARK: Search

/// Finds files by name or path across the whole repository (`git.files`), or, from a helper
/// without it, across the folders opened so far. Ranked: exact names, then names starting with
/// the query, then names holding it, then paths holding it.
@MainActor @Observable final class FileSearch {
    enum State: Equatable {
        case idle, searching, ready
        case failed(String)
    }

    struct Hit: Identifiable, Hashable {
        var path: String
        var route: ReviewRoute
        var id: String { path }
        var name: String { DisplayText.clean(path.split(separator: "/").last.map(String.init) ?? path) }
        var folder: String {
            guard let slash = path.lastIndex(of: "/") else { return "" }
            return DisplayText.clean(String(path[..<slash]))
        }
    }

    nonisolated static let limit = 200

    let subject: ReviewSubject
    let ref: String?
    private let services: any ReviewServices

    private(set) var state: State = .idle
    private(set) var hits: [Hit] = []
    /// More files matched than `limit`.
    private(set) var more = false
    /// The helper cannot list every file, so only opened folders were searched.
    private(set) var partial = false
    var query = "" {
        didSet { if query != oldValue { run() } }
    }

    /// Repository paths, read on the first search.
    private var index: [String]?
    private var task: Task<Void, Never>?
    /// What to search when the helper cannot list every file: the paths the tree has read.
    @ObservationIgnored var loadedPaths: () -> (paths: [String], base: ReviewPathBase) = { ([], .session) }
    @ObservationIgnored var showsHidden: () -> Bool = { false }

    init(subject: ReviewSubject, services: any ReviewServices, ref: String?) {
        self.subject = subject
        self.services = services
        self.ref = ref
    }

    var isActive: Bool { !query.trimmingCharacters(in: .whitespaces).isEmpty }

    /// Waits for the search for the current query to finish.
    func settle() async { await task?.value }

    private func run() {
        task?.cancel()
        let query = query.trimmingCharacters(in: .whitespaces)
        guard !query.isEmpty else {
            hits = []
            more = false
            state = .idle
            task = nil
            return
        }
        if hits.isEmpty { state = .searching }
        task = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(120))
            guard !Task.isCancelled, let self else { return }
            let (paths, base) = await source()
            guard !Task.isCancelled else { return }
            let hidden = showsHidden()
            let limit = Self.limit + 1
            let ranked = await Task.detached(priority: .userInitiated) {
                FileSearch.rank(paths, query: query, hidden: hidden, limit: limit)
            }.value
            guard !Task.isCancelled, self.query.trimmingCharacters(in: .whitespaces) == query else { return }
            more = ranked.count > Self.limit
            let ref = ref
            hits = ranked.prefix(Self.limit).map { Hit(path: $0, route: .file(path: $0, ref: ref, base: base)) }
            state = .ready
        }
    }

    /// Every repository path, read once; the tree's paths when the helper cannot list them.
    private func source() async -> ([String], ReviewPathBase) {
        if let index { return (index, .repository) }
        if !partial {
            do {
                let files = try await services.files(subject, ref: ref)
                index = files.paths
                return (files.paths, .repository)
            } catch {
                partial = true
            }
        }
        let loaded = loadedPaths()
        return (loaded.paths, loaded.base)
    }

    /// The paths matching `query`, best first, at most `limit`.
    nonisolated static func rank(_ paths: [String], query: String, hidden: Bool, limit: Int) -> [String] {
        let needle = query.lowercased()
        var scored: [(score: Int, path: String)] = []
        for path in paths {
            if !hidden, path.split(separator: "/").contains(where: { $0.hasPrefix(".") }) { continue }
            let lower = path.lowercased()
            let name = lower.split(separator: "/").last.map(String.init) ?? lower
            let score: Int
            if name == needle {
                score = 0
            } else if name.hasPrefix(needle) {
                score = 1
            } else if name.contains(needle) {
                score = 2
            } else if lower.contains(needle) {
                score = 3
            } else {
                continue
            }
            scored.append((score, path))
        }
        scored.sort { a, b in
            if a.score != b.score { return a.score < b.score }
            if a.path.count != b.path.count { return a.path.count < b.path.count }
            return a.path < b.path
        }
        return scored.prefix(limit).map(\.path)
    }
}

extension FilesModel {
    /// The file paths the tree has read, for a search without `git.files`.
    var loadedFilePaths: [String] {
        var listings: [(String, [DirEntry])] = [(path, entries)]
        for (folder, listing) in folders {
            if case .ready(let entries) = listing { listings.append((folder, entries)) }
        }
        return listings.flatMap { folder, entries in
            entries.filter { $0.kind == .file || ($0.kind == nil && !$0.dir) }.map { Self.join(folder, $0.name) }
        }
    }

    /// Hooks the search up to what the tree has read and to the hidden-files toggle.
    func connectSearch() {
        search.loadedPaths = { [weak self] in
            guard let self else { return ([], .session) }
            return (loadedFilePaths, base)
        }
        search.showsHidden = { [weak self] in self?.showsHidden ?? false }
    }
}
