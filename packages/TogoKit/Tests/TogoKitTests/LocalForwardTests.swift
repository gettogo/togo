import Darwin
import Foundation
import Synchronization
import Testing
@testable import TogoKit

/// Stands in for the machine's end of a direct-tcpip channel.
final class FakeTCPChannel: ExecChannel {
    enum Mode: Sendable {
        /// Answers the first complete HTTP request with this body, then closes.
        case http(String)
        case echo
        /// Accepts bytes and never answers.
        case hold
    }

    let incoming = ByteStream()
    private let mode: Mode
    private let state = Mutex((received: Data(), answered: false, closed: false))

    init(mode: Mode) {
        self.mode = mode
    }

    var received: Data { state.withLock { $0.received } }
    var isClosed: Bool { state.withLock { $0.closed } }

    func write(_ data: Data) async throws {
        let request = state.withLock { state -> Bool in
            state.received += data
            guard !state.answered, state.received.range(of: Data("\r\n\r\n".utf8)) != nil else { return false }
            state.answered = true
            return true
        }
        switch mode {
        case .http(let body) where request:
            let response = "HTTP/1.1 200 OK\r\nContent-Length: \(body.utf8.count)\r\nConnection: close\r\n\r\n\(body)"
            _ = await incoming.write(Data(response.utf8))
            incoming.finish()
        case .echo:
            _ = await incoming.write(data)
        default:
            break
        }
    }

    func close() async {
        state.withLock { $0.closed = true }
        incoming.finish()
    }
}

final class FakeOpener: TCPChannelOpener {
    enum Behaviour: Sendable {
        case open(FakeTCPChannel.Mode)
        case fail(any Error & Sendable)
    }

    private let behaviour: Behaviour
    private let state = Mutex((targets: [String](), channels: [FakeTCPChannel]()))
    private let closed = Mutex((done: false, next: 0, waiters: [Int: CheckedContinuation<Void, Never>]()))

    init(_ behaviour: Behaviour) {
        self.behaviour = behaviour
    }

    var opens: Int { state.withLock { $0.targets.count } }
    var targets: [String] { state.withLock { $0.targets } }
    var channels: [FakeTCPChannel] { state.withLock { $0.channels } }

    func openDirectTCPIP(host: String, port: Int) async throws -> any ExecChannel {
        state.withLock { $0.targets.append("\(host):\(port)") }
        switch behaviour {
        case .fail(let error):
            throw error
        case .open(let mode):
            let channel = FakeTCPChannel(mode: mode)
            state.withLock { $0.channels.append(channel) }
            return channel
        }
    }

    /// Callers waiting in `waitUntilClosed()` now.
    var waiting: Int { closed.withLock { $0.waiters.count } }

    func waitUntilClosed() async {
        let id = closed.withLock { state in
            defer { state.next += 1 }
            return state.next
        }
        await withTaskCancellationHandler {
            await withCheckedContinuation { continuation in
                let done = closed.withLock { state in
                    if !state.done { state.waiters[id] = continuation }
                    return state.done
                }
                if done { continuation.resume() }
                if Task.isCancelled { closed.withLock { $0.waiters.removeValue(forKey: id) }?.resume() }
            }
        } onCancel: {
            closed.withLock { $0.waiters.removeValue(forKey: id) }?.resume()
        }
    }

    /// Drops the pretend SSH connection.
    func drop() {
        let waiters = closed.withLock { state in
            state.done = true
            defer { state.waiters = [:] }
            return state.waiters.values
        }
        for waiter in waiters { waiter.resume() }
    }
}

/// A blocking IPv4 TCP socket for driving a forward from a test.
final class LoopbackSocket {
    private let fd: Int32

    private init(fd: Int32) {
        self.fd = fd
    }

    deinit {
        Darwin.close(fd)
    }

    /// Nil when the connection is refused.
    static func connect(host: String = "127.0.0.1", port: Int) -> LoopbackSocket? {
        let fd = socket(AF_INET, SOCK_STREAM, 0)
        guard fd >= 0 else { return nil }
        var timeout = timeval(tv_sec: 5, tv_usec: 0)
        setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &timeout, socklen_t(MemoryLayout<timeval>.size))
        var address = sockaddr_in()
        address.sin_family = sa_family_t(AF_INET)
        address.sin_port = in_port_t(UInt16(port).bigEndian)
        inet_pton(AF_INET, host, &address.sin_addr)
        let result = withUnsafePointer(to: &address) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                Darwin.connect(fd, $0, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        guard result == 0 else {
            Darwin.close(fd)
            return nil
        }
        return LoopbackSocket(fd: fd)
    }

