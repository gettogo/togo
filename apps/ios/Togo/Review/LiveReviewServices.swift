import Foundation
import TogoKit

/// Review calls against a machine's helper, over the same helper connection the rest of the app
/// uses, and port previews over that connection's SSH session.
///
/// Calls the user makes by tapping, such as a write, a diff or a folder, may ask for a password or
/// code if the connection has to sign in again. Status and ports are refreshed on their own, so
/// they never ask.
///
/// Forwards live only in the foreground: going to the background stops every one, and coming
/// back opens each again on the local port it had, so a preview's address keeps working.
@MainActor final class LiveReviewServices: ReviewServices {
    enum ForwardError: Error, Equatable, LocalizedError {
        case limit
        /// The server listens on no loopback address, so the machine itself cannot reach it.
        case notLoopback(ListeningPort)

        var errorDescription: String? {
            switch self {
            case .limit:
                String(
                    localized: "review.forward.limit",
                    defaultValue: "Up to \(LiveReviewServices.forwardLimit) previews can be open at once. Close one first."
                )
            case .notLoopback(let port):
                String(
                    localized: "review.forward.notLoopback",
                    defaultValue: "Port \(String(port.port)) only listens on \(port.address), so it can't be previewed from here."
                )
            }
        }
    }

    nonisolated static let forwardLimit = 4

    private struct Forward {
        let id = UUID()
        let subject: ReviewSubject
        let port: ListeningPort
        let target: ForwardTarget
        let forward: LocalForward
        var events: Task<Void, Never>?
        /// A failure was already reported, so its end needs no second message.
        var failed = false
    }

    private struct ForwardKey: Hashable {
        let hostID: HostID
        let target: ForwardTarget
    }

    /// What a stopped forward needs to open again on the same local port.
    private struct Paused {
        let subject: ReviewSubject
        let port: ListeningPort
        let localPort: Int
    }

    private let channels: any HelperCalling
    private let records: (HostID) -> HostRecord?
    private let opener: (HostRecord) async throws -> any TCPChannelOpener
    private let prompter: (HostRecord) -> AttachPrompter?
    private var forwards: [Int: Forward] = [:]
    private var paused: [Paused] = []
    /// Forwards still starting, so a second tap joins the first instead of opening another.
    private var opening: [ForwardKey: Task<Int, any Error>] = [:]
    private var foreground = true
    /// The latest foreground change; each one waits for the one before.
    private var transition: Task<Void, Never>?
    /// One stream per `forwardEvents()` call, each ended when its reader stops.
    private var subscribers: [UUID: AsyncStream<(localPort: Int, message: String)>.Continuation] = [:]

    init(
        channels: any HelperCalling,
        records: @escaping (HostID) -> HostRecord?,
        opener: @escaping (HostRecord) async throws -> any TCPChannelOpener,
        prompter: @escaping (HostRecord) -> AttachPrompter? = { _ in nil }
    ) {
        self.channels = channels
        self.records = records
        self.opener = opener
        self.prompter = prompter
    }

    /// Local ports with a forward listening now.
    var openLocalPorts: Set<Int> { Set(forwards.keys) }

    // MARK: Git and files

    func status(_ s: ReviewSubject) async throws -> GitStatus {
        try await call(s, asking: false) { try await $0.gitStatus(id: s.sessionID) }
    }

    func diff(_ s: ReviewSubject, _ source: DiffSource) async throws -> GitDiff {
        let params: GitDiffParams
        switch source {
        case .worktree(let path): params = GitDiffParams(id: s.sessionID, path: path)
        case .staged(let path): params = GitDiffParams(id: s.sessionID, path: path, staged: true)
        case .commit(let hash, let path): params = GitDiffParams(id: s.sessionID, path: path, commit: hash)
        case .untracked: throw HelperError.malformedResponse
        }
        return try await call(s) { try await $0.gitDiff(params) }
    }

    func read(_ s: ReviewSubject, path: String, ref: String?, base: ReviewPathBase) async throws -> FsReadResult {
        let params = FsReadParams(id: s.sessionID, path: path, ref: ref, root: Self.fromRoot(s, base))
        return try await call(s) { try await $0.readFile(params) }
    }

