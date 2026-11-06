#if DEBUG
import Foundation
import TogoKit

/// Errors that are not the helper's. Stale, locked and unmanaged are the helper's own, as
/// `HelperError` values: see `HelperError.stale`, `.locked` and `.unmanaged` below.
enum ReviewFixtureError: Error, LocalizedError, Equatable {
    case offline

    var errorDescription: String? { "The machine didn't answer." }
}

extension HelperError {
    static let stale = HelperError.conflict(GitConflict.stale.rawValue)
    static let locked = HelperError.conflict(GitConflict.locked.rawValue)
    /// `-32007`: the session was not started from Togo.
    static let unmanaged = HelperError.rpc(RPCErrorObject(code: RPCErrorCode.notManaged, message: "not managed"))
}

@MainActor enum ReviewFakes {
    /// What `-ui-fakes` reviews: see `UITestReviewServices`.
    static func uiTestServices() -> any ReviewServices {
        UITestReviewServices()
    }
}

/// Review services that serve `ReviewFixtures`. Writes change the status they serve, any call can
/// be made to fail or wait, and the calls are recorded.
@MainActor final class FakeReviewServices: ReviewServices {
    enum Call: Hashable {
        case status, diff, read, listDir, log, branches, stage, unstage, discard, commit, ports, forward, stopForward
    }

    enum Write: Hashable {
        case stage([String]), unstage([String]), discard([String])
        case commit(message: String, amend: Bool, head: String?)
    }

    /// A call that throws this instead of answering.
    var failures: [Call: any Error] = [:]
    /// A call that waits this long before answering, for loading states.
    var delays: [Call: Duration] = [:]
    private(set) var calls: [Call] = []
    private(set) var writes: [Write] = []
    private(set) var stoppedForwards: [Int] = []
    /// What each `read` and `listDir` path was relative to, by path.
    private(set) var bases: [String: ReviewPathBase] = [:]
    /// Replies for `read`, by path, in place of the fixtures' text.
    var reads: [String: FsReadResult] = [:]
    /// Listings by path, in place of the fixtures' tree.
    var trees: [String: [DirEntry]] = [:]
    /// Every commit the log can serve, newest first; a `ref` found in `refLogs` serves that list.
    var commits = ReviewFixtures.log
    var branchList = ReviewFixtures.branches
    var refLogs: [String: [GitCommit]] = [:]
    /// What `ports` answers.
    var portList = ReviewFixtures.ports
    private(set) var logRequests: [(ref: String?, limit: Int)] = []
    private(set) var current: GitStatus
    /// The hash an amend must name, as `git.log` lists first.
    private(set) var head = ReviewFixtures.log[0].hash
    private var subscribers: [UUID: AsyncStream<(localPort: Int, message: String)>.Continuation] = [:]

    init(status: GitStatus = ReviewFixtures.status) {
        current = status
    }

    /// Reports a forward that ended on its own.
    func endForward(localPort: Int, message: String) {
        for sink in subscribers.values {
            sink.yield((localPort, message))
        }
    }

    /// The agent edits a file after the phone fetched the status: its content no longer matches
    /// what the user saw.
    func editBehindBack(_ path: String) {
        change([path]) { $0.fingerprint = ($0.fingerprint ?? "blob:0") + "-edited" }
    }

    /// The agent commits after the phone fetched the history.
    func moveHead() {
        head = String(repeating: "c", count: 40)
    }