    /// A port nothing listens on right now. Without `range` the kernel picks one from its
    /// ephemeral range, which parallel tests also draw client ports from.
    static func freePort(in range: ClosedRange<Int>? = nil) -> Int {
        while true {
            if let port = bindable(range.map { Int.random(in: $0) } ?? 0) { return port }
        }
    }

    private static func bindable(_ port: Int) -> Int? {
        let fd = socket(AF_INET, SOCK_STREAM, 0)
        defer { Darwin.close(fd) }
        var address = sockaddr_in()
        address.sin_family = sa_family_t(AF_INET)
        address.sin_port = in_port_t(UInt16(port).bigEndian)
        inet_pton(AF_INET, "127.0.0.1", &address.sin_addr)
        var length = socklen_t(MemoryLayout<sockaddr_in>.size)
        let bound = withUnsafeMutablePointer(to: &address) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                bind(fd, $0, length) == 0 && getsockname(fd, $0, &length) == 0
            }
        }
        return bound ? Int(UInt16(bigEndian: address.sin_port)) : nil
    }

    /// The first IPv4 address of this Mac that is not loopback, if it has one.
    static func externalAddress() -> String? {
        var list: UnsafeMutablePointer<ifaddrs>?
        guard getifaddrs(&list) == 0 else { return nil }
        defer { freeifaddrs(list) }
        var cursor = list
        while let entry = cursor {
            defer { cursor = entry.pointee.ifa_next }
            guard let address = entry.pointee.ifa_addr, address.pointee.sa_family == sa_family_t(AF_INET),
                  entry.pointee.ifa_flags & UInt32(IFF_LOOPBACK) == 0, entry.pointee.ifa_flags & UInt32(IFF_UP) != 0
            else { continue }
            var text = [CChar](repeating: 0, count: Int(INET_ADDRSTRLEN))
            address.withMemoryRebound(to: sockaddr_in.self, capacity: 1) { inet in
                var raw = inet.pointee.sin_addr
                _ = inet_ntop(AF_INET, &raw, &text, socklen_t(INET_ADDRSTRLEN))
            }
            return String(decoding: text.prefix { $0 != 0 }.map { UInt8(bitPattern: $0) }, as: UTF8.self)
        }
        return nil
    }

    func send(_ text: String) {
        let bytes = Array(text.utf8)
        var sent = 0
        while sent < bytes.count {
            let n = bytes[sent...].withUnsafeBytes { Darwin.write(fd, $0.baseAddress, $0.count) }
            guard n > 0 else { return }
            sent += n
        }
    }

    /// Reads until `count` bytes arrived, the peer closed or five seconds passed.
    func read(count: Int) -> Data {
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 4096)
        while data.count < count {
            let n = Darwin.read(fd, &buffer, buffer.count)
            guard n > 0 else { break }
            data.append(contentsOf: buffer[0..<n])
        }
        return data
    }

    /// True when the peer closed the connection within five seconds.
    func readsEOF() -> Bool {
        var buffer = [UInt8](repeating: 0, count: 64)
        while true {
            let n = Darwin.read(fd, &buffer, buffer.count)
            if n == 0 { return true }
            if n < 0 { return errno == ECONNRESET }
        }
    }
}

struct LocalForwardTests {
    static let target = ForwardTarget(ListeningPort(port: 3000, address: "127.0.0.1", pid: 1, command: "node"))!

    @Test func wildcardTargetsLoopback() throws {
        for address in ["0.0.0.0", "*", ""] {
            let target = try #require(ForwardTarget(ListeningPort(port: 3000, address: address, pid: 1, command: "node")))
            #expect(target == Self.target)
            #expect(target.host == "127.0.0.1")
            #expect(target.port == 3000)
        }
        #expect(ForwardTarget(ListeningPort(port: 80, address: "localhost", pid: 1, command: "x"))?.host == "localhost")
    }

    @Test func ipv6LoopbackTarget() {
        for address in ["::", "[::]", "[::1]", "::1"] {
            #expect(ForwardTarget(ListeningPort(port: 5173, address: address, pid: 1, command: "vite"))?.host == "::1")
        }
    }

    @Test func nonLoopbackTargetsRejected() async {
        for address in ["10.0.0.5", "192.168.1.20", "fe80::1", "127.0.0.2", "example.com", "[::ffff:10.0.0.1]"] {
            #expect(ForwardTarget(ListeningPort(port: 3000, address: address, pid: 1, command: "node")) == nil)
        }
        for port in [0, -1, 65_536] {
            #expect(ForwardTarget(ListeningPort(port: port, address: "127.0.0.1", pid: 1, command: "node")) == nil)
        }
        // The SSH side refuses other hosts too, before any channel is asked for.
        let (connection, _) = SSHServerTests.connection(HostEndpoint(host: "127.0.0.1", port: 1, user: "u"))
        await #expect(throws: DirectTCPIPError.self) { try await connection.openDirectTCPIP(host: "10.0.0.5", port: 80) }
    }

