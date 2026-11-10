import Foundation
import Observation
import TogoKit

/// Words on the history screens, under `review.history.`.
enum HistoryCopy {
    static let title = String(localized: "review.history.title", defaultValue: "History")
    static let branch = String(localized: "review.history.branch", defaultValue: "Branch")
    static let detached = String(localized: "review.history.detached", defaultValue: "Detached HEAD")
    static let showFiles = String(localized: "review.history.showFiles", defaultValue: "Show changed files")
    static let copyHash = String(localized: "review.history.copyHash", defaultValue: "Copy hash")
    static let merge = String(localized: "review.history.merge", defaultValue: "Merge")
    static let loading = String(localized: "review.history.loading", defaultValue: "Reading the history")
    static let loadingFiles = String(localized: "review.history.loadingFiles", defaultValue: "Listing the files")
    static let failed = String(localized: "review.history.failed", defaultValue: "Couldn't read the history")
    static let empty = String(localized: "review.history.empty", defaultValue: "No commits yet")
    static let emptyMessage = String(
        localized: "review.history.emptyMessage", defaultValue: "Commits show up here once the first one is made."
    )
    static let tryAgain = String(localized: "review.history.tryAgain", defaultValue: "Try again")
    static let localOnly = String(localized: "review.history.localOnly", defaultValue: "Local only")
    static let showMore = String(localized: "review.history.showMore", defaultValue: "Show more")
    static let loadingMore = String(localized: "review.history.loadingMore", defaultValue: "Loading more")
    static let moreFailed = String(
        localized: "review.history.moreFailed", defaultValue: "Couldn't load more commits."
    )
    static let latest200 = String(localized: "review.history.latest200", defaultValue: "Showing the latest 200 commits")
    static let browse = String(localized: "review.history.browse", defaultValue: "Browse files at this commit")
    static let changedFiles = String(localized: "review.history.changedFiles", defaultValue: "Changed files")
    static let noChanges = String(localized: "review.history.noChanges", defaultValue: "No file changes")
    static let noChangesMessage = String(
        localized: "review.history.noChangesMessage", defaultValue: "This commit doesn't change any files."
    )
    static let cutOff = String(
        localized: "review.history.cutOff", defaultValue: "This commit is large, so only the first files are listed."
    )

    static func upstream(_ upstream: String?, current: Bool) -> String {
        switch (upstream, current) {
        case let (name?, true):
            String(localized: "review.history.currentTracks", defaultValue: "Current branch, tracks \(name)")
        case let (name?, false):
            String(localized: "review.history.tracks", defaultValue: "Tracks \(name)")
        case (nil, true):
            String(localized: "review.history.currentLocal", defaultValue: "Current branch, local only")
        case (nil, false):
            localOnly
        }
    }

    static func filesChanged(_ count: Int) -> String {
        count == 1
            ? String(localized: "review.history.fileChanged", defaultValue: "1 file")
            : String(localized: "review.history.filesChanged", defaultValue: "\(count) files")
    }

    /// "main (current)" in the branch menu.
    static func current(_ branch: String) -> String {
        String(localized: "review.history.currentBranch", defaultValue: "\(branch) (current)")
    }

    static func added(_ count: Int) -> String {
        count == 1
            ? String(localized: "review.history.addedOne", defaultValue: "1 line added")
            : String(localized: "review.history.added", defaultValue: "\(count) lines added")
    }

    static func removed(_ count: Int) -> String {
        count == 1
            ? String(localized: "review.history.removedOne", defaultValue: "1 line removed")
            : String(localized: "review.history.removed", defaultValue: "\(count) lines removed")
    }

    static func change(_ change: DiffFile.Change) -> String {
        switch change {
        case .added: String(localized: "review.history.change.added", defaultValue: "Added")
        case .deleted: String(localized: "review.history.change.deleted", defaultValue: "Deleted")
        case .modified: String(localized: "review.history.change.modified", defaultValue: "Modified")
        case .renamed: String(localized: "review.history.change.renamed", defaultValue: "Renamed")
        case .copied: String(localized: "review.history.change.copied", defaultValue: "Copied")
        case .modeOnly: String(localized: "review.history.change.mode", defaultValue: "Permissions changed")
        }
    }
}

extension GitCommit {
    var displaySubject: String { DisplayText.clean(subject) }
    var displayAuthor: String { DisplayText.clean(author) }
}

extension GitBranches.Branch {
    var displayName: String { DisplayText.clean(name) }
}

/// The log of one branch of the session's repository, the current one unless another is picked,
/// with the lanes of its commit graph. Commits come in pages of 50, up to the 200 the helper
/// serves; each page asks for the whole log so far, since the helper takes a limit and no offset.
@MainActor @Observable final class HistoryModel {
    enum State: Equatable {
        case loading, ready
        case failed(String)
    }

    nonisolated static let pageSize = 50
    nonisolated static let maxCommits = 200

    let subject: ReviewSubject
    private let services: any ReviewServices
    private let now: () -> Date