    /// Writes need a session Togo started, then every path must still look as the user saw it.
    private func enterWrite(_ call: Call, _ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws {
        try await enter(call)
        guard s.canWrite else { throw HelperError.unmanaged }
        for path in paths {
            guard let file = current.files.first(where: { $0.path == path }),
                  let saw = seen[path], saw == GitSeen(file) else { throw HelperError.stale }
        }
    }

    private func enter(_ call: Call) async throws {
        calls.append(call)
        if let delay = delays[call] { try await Task.sleep(for: delay) }
        if let failure = failures[call] { throw failure }
    }

    func status(_ s: ReviewSubject) async throws -> GitStatus {
        try await enter(.status)
        return current
    }

    func diff(_ s: ReviewSubject, _ source: DiffSource) async throws -> GitDiff {
        try await enter(.diff)
        switch source {
        case .worktree(let path), .staged(let path), .untracked(let path):
            if path == ReviewFixtures.bigDiffPath, UITestLaunch.current.wideDiff {
                return GitDiff(diff: ReviewFixtures.wideDiff, truncated: false, binary: [])
            }
            return ReviewFixtures.diffs[path] ?? GitDiff(diff: "", truncated: false, binary: [])
        case .commit(_, let path?):
            return ReviewFixtures.diffs[path] ?? GitDiff(diff: "", truncated: false, binary: [])
        case .commit(_, nil):
            let small = ["src/router.ts", "src/handlers/health.ts", "src/store.ts"]
            return GitDiff(diff: small.compactMap { ReviewFixtures.diffs[$0]?.diff }.joined(), truncated: false, binary: [])
        }
    }

    func read(_ s: ReviewSubject, path: String, ref: String?, base: ReviewPathBase) async throws -> FsReadResult {
        try await enter(.read)
        bases[path] = base
        if let reply = reads[path] { return reply }
        let text = ReviewFixtures.files[path] ?? "\(path)\n"
        return FsReadResult(path: path, size: text.utf8.count, encoding: .utf8, content: text, truncated: false)
    }

    func listDir(_ s: ReviewSubject, path: String, ref: String?, base: ReviewPathBase) async throws -> ListDirResult {
        try await enter(.listDir)
        bases[path] = base
        return ListDirResult(path: path, entries: trees[path] ?? ReviewFixtures.tree[path] ?? [])
    }

    func log(_ s: ReviewSubject, ref: String?, limit: Int) async throws -> [GitCommit] {
        try await enter(.log)
        logRequests.append((ref, limit))
        return Array((ref.flatMap { refLogs[$0] } ?? commits).prefix(limit))
    }

    func branches(_ s: ReviewSubject) async throws -> GitBranches {
        try await enter(.branches)
        return branchList
    }

    func stage(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus {
        try await enterWrite(.stage, s, paths: paths, seen: seen)
        writes.append(.stage(paths))
        change(paths) { file in
            if file.index == "?" {
                file.index = "A"
            } else if file.index == "." || file.worktree == "D" {
                file.index = file.worktree
            }
            file.worktree = "."
        }
        return current
    }

    func unstage(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus {
        try await enterWrite(.unstage, s, paths: paths, seen: seen)
        writes.append(.unstage(paths))
        change(paths) { file in
            file.worktree = file.index == "A" ? "?" : file.index
            file.index = file.index == "A" ? "?" : "."
        }
        return current
    }

    func discard(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus {
        try await enterWrite(.discard, s, paths: paths, seen: seen)
        writes.append(.discard(paths))
        current.files.removeAll { paths.contains($0.path) }
        return current
    }

    func commit(_ s: ReviewSubject, message: String, amend: Bool, head sawHead: String?) async throws -> GitCommitResult {
        try await enter(.commit)
        guard s.canWrite else { throw HelperError.unmanaged }
        if amend {
            guard let sawHead, GitCommitParams.isValidHash(sawHead) else { throw RPCErrorObject(code: -32602, message: "invalid params") }
            guard sawHead == head else { throw HelperError.stale }
        }
        guard amend || current.files.contains(where: { $0.index != "." && $0.index != "?" }) else {
            throw HelperError.conflict(GitConflict.nothingStaged.rawValue)
        }
        writes.append(.commit(message: message, amend: amend, head: sawHead))
        current.files.removeAll { $0.index != "." && $0.index != "?" && $0.worktree == "." }
        return GitCommitResult(hash: String(repeating: "b", count: 40), short: "bbbbbbb")
    }

    func ports(_ s: ReviewSubject) async throws -> [ListeningPort] {
        try await enter(.ports)
        return portList
    }

    func forward(_ s: ReviewSubject, port: ListeningPort) async throws -> ForwardInfo {
        try await enter(.forward)
        let local = 50_000 + port.port % 1_000
        return ForwardInfo(remote: port, localPort: local, url: URL(string: "http://127.0.0.1:\(local)/")!)
    }

    func stopForward(localPort: Int) async {
        calls.append(.stopForward)
        stoppedForwards.append(localPort)
    }

    func forwardEvents() -> AsyncStream<(localPort: Int, message: String)> {
        let (stream, sink) = AsyncStream.makeStream(of: (localPort: Int, message: String).self)
        let id = UUID()
        subscribers[id] = sink
        sink.onTermination = { [weak self] _ in
            Task { @MainActor in self?.subscribers[id] = nil }
        }
        return stream
    }

    private func change(_ paths: [String], _ edit: (inout GitStatus.File) -> Void) {
        for index in current.files.indices where paths.contains(current.files[index].path) {
            edit(&current.files[index])
        }
    }
}
#endif
