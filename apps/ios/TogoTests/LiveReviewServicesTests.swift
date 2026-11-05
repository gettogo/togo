import Darwin
import Foundation
import Synchronization
import Testing
import TogoKit
@testable import Togo

@MainActor @Suite(.timeLimit(.minutes(1))) struct LiveReviewServicesTests {
    private let subject = ReviewSubject(
        hostID: Samples.studio, sessionID: "s_abcdefghij", bib: "01", title: "api", cwd: "/Users/sam/src/api",
        managed: true, capabilities: [Capability.git, Capability.gitWrite, Capability.ports]
    )

    private func services(helper: ScriptedHelper = ScriptedHelper(), opener: RefusingOpener = RefusingOpener()) -> LiveReviewServices {
        services(helper: helper) { opener.count(); return opener }
    }

    private func services(
        helper: ScriptedHelper = ScriptedHelper(), opener: @escaping @Sendable () -> any TCPChannelOpener
    ) -> LiveReviewServices {
        let record = HostRecord(
            id: Samples.studio,
            profile: HostProfile(
                id: Samples.studio, name: "Studio", endpoint: HostEndpoint(host: "studio.local", port: 22, user: "sam"), auth: .password
            ),
            symbol: "desktop"
        )
        return LiveReviewServices(
            channels: helper, records: { $0 == record.id ? record : nil }, opener: { _ in opener() }
        )
    }

    private func port(_ number: Int) -> ListeningPort {
        ListeningPort(port: number, address: "127.0.0.1", pid: 4121, command: "node")
    }

    @Test func untrackedDiffNeverCallsHelper() async {
        let helper = ScriptedHelper()
        let services = services(helper: helper)
        await #expect(throws: HelperError.malformedResponse) { try await services.diff(subject, .untracked(path: "Notes.md")) }
        #expect(helper.requests.isEmpty)
        #expect(helper.calls == 0)
    }

    @Test func listDirWithoutRefJoinsCwd() async throws {
        let helper = ScriptedHelper()
        helper.answer("fs.listDir", #"{"path":"/Users/sam/src/api/Sources/App","entries":[]}"#)
        let services = services(helper: helper)
        _ = try await services.listDir(subject, path: "Sources/App", ref: nil)
        _ = try await services.listDir(subject, path: "", ref: nil)
        let paths = helper.requests.filter { $0.method == "fs.listDir" }.map { $0.params["path"] }
        #expect(paths == [.string("/Users/sam/src/api/Sources/App"), .string("/Users/sam/src/api")])
        #expect(helper.requests.allSatisfy { $0.params["ref"] == nil && $0.params["id"] == nil })
        #expect(helper.requests.allSatisfy { $0.params["includeFiles"] == .bool(true) })
    }

    @Test func listDirWithRefSendsIdAndRef() async throws {
        let helper = ScriptedHelper()
        helper.answer("fs.listDir", #"{"path":"Sources","entries":[{"name":"App","dir":true}]}"#)
        let services = services(helper: helper)
        let list = try await services.listDir(subject, path: "Sources", ref: "a1b2c3d")
        #expect(list.entries == [DirEntry(name: "App", dir: true)])
        let request = try #require(helper.requests.first { $0.method == "fs.listDir" })
        #expect(request.params["path"] == .string("Sources"))
        #expect(request.params["id"] == .string("s_abcdefghij"))
        #expect(request.params["ref"] == .string("a1b2c3d"))
        #expect(request.params["includeFiles"] == .bool(true))
    }

    @Test func repositoryPathsGoFromTheRoot() async throws {
        let helper = ScriptedHelper()
        helper.answer("fs.read", #"{"path":"apps/web/a.ts","size":1,"encoding":"utf8","content":"x","truncated":false}"#)
        helper.answer("fs.listDir", #"{"path":"/r/apps/tools","entries":[]}"#)
        let services = services(helper: helper)
        _ = try await services.read(subject, path: "apps/web/a.ts", ref: nil, base: .repository)
        _ = try await services.read(subject, path: "a.ts", ref: nil, base: .session)
        _ = try await services.listDir(subject, path: "apps/tools", ref: nil, base: .repository)
        let reads = helper.requests.filter { $0.method == "fs.read" }
        #expect(reads.map { $0.params["root"] } == [.bool(true), nil])
        let list = try #require(helper.requests.first { $0.method == "fs.listDir" })
        #expect(list.params["path"] == .string("apps/tools"))
        #expect(list.params["root"] == .bool(true))
        #expect(list.params["id"] == .string("s_abcdefghij"))

        // A helper without writes reports status from the session folder, so nothing changes.
        var older = subject
        older.capabilities = [Capability.git]
        _ = try await services.read(older, path: "a.ts", ref: nil, base: .repository)
        #expect(helper.requests.filter { $0.method == "fs.read" }.last?.params["root"] == nil)
    }

    @Test func conflictPassesThrough() async throws {
        let helper = ScriptedHelper()
        helper.fail("git.stage", code: RPCErrorCode.conflict, data: #"{"reason":"stale"}"#)
        let services = services(helper: helper)
        let seen = ["App.swift": GitSeen(status: ".M", fingerprint: "blob:ab12")]
        await #expect(throws: HelperError.conflict("stale")) {
            try await services.stage(subject, paths: ["App.swift"], seen: seen)
        }
        let request = try #require(helper.requests.first { $0.method == "git.stage" })
        #expect(request.params["seen"] == .object([
            "App.swift": .object(["status": .string(".M"), "fingerprint": .string("blob:ab12")]),
        ]))
    }

    @Test func forwardLimitIsFour() async throws {
        let services = services()
        var opened: [ForwardInfo] = []
        for number in 3000..<3004 {
            opened.append(try await services.forward(subject, port: port(number)))
        }
        #expect(Set(opened.map(\.localPort)).count == 4)
        await #expect(throws: LiveReviewServices.ForwardError.limit) { try await services.forward(subject, port: port(3004)) }
        // The same server again is the forward already open, not a fifth.
        #expect(try await services.forward(subject, port: port(3001)) == opened[1])
        await services.stopForward(localPort: opened[0].localPort)
        _ = try await services.forward(subject, port: port(3004))
        for info in opened.dropFirst() { await services.stopForward(localPort: info.localPort) }
    }

    @Test func backgroundStopsForwards() async throws {
        let services = services()
        let info = try await services.forward(subject, port: port(3000))
        #expect(info.url == URL(string: "http://127.0.0.1:\(info.localPort)/"))
        #expect(LoopbackProbe.accepts(info.localPort))
        services.setForeground(false)
        await services.settle()
        #expect(!LoopbackProbe.accepts(info.localPort))
        #expect(services.openLocalPorts.isEmpty)
    }

    @Test func forwardReopensOnSameLocalPort() async throws {
        let opener = RefusingOpener()
        let services = services(opener: opener)
        let info = try await services.forward(subject, port: port(3000))
        services.setForeground(false)
        services.setForeground(true)
        await services.settle()
        #expect(services.openLocalPorts == [info.localPort])
        #expect(LoopbackProbe.accepts(info.localPort))
        #expect(opener.opens == 2)
        await services.stopForward(localPort: info.localPort)
        #expect(!LoopbackProbe.accepts(info.localPort))
    }

    /// The helper connections close as the app leaves, while the forwards are still stopping.
    @Test func connectionClosingDuringPauseReopensEveryForward() async throws {
        let openers = Openers()
        let services = services { openers.next() }
        let first = try await services.forward(subject, port: port(3000))
        let second = try await services.forward(subject, port: port(3001))
        services.setForeground(false)
        openers.dropAll()
        await services.settle()
        #expect(services.openLocalPorts.isEmpty)
        services.setForeground(true)
        await services.settle()
        #expect(services.openLocalPorts == [first.localPort, second.localPort])
        #expect(LoopbackProbe.accepts(first.localPort) && LoopbackProbe.accepts(second.localPort))
        await services.stopForward(localPort: first.localPort)
        await services.stopForward(localPort: second.localPort)
    }

    @Test func refusedUpstreamBecomesMessage() async throws {
        let opener = RefusingOpener()
        let services = services(opener: opener)
        let events = services.forwardEvents()
        let other = services.forwardEvents()
        let info = try await services.forward(subject, port: port(3000))
        // A browser asking for the page; it stays connected until the forward answers.
        let browser = try #require(LoopbackProbe.connect(info.localPort))
        defer { close(browser) }
        _ = "GET / HTTP/1.1\r\n\r\n".withCString { send(browser, $0, strlen($0), 0) }
        var iterator = events.makeAsyncIterator()
        let event = try #require(await iterator.next())
        #expect(opener.dialCount == 1)
        #expect(event.localPort == info.localPort)
        #expect(event.message == "Nothing is listening on 3000 any more")
        // Every reader gets its own copy.
        var otherIterator = other.makeAsyncIterator()
        let copy = try #require(await otherIterator.next())
        #expect(copy.localPort == info.localPort && copy.message == event.message)
        await services.stopForward(localPort: info.localPort)
    }

    @Test func cancelledReaderStopsListening() async {
        let services = services()
        let reader = Task { for await _ in services.forwardEvents() {} }
        let kept = services.forwardEvents()
        let started = ContinuousClock.now + .seconds(2)
        while services.eventSubscriberCount != 2, ContinuousClock.now < started {
            try? await Task.sleep(for: .milliseconds(10))
        }
        #expect(services.eventSubscriberCount == 2)
        reader.cancel()
        _ = await reader.value
        let deadline = ContinuousClock.now + .seconds(2)
        while services.eventSubscriberCount != 1, ContinuousClock.now < deadline {
            try? await Task.sleep(for: .milliseconds(10))
        }
        #expect(services.eventSubscriberCount == 1)
        _ = kept
    }

    @Test func nonLoopbackServerIsNotForwarded() async {
        let services = services()
        let lan = ListeningPort(port: 3000, address: "192.168.1.4", pid: 1, command: "node")
        await #expect(throws: LiveReviewServices.ForwardError.notLoopback(lan)) { try await services.forward(subject, port: lan) }
        #expect(LiveReviewServices.ForwardError.notLoopback(lan).localizedDescription.hasPrefix("Port 3000 only listens on 192.168.1.4"))
    }

    @Test func diffSourcesMapToParams() async throws {
        let helper = ScriptedHelper()
        helper.answer("git.diff", #"{"diff":"","truncated":false,"binary":[]}"#)
        let services = services(helper: helper)
        _ = try await services.diff(subject, .worktree(path: "a.txt"))
        _ = try await services.diff(subject, .staged(path: "a.txt"))
        let hash = String(repeating: "c", count: 40)
        _ = try await services.diff(subject, .commit(hash: hash, path: nil))
        let params = helper.requests.filter { $0.method == "git.diff" }.map(\.params)
        #expect(params.count == 3)
        #expect(params[0]["path"] == .string("a.txt") && params[0]["staged"] != .bool(true))
        #expect(params[1]["staged"] == .bool(true))
        #expect(params[2]["commit"] == .string(hash) && params[2]["path"] == nil)
    }
}

/// A helper channel whose far end answers each method from a script. Requests are recorded.
final class ScriptedHelper: HelperCalling {
    struct Request: Sendable {
        let method: String
        let params: [String: JSONValue]
    }

    private let script = Mutex<[String: String]>([:])
    private let log = Mutex<(requests: [Request], calls: Int)>(([], 0))
    private let client: HelperClient
    private let ready = Mutex<Task<Void, any Error>?>(nil)

    init() {
        let channel = ScriptedChannel()
        client = HelperClient(channel: channel, callTimeout: .seconds(5))
        channel.respond = { [unowned self] id, method, params in
            log.withLock { $0.requests.append(Request(method: method, params: params)) }
            if method == "hello" {
                return #"{"jsonrpc":"2.0","id":\#(id),"result":{"protocol":1,"helper":{"version":"t","os":"darwin","arch":"arm64","hostname":"h"},"capabilities":["session.v1","fs.v1","git.v1","gitwrite.v1","ports.v1"]}}"#
            }
            let body = script.withLock { $0[method] } ?? #""result":{}"#
            return #"{"jsonrpc":"2.0","id":\#(id),\#(body)}"#
        }
    }

    var requests: [Request] { log.withLock { $0.requests.filter { $0.method != "hello" } } }
    var calls: Int { log.withLock { $0.calls } }

    func answer(_ method: String, _ result: String) {
        script.withLock { $0[method] = #""result":\#(result)"# }
    }

    func fail(_ method: String, code: Int, data: String) {
        script.withLock { $0[method] = #""error":{"code":\#(code),"message":"refused","data":\#(data)}"# }
    }

    func call<T: Sendable>(
        _ record: HostRecord, prompter: (any ConnectionPrompter)?, _ body: @Sendable (HelperClient) async throws -> T
    ) async throws -> T {
        log.withLock { $0.calls += 1 }
        let client = self.client
        let hello = ready.withLock { task in
            if let task { return task }
            let made = Task { _ = try await client.hello(client: ClientInfo(name: "togo-tests", version: "0")) }
            task = made
            return made
        }
        try await hello.value
        return try await body(client)
    }

    func drop(_ id: UUID) async {}
    func dropAll() async {}
}

/// Answers every frame the client writes with the frame `respond` builds for it.
private final class ScriptedChannel: ExecChannel, @unchecked Sendable {
    let incoming = ByteStream()
    private let decoder = Mutex(FrameDecoder())
    /// Set once before the client sends anything.
    var respond: @Sendable (Int, String, [String: JSONValue]) -> String = { _, _, _ in "" }

    private struct Wire: Decodable {
        let id: Int
        let method: String
        let params: [String: JSONValue]?
    }

    func write(_ data: Data) async throws {
        for frame in try decoder.withLock({ try $0.append(data) }) {
            let request = try JSONDecoder().decode(Wire.self, from: frame)
            let reply = respond(request.id, request.method, request.params ?? [:])
            _ = await incoming.write(try Frame.encode(Data(reply.utf8)))
        }
    }

    func close() async {
        incoming.finish()
    }
}

/// An SSH connection that never goes away and whose machine refuses every forwarded connection.
final class RefusingOpener: TCPChannelOpener {
    private let made = Mutex(0)

    var opens: Int { made.withLock { $0 } }

    func count() {
        made.withLock { $0 += 1 }
    }

    private let dials = Mutex(0)
    var dialCount: Int { dials.withLock { $0 } }

    func openDirectTCPIP(host: String, port: Int) async throws -> any ExecChannel {
        dials.withLock { $0 += 1 }
        throw DirectTCPIPError.refused
    }

    func waitUntilClosed() async {
        await withCheckedContinuation { (_: CheckedContinuation<Void, Never>) in }
    }
}

/// One SSH connection per forward opened; the test can close them all at once.
final class Openers: Sendable {
    private let made = Mutex<[DroppableOpener]>([])

    func next() -> any TCPChannelOpener {
        let opener = DroppableOpener()
        made.withLock { $0.append(opener) }
        return opener
    }

    func dropAll() {
        for opener in made.withLock({ $0 }) { opener.drop() }
    }
}

final class DroppableOpener: TCPChannelOpener {
    private let state = Mutex((closed: false, waiters: [CheckedContinuation<Void, Never>]()))

    func openDirectTCPIP(host: String, port: Int) async throws -> any ExecChannel {
        throw DirectTCPIPError.refused
    }

    func waitUntilClosed() async {
        await withCheckedContinuation { continuation in
            let closed = state.withLock { state in
                if !state.closed { state.waiters.append(continuation) }
                return state.closed
            }
            if closed { continuation.resume() }
        }
    }

    func drop() {
        let waiters = state.withLock { state in
            state.closed = true
            defer { state.waiters = [] }
            return state.waiters
        }
        for waiter in waiters { waiter.resume() }
    }
}

enum LoopbackProbe {
    /// Whether something accepts a TCP connection on 127.0.0.1 at `port`.
    static func accepts(_ port: Int) -> Bool {
        guard let fd = connect(port) else { return false }
        close(fd)
        return true
    }

    /// A connected socket the caller closes, or nil when the connection is refused.
    static func connect(_ port: Int) -> Int32? {
        let fd = socket(AF_INET, SOCK_STREAM, 0)
        guard fd >= 0 else { return nil }
        var address = sockaddr_in()
        address.sin_family = sa_family_t(AF_INET)
        address.sin_port = in_port_t(UInt16(port).bigEndian)
        inet_pton(AF_INET, "127.0.0.1", &address.sin_addr)
        let connected = withUnsafePointer(to: &address) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                Darwin.connect(fd, $0, socklen_t(MemoryLayout<sockaddr_in>.size)) == 0
            }
        }
        guard connected else {
            close(fd)
            return nil
        }
        return fd
    }
}