    @Test func concurrentStartsShareOneListener() async throws {
        let forward = LocalForward(opener: FakeOpener(.open(.hold)), target: Self.target, preferredLocalPort: nil)
        async let first = forward.start()
        async let second = forward.start()
        let ports = try await [first, second]
        #expect(ports[0] == ports[1])
        await forward.stop()
        for port in Set(ports) { #expect(LoopbackSocket.connect(port: port) == nil) }
    }

    @Test func droppedConnectionClosesListener() async throws {
        let opener = FakeOpener(.open(.hold))
        let forward = LocalForward(opener: opener, target: Self.target, preferredLocalPort: nil)
        let port = try await forward.start()
        let socket = try #require(LoopbackSocket.connect(port: port))
        #expect(await eventually { opener.opens == 1 })
        opener.drop()
        var events = forward.events.makeAsyncIterator()
        #expect(await events.next() == .upstreamFailed(SSHError.channelClosed(exitStatus: nil).localizedDescription))
        #expect(await events.next() == .stopped)
        #expect(socket.readsEOF())
        #expect(LoopbackSocket.connect(port: port) == nil)
        #expect(opener.opens == 1)
    }

    @Test func prefersRequestedLocalPort() async throws {
        // Outside the ephemeral range, so no parallel test's client socket can take it meanwhile.
        let wanted = LoopbackSocket.freePort(in: 10_000...19_999)
        let forward = LocalForward(opener: FakeOpener(.open(.hold)), target: Self.target, preferredLocalPort: wanted)
        #expect(try await forward.start() == wanted)
        await forward.stop()
    }

    @Test func fallsBackWhenPortTaken() async throws {
        let first = LocalForward(opener: FakeOpener(.open(.hold)), target: Self.target, preferredLocalPort: nil)
        let taken = try await first.start()
        let second = LocalForward(opener: FakeOpener(.open(.hold)), target: Self.target, preferredLocalPort: taken)
        let port = try await second.start()
        #expect(port != taken)
        #expect(port > 0)
        await first.stop()
        await second.stop()
    }

    @Test func pipesBytesBothWays() async throws {
        let opener = FakeOpener(.open(.http("hello from the machine")))
        let forward = LocalForward(opener: opener, target: Self.target, preferredLocalPort: nil)
        let port = try await forward.start()
        let (body, response) = try await URLSession(configuration: .ephemeral)
            .data(from: try #require(URL(string: "http://127.0.0.1:\(port)/index.html")))
        #expect((response as? HTTPURLResponse)?.statusCode == 200)
        #expect(String(decoding: body, as: UTF8.self) == "hello from the machine")
        #expect(opener.targets == ["127.0.0.1:3000"])
        let request = String(decoding: try #require(opener.channels.first).received, as: UTF8.self)
        #expect(request.hasPrefix("GET /index.html HTTP/1.1\r\n"))
        await forward.stop()
    }

    @Test func parallelConnectionsGetOwnChannels() async throws {
        let opener = FakeOpener(.open(.http("ok")))
        let forward = LocalForward(opener: opener, target: Self.target, preferredLocalPort: nil)
        let port = try await forward.start()
        let session = URLSession(configuration: .ephemeral)
        let url = try #require(URL(string: "http://127.0.0.1:\(port)/"))
        let bodies = try await withThrowingTaskGroup(of: String.self) { group in
            for _ in 0..<6 {
                group.addTask { String(decoding: try await session.data(from: url).0, as: UTF8.self) }
            }
            return try await group.reduce(into: []) { $0.append($1) }
        }
        #expect(bodies == Array(repeating: "ok", count: 6))
        #expect(opener.opens == 6)
        await forward.stop()
    }

    @Test func upstreamRefusedReported() async throws {
        let forward = LocalForward(opener: FakeOpener(.fail(DirectTCPIPError.refused)), target: Self.target, preferredLocalPort: nil)
        let port = try await forward.start()
        let socket = try #require(LoopbackSocket.connect(port: port))
        socket.send("GET / HTTP/1.1\r\n\r\n")
        var events = forward.events.makeAsyncIterator()
        #expect(await events.next() == .upstreamRefused)
        #expect(socket.readsEOF())
        await forward.stop()
    }

    @Test func stopClosesEverything() async throws {
        let opener = FakeOpener(.open(.hold))
        let forward = LocalForward(opener: opener, target: Self.target, preferredLocalPort: nil)
        let port = try await forward.start()
        let sockets = try (0..<2).map { _ in try #require(LoopbackSocket.connect(port: port)) }
        #expect(await eventually { opener.channels.count == 2 })
        await forward.stop()
        #expect(opener.channels.allSatisfy { $0.isClosed })
        for socket in sockets { #expect(socket.readsEOF()) }
        #expect(LoopbackSocket.connect(port: port) == nil)
        var events = forward.events.makeAsyncIterator()
        #expect(await events.next() == .stopped)
        #expect(await events.next() == nil)
    }

    @Test func stopEndsConnectionWatch() async throws {
        let opener = FakeOpener(.open(.echo))
        let forward = LocalForward(opener: opener, target: Self.target, preferredLocalPort: nil)
        _ = try await forward.start()
        #expect(await eventually { opener.waiting == 1 })
        await forward.stop()
        #expect(await eventually { opener.waiting == 0 })
    }

    @Test func listensOnLoopbackOnly() async throws {
        let forward = LocalForward(opener: FakeOpener(.open(.echo)), target: Self.target, preferredLocalPort: nil)
        let port = try await forward.start()
        #expect(await forward.boundHost == "127.0.0.1")
        if let external = LoopbackSocket.externalAddress() {
            #expect(LoopbackSocket.connect(host: external, port: port) == nil)
        }
        let socket = try #require(LoopbackSocket.connect(port: port))
        socket.send("ping")
        #expect(socket.read(count: 4) == Data("ping".utf8))
        await forward.stop()
    }

    @Test func connectionsBeyondLimitAreRefused() async throws {
        let opener = FakeOpener(.open(.hold))
        let forward = LocalForward(opener: opener, target: Self.target, preferredLocalPort: nil)
        let port = try await forward.start()
        let held = try (0..<LocalForward.connectionLimit).map { _ in try #require(LoopbackSocket.connect(port: port)) }
        #expect(await eventually { opener.opens == LocalForward.connectionLimit })
        let extra = try #require(LoopbackSocket.connect(port: port))
        #expect(extra.readsEOF())
        #expect(opener.opens == LocalForward.connectionLimit)
        await forward.stop()
        #expect(held.count == LocalForward.connectionLimit)
    }

    @Test func lostConnectionStopsForward() async throws {
        let forward = LocalForward(
            opener: FakeOpener(.fail(SSHError.channelClosed(exitStatus: nil))), target: Self.target, preferredLocalPort: nil
        )
        let port = try await forward.start()
        let socket = try #require(LoopbackSocket.connect(port: port))
        var events = forward.events.makeAsyncIterator()
        #expect(await events.next() == .upstreamFailed(SSHError.channelClosed(exitStatus: nil).localizedDescription))
        #expect(await events.next() == .stopped)
        #expect(socket.readsEOF())
        #expect(LoopbackSocket.connect(port: port) == nil)
    }

    // MARK: Sign-in callbacks

    @Test func exactPortBindsThatPort() async throws {
        let wanted = LoopbackSocket.freePort(in: 20_000...29_999)
        let forward = LocalForward(
            opener: FakeOpener(.open(.hold)), target: Self.target, preferredLocalPort: wanted, exactLocalPort: true
        )
        #expect(try await forward.start() == wanted)
        await forward.stop()
    }

    @Test func exactPortFailsWhenTaken() async throws {
        let first = LocalForward(opener: FakeOpener(.open(.hold)), target: Self.target, preferredLocalPort: nil)
        let taken = try await first.start()
        let second = LocalForward(
            opener: FakeOpener(.open(.hold)), target: Self.target, preferredLocalPort: taken, exactLocalPort: true
        )
        await #expect(throws: (any Error).self) { try await second.start() }
        await first.stop()
        await second.stop()
    }

    @Test func acceptedConnectionsAreReported() async throws {
        let count = Mutex(0)
        let opener = FakeOpener(.open(.http("signed in")))
        let forward = LocalForward(
            opener: opener, target: Self.target, preferredLocalPort: nil, onAccept: { count.withLock { $0 += 1 } }
        )
        let port = try await forward.start()
        let url = try #require(URL(string: "http://127.0.0.1:\(port)/callback?code=abc"))
        _ = try await URLSession(configuration: .ephemeral).data(from: url)
        #expect(count.withLock { $0 } >= 1)
        // The callback goes down the SSH channel to the machine's own loopback port.
        #expect(opener.targets == ["127.0.0.1:3000"])
        await forward.stop()
    }
}