    private(set) var state: State = .loading
    /// The current branch first, then the others by name, for the branch menu.
    private(set) var branches: [GitBranches.Branch] = []
    private(set) var commits: [GitCommit] = [] {
        didSet { graph = HistoryGraph.rows(commits) }
    }
    /// One row of lanes per commit, in step with `commits`.
    private(set) var graph: [HistoryGraph.Row] = []
    /// The branch picked in the menu; nil follows the current one.
    private(set) var selectedRef: String?
    /// The checked-out branch; nil on a detached HEAD.
    private(set) var currentBranch: String?
    private(set) var limit = HistoryModel.pageSize
    private(set) var loadingMore = false
    private(set) var moreFailed = false
    private var generation = 0

    init(subject: ReviewSubject, services: any ReviewServices, now: @escaping () -> Date = Date.init) {
        self.subject = subject
        self.services = services
        self.now = now
    }

    var canLoadMore: Bool { commits.count >= limit && limit < Self.maxCommits }
    var reachedLimit: Bool { limit >= Self.maxCommits && commits.count >= Self.maxCommits }

    func loadIfNeeded() async {
        guard commits.isEmpty, branches.isEmpty, state == .loading else { return }
        await load()
    }

    /// Reads the branches and the first page again.
    func load() async {
        generation += 1
        let mine = generation
        limit = Self.pageSize
        moreFailed = false
        let ref = selectedRef
        do {
            let result = try await services.branches(subject)
            let page = try await services.log(subject, ref: ref, limit: limit)
            guard mine == generation else { return }
            branches = Self.ordered(result)
            currentBranch = result.current ?? result.branches.first(where: \.current)?.name
            commits = page
            state = .ready
        } catch {
            guard mine == generation else { return }
            state = .failed(ServiceErrors.message(error))
        }
    }

    /// The branch the log shows, for the menu's label: the one picked, else the current one.
    var shownBranch: String? { selectedRef ?? currentBranch }

    /// The widest row of lanes, so every row draws its graph in one column width.
    var graphWidth: Int { graph.map(\.width).max() ?? 1 }

    /// Shows the log of `branch`; the current branch is followed as HEAD moves.
    func select(branch: String) async {
        let ref = branch == currentBranch ? nil : branch
        guard ref != selectedRef else { return }
        selectedRef = ref
        await reloadLog()
    }

    func loadMore() async {
        guard canLoadMore, !loadingMore else { return }
        let mine = generation
        loadingMore = true
        defer { loadingMore = false }
        let next = limit + Self.pageSize
        do {
            let page = try await services.log(subject, ref: selectedRef, limit: next)
            guard mine == generation else { return }
            commits = page
            limit = next
            moreFailed = false
        } catch {
            guard mine == generation else { return }
            moreFailed = true
        }
    }

    /// A commit opens on its diff, every file it changed one after another.
    func route(for commit: GitCommit) -> ReviewRoute { .diff(.commit(hash: commit.hash, path: nil)) }

    /// The list of the files a commit changed, with a way into the tree as it was.
    func filesRoute(for commit: GitCommit) -> ReviewRoute { .commit(commit) }

    /// "3 hours ago", measured from the model's clock.
    func when(_ commit: GitCommit) -> String {
        Self.formatter.localizedString(for: commit.date, relativeTo: now())
    }

    private func reloadLog() async {
        generation += 1
        let mine = generation
        limit = Self.pageSize
        moreFailed = false
        do {
            let page = try await services.log(subject, ref: selectedRef, limit: limit)
            guard mine == generation else { return }
            commits = page
            state = .ready
        } catch {
            guard mine == generation else { return }
            state = .failed(ServiceErrors.message(error))
        }
    }

    private static func ordered(_ result: GitBranches) -> [GitBranches.Branch] {
        let current = result.branches.filter { $0.current || $0.name == result.current }
        let others = result.branches.filter { branch in !current.contains(branch) }
        return current + others.sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
    }

    private static let formatter: RelativeDateTimeFormatter = {
        let formatter = RelativeDateTimeFormatter()
        formatter.unitsStyle = .full
        return formatter
    }()
}

/// One commit: the files it changed, each opening its diff, and a way into the tree as it was.
@MainActor @Observable final class CommitDetailModel {
    enum State: Equatable {
        case loading, ready, empty
        case failed(String)
    }

    let subject: ReviewSubject
    let commit: GitCommit
    private let services: any ReviewServices

    private(set) var state: State = .loading
    private(set) var files: [DiffFile] = []
    private(set) var isCutOff = false

    init(subject: ReviewSubject, services: any ReviewServices, commit: GitCommit) {
        self.subject = subject
        self.services = services
        self.commit = commit
    }

    var browseRoute: ReviewRoute { .folder(path: "", ref: commit.hash) }

    func route(for file: DiffFile) -> ReviewRoute {
        .diff(.commit(hash: commit.hash, path: file.id))
    }

    func loadIfNeeded() async {
        guard state == .loading, files.isEmpty else { return }
        await load()
    }

    func load() async {
        do {
            let diff = try await services.diff(subject, .commit(hash: commit.hash, path: nil))
            let parsed = await Task.detached(priority: .userInitiated) { DiffParser.parse(diff) }.value
            files = parsed
            isCutOff = diff.truncated
            state = parsed.isEmpty ? .empty : .ready
        } catch {
            state = .failed(ServiceErrors.message(error))
        }
    }
}