    func listDir(_ s: ReviewSubject, path: String, ref: String?, base: ReviewPathBase) async throws -> ListDirResult {
        if Self.fromRoot(s, base) {
            return try await call(s) {
                try await $0.listRepoDir(path: path, showHidden: true, includeFiles: true, sessionID: s.sessionID, ref: ref)
            }
        }
        if let ref {
            return try await call(s) {
                try await $0.listDir(path: path, showHidden: true, includeFiles: true, sessionID: s.sessionID, ref: ref)
            }
        }
        let absolute = Self.join(s.cwd, path)
        return try await call(s) { try await $0.listDir(path: absolute, showHidden: true, includeFiles: true) }
    }

    func log(_ s: ReviewSubject, ref: String?, limit: Int) async throws -> [GitCommit] {
        try await call(s) { try await $0.gitLog(GitLogParams(id: s.sessionID, ref: ref, limit: limit)) }
    }

    func branches(_ s: ReviewSubject) async throws -> GitBranches {
        try await call(s) { try await $0.gitBranches(id: s.sessionID) }
    }

    func stage(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus {
        try await call(s) { try await $0.gitStage(id: s.sessionID, paths: paths, seen: seen) }
    }

    func unstage(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus {
        try await call(s) { try await $0.gitUnstage(id: s.sessionID, paths: paths, seen: seen) }
    }

    func discard(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus {
        try await call(s) { try await $0.gitDiscard(id: s.sessionID, paths: paths, seen: seen) }
    }

    func commit(_ s: ReviewSubject, message: String, amend: Bool, head: String?) async throws -> GitCommitResult {
        let params = GitCommitParams(id: s.sessionID, message: message, amend: amend, head: head)
        return try await call(s) { try await $0.gitCommit(params) }
    }

    func ports(_ s: ReviewSubject) async throws -> [ListeningPort] {
        try await call(s, asking: false) { try await $0.ports(id: s.sessionID) }
    }

    /// Whether a path goes to the helper as relative to the repository root. Only a helper with
    /// git writes reports status at the root; an older one reports it from the session folder,
    /// so its paths already read right from there.
    private static func fromRoot(_ s: ReviewSubject, _ base: ReviewPathBase) -> Bool {
        base == .repository && s.capabilities.contains(Capability.gitWrite)
    }

    /// `path` joined onto `cwd`; empty is `cwd` itself.
    static func join(_ cwd: String, _ path: String) -> String {
        let relative = path.drop { $0 == "/" }
        guard !relative.isEmpty else { return cwd }
        return cwd.hasSuffix("/") ? cwd + relative : cwd + "/" + relative
    }

    private func record(_ id: HostID) throws -> HostRecord {
        guard let record = records(id) else { throw LiveServiceError.unknownMachine }
        guard !record.noHelper else { throw LiveServiceError.needsHelper }
        return record
    }

    /// Questions the machine asks during a call leave when the call ends, however it ends.
    private func call<T: Sendable>(
        _ s: ReviewSubject, asking: Bool = true, _ body: @escaping @Sendable (HelperClient) async throws -> T
    ) async throws -> T {
        let record = try record(s.hostID)
        let prompter = asking && foreground ? prompter(record) : nil
        let attempt = prompter?.beginAttempt()
        do {
            let result = try await channels.call(record, prompter: prompter, body)
            if let prompter, let attempt { await prompter.endAttempt(attempt) }
            return result
        } catch {
            if let prompter, let attempt { await prompter.endAttempt(attempt) }
            throw error
        }
    }

    // MARK: Forwards

    func forward(_ s: ReviewSubject, port: ListeningPort) async throws -> ForwardInfo {
        guard let target = ForwardTarget(port) else { throw ForwardError.notLoopback(port) }
        let key = ForwardKey(hostID: s.hostID, target: target)
        if let open = forwards.first(where: { ForwardKey(hostID: $0.value.subject.hostID, target: $0.value.target) == key }) {
            return Self.info(open.value.port, open.key)
        }
        if let opening = opening[key] {
            return Self.info(port, try await opening.value)
        }
        guard forwards.count + paused.count + opening.count < Self.forwardLimit else { throw ForwardError.limit }
        let task = Task { try await self.open(s, port: port, target: target, preferred: nil) }
        opening[key] = task
        defer { opening[key] = nil }
        let localPort = try await task.value
        if !foreground {
            // The app went to the background while this one opened; it comes back with the rest.
            await stopAll(detach([localPort]))
        }
        return Self.info(port, localPort)
    }

    func stopForward(localPort: Int) async {
        paused.removeAll { $0.localPort == localPort }
        guard let open = forwards.removeValue(forKey: localPort) else { return }
        open.events?.cancel()
        await open.forward.stop()
    }

    func forwardEvents() -> AsyncStream<(localPort: Int, message: String)> {
        let (stream, sink) = AsyncStream.makeStream(
            of: (localPort: Int, message: String).self, bufferingPolicy: .bufferingNewest(32)
        )
        let id = UUID()
        subscribers[id] = sink
        sink.onTermination = { [weak self] _ in
            Task { @MainActor in self?.subscribers[id] = nil }
        }
        return stream
    }

    /// Number of readers still listening for forward events.
    var eventSubscriberCount: Int { subscribers.count }

    private func emit(_ localPort: Int, _ message: String) {
        for sink in subscribers.values {
            sink.yield((localPort, message))
        }
    }

    /// The app moved to or from the foreground. Changes apply in order. Going to the background
    /// records every open forward as paused at once, before anything can close under them.
    func setForeground(_ on: Bool) {
        guard on != foreground else { return }
        foreground = on
        let previous = transition
        let stopping = on ? [] : detach(Array(forwards.keys))
        transition = Task { [weak self] in
            await previous?.value
            guard let self else { return }
            if on { await self.resume() } else { await self.stopAll(stopping) }
        }
    }

    /// Returns once every foreground change made so far has taken effect.
    func settle() async {
        while let current = transition {
            await current.value
            if transition == current { return }
        }
    }

    /// Moves these forwards to `paused` and returns them for stopping. Synchronous, so no event
    /// can drop a forward in between.
    private func detach(_ localPorts: [Int]) -> [Forward] {
        localPorts.sorted().compactMap { localPort in
            guard let open = forwards.removeValue(forKey: localPort) else { return nil }
            paused.append(Paused(subject: open.subject, port: open.port, localPort: localPort))
            open.events?.cancel()
            return open
        }
    }

    private func stopAll(_ stopping: [Forward]) async {
        for open in stopping {
            await open.forward.stop()
        }
    }

    private func resume() async {
        let waiting = paused
        paused = []
        for entry in waiting {
            guard foreground else {
                paused.append(entry)
                continue
            }
            // A machine removed while the app was away takes its previews with it.
            guard records(entry.subject.hostID) != nil, let target = ForwardTarget(entry.port) else { continue }
            do {
                let localPort = try await open(entry.subject, port: entry.port, target: target, preferred: entry.localPort)
                if localPort != entry.localPort {
                    // A preview still shows the old address, which nothing serves now.
                    await stopForward(localPort: localPort)
                    emit(entry.localPort, Self.previewClosed)
                }
            } catch {
                emit(entry.localPort, error.localizedDescription)
            }
        }
    }

    private func open(_ s: ReviewSubject, port: ListeningPort, target: ForwardTarget, preferred: Int?) async throws -> Int {
        let record = try record(s.hostID)
        let forward = LocalForward(opener: try await opener(record), target: target, preferredLocalPort: preferred)
        let localPort = try await forward.start()
        var entry = Forward(subject: s, port: port, target: target, forward: forward)
        let id = entry.id
        entry.events = Task { [weak self] in
            for await event in forward.events {
                self?.handle(event, from: id, localPort: localPort, remote: port.port)
            }
        }
        forwards[localPort] = entry
        return localPort
    }

    /// Events from a forward this class stopped itself are not news to anyone.
    private func handle(_ event: ForwardEvent, from id: UUID, localPort: Int, remote: Int) {
        guard let open = forwards[localPort], open.id == id else { return }
        switch event {
        case .upstreamRefused:
            emit(localPort, ReviewCopy.nothingListening(remote))
        case .upstreamFailed(let detail):
            forwards[localPort]?.failed = true
            if foreground { emit(localPort, detail) }
        case .stopped:
            if !foreground {
                // Its connection closed as the app left; it reopens with the others.
                _ = detach([localPort])
            } else {
                forwards[localPort] = nil
                if !open.failed { emit(localPort, Self.previewClosed) }
            }
        }
    }

    private static func info(_ port: ListeningPort, _ localPort: Int) -> ForwardInfo {
        ForwardInfo(remote: port, localPort: localPort, url: URL(string: "http://127.0.0.1:\(localPort)/")!)
    }

    private static let previewClosed = String(
        localized: "review.forward.closed", defaultValue: "The preview stopped. Open it again to keep going."
    )
}
