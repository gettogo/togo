#if DEBUG
import Foundation
import Synchronization
import TogoKit

/// The repositories UI tests review. Session 04 (`app`) has three changes that one stage makes
/// ready to commit; every other session serves `ReviewFixtures`, untracked files included.
/// Previews open through a real `LocalForward`, whose channels end in a page served in-process.
@MainActor final class UITestReviewServices: ReviewServices {
    static let appSession = "s-04"

    static var appStatus: GitStatus {
        GitStatus(
            repo: true, root: "/Users/sam/src/app", branch: "main", ahead: 0, behind: 0, truncated: false,
            files: [
                ReviewFixtures.file("Sources/App.swift", ".", "M"),
                ReviewFixtures.file("Sources/Store.swift", "M", "."),
                ReviewFixtures.file("Sources/Theme.swift", "A", "."),
            ]
        )
    }

    /// Two changes far apart, so there is a next one to go to.
    static let appDiff = """
        diff --git a/Sources/App.swift b/Sources/App.swift
        index 3f2a1c4..8b7d0e9 100644
        --- a/Sources/App.swift
        +++ b/Sources/App.swift
        @@ -1,6 +1,7 @@
         import SwiftUI
        +import Observation
        \u{20}
         @main
         struct ItemsApp: App {
             @State private var store = Store()
        @@ -40,7 +41,7 @@ struct ItemsApp: App {
             var body: some Scene {
                 WindowGroup {
        -            ContentView()
        +            ContentView(store: store)
                 }
             }
         }

        """

    private let app = FakeReviewServices(status: UITestReviewServices.appStatus)
    private let other = FakeReviewServices()
    private let demo: FakeReviewServices = {
        let fake = FakeReviewServices(status: DemoAPIFixture.status)
        fake.portList = DemoAPIFixture.ports
        return fake
    }()
    private let opener = PageOpener()
    private var forwards: [Int: LocalForward] = [:]
    private var subscribers: [UUID: AsyncStream<(localPort: Int, message: String)>.Continuation] = [:]

    private func fake(_ s: ReviewSubject) -> FakeReviewServices {
        if DemoAPIFixture.enabled, s.sessionID == DemoAPIFixture.session { return demo }
        return s.sessionID == Self.appSession ? app : other
    }

    func status(_ s: ReviewSubject) async throws -> GitStatus { try await fake(s).status(s) }
    func diff(_ s: ReviewSubject, _ source: DiffSource) async throws -> GitDiff {
        if DemoAPIFixture.enabled, s.sessionID == DemoAPIFixture.session {
            _ = try await demo.diff(s, source)
            let path: String? = switch source {
            case .worktree(let path), .staged(let path), .untracked(let path): path
            case .commit(_, let path): path
            }
            return GitDiff(diff: path.flatMap { DemoAPIFixture.diffs[$0] } ?? "", truncated: false, binary: [])
        }
        if s.sessionID == Self.appSession, case .worktree("Sources/App.swift") = source {
            _ = try await app.diff(s, source)
            return GitDiff(diff: Self.appDiff, truncated: false, binary: [])
        }
        return try await fake(s).diff(s, source)
    }
    func read(_ s: ReviewSubject, path: String, ref: String?, base: ReviewPathBase) async throws -> FsReadResult {
        try await fake(s).read(s, path: path, ref: ref, base: base)
    }
    func listDir(_ s: ReviewSubject, path: String, ref: String?, base: ReviewPathBase) async throws -> ListDirResult {
        try await fake(s).listDir(s, path: path, ref: ref, base: base)
    }
    func log(_ s: ReviewSubject, ref: String?, limit: Int) async throws -> [GitCommit] {
        try await fake(s).log(s, ref: ref, limit: limit)
    }
    func branches(_ s: ReviewSubject) async throws -> GitBranches { try await fake(s).branches(s) }
    func stage(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus {
        try await fake(s).stage(s, paths: paths, seen: seen)
    }
    func unstage(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus {
        try await fake(s).unstage(s, paths: paths, seen: seen)
    }
    func discard(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus {
        try await fake(s).discard(s, paths: paths, seen: seen)
    }
    func commit(_ s: ReviewSubject, message: String, amend: Bool, head: String?) async throws -> GitCommitResult {
        try await fake(s).commit(s, message: message, amend: amend, head: head)
    }
    func ports(_ s: ReviewSubject) async throws -> [ListeningPort] { try await fake(s).ports(s) }

    func forward(_ s: ReviewSubject, port: ListeningPort) async throws -> ForwardInfo {
        guard let target = ForwardTarget(port) else { throw DirectTCPIPError.refused }
        let forward = LocalForward(opener: opener, target: target, preferredLocalPort: nil)
        let localPort = try await forward.start()
        forwards[localPort] = forward
        Task { [weak self] in
            for await event in forward.events where event == .upstreamRefused {
                self?.emit(localPort, ReviewCopy.nothingListening(port.port))
            }
        }
        return ForwardInfo(remote: port, localPort: localPort, url: PortsModel.loopbackURL(localPort))
    }

    func stopForward(localPort: Int) async {
        await forwards.removeValue(forKey: localPort)?.stop()
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

    private func emit(_ localPort: Int, _ message: String) {
        for sink in subscribers.values { sink.yield((localPort, message)) }
    }
}

/// Stands in for the machine's side of a forward: every connection reaches a server that
/// answers one request with a small page.
private final class PageOpener: TCPChannelOpener {
    func openDirectTCPIP(host: String, port: Int) async throws -> any ExecChannel {
        PageChannel(port: port)
    }

    /// The pretend SSH connection never drops; the wait ends only when its task is cancelled.
    func waitUntilClosed() async {
        try? await Task.sleep(for: .seconds(365 * 86_400))
    }
}

private final class PageChannel: ExecChannel {
    let incoming = ByteStream()
    private let port: Int
    private let request = Mutex((received: Data(), answered: false))

    init(port: Int) {
        self.port = port
    }

    func write(_ data: Data) async throws {
        let complete = request.withLock { state -> Bool in
            state.received += data
            guard !state.answered, state.received.range(of: Data("\r\n\r\n".utf8)) != nil else { return false }
            state.answered = true
            return true
        }
        guard complete else { return }
        if DemoAPIFixture.enabled {
            _ = await incoming.write(Data(DemoAPIFixture.rootResponse.utf8))
            incoming.finish()
            return
        }
        let body = """
            <!doctype html><meta name="viewport" content="width=device-width, initial-scale=1">\
            <title>Dev server</title><h1 style="font: 600 28px -apple-system">Running on \(port)</h1>
            """
        let response = "HTTP/1.1 200 OK\r\nContent-Type: text/html; charset=utf-8\r\n"
            + "Content-Length: \(body.utf8.count)\r\nConnection: close\r\n\r\n\(body)"
        _ = await incoming.write(Data(response.utf8))
        incoming.finish()
    }

    func close() async {
        incoming.finish()
    }
}
#endif
