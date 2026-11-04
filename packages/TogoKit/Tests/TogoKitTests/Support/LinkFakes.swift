import Foundation
import Synchronization
@testable import TogoKit

/// A transport whose calls are recorded and whose state and output the test drives.
actor FakeTransport: ByteTransport {
    enum Call: Equatable {
        case connect, send(Data), resize(Int, Int), close
    }

    nonisolated let incoming = ByteStream()
    nonisolated let state: AsyncStream<TransportState>
    private let stateSink: AsyncStream<TransportState>.Continuation
    private let connectError: (any Error)?
    private let resizeGate: Gate?
    private(set) var calls: [Call] = []

    /// With a `resizeGate`, `resize` is recorded at once and returns when the gate opens.
    init(connectError: (any Error)? = nil, resizeGate: Gate? = nil) {
        self.connectError = connectError
        self.resizeGate = resizeGate
        (state, stateSink) = AsyncStream<TransportState>.makeStream()
    }

    func connect() async throws {
        calls.append(.connect)
        if let connectError { throw connectError }
        stateSink.yield(.connected)
    }

    func send(_ data: Data) async throws { calls.append(.send(data)) }
    func resize(cols: Int, rows: Int) async throws {
        calls.append(.resize(cols, rows))
        if let resizeGate { await resizeGate.passIfOpen() }
    }

    func close() async {
        calls.append(.close)
        stateSink.yield(.closed(.requested))
        incoming.finish()
    }

    nonisolated func emit(_ state: TransportState) { stateSink.yield(state) }
    nonisolated func output(_ text: String) async { _ = await incoming.write(Data(text.utf8)) }
    var sent: [Data] { calls.compactMap { if case .send(let data) = $0 { data } else { nil } } }
    var resizes: [Call] { calls.filter { if case .resize = $0 { true } else { false } } }
    var isClosed: Bool { calls.contains(.close) }
}

/// Records each sleep and moves `now` forward by it instead of waiting.
final class TestClock: LinkClock {
    private let state = Mutex<(sleeps: [Duration], offset: Duration)>(([], .zero))
    private let base = ContinuousClock.now

    func sleep(for duration: Duration) async throws {
        state.withLock { $0.sleeps.append(duration); $0.offset += duration }
        await Task.yield()
        try Task.checkCancellation()
    }

    var now: ContinuousClock.Instant { base + state.withLock { $0.offset } }
    var sleeps: [Duration] { state.withLock { $0.sleeps } }
    func advance(_ duration: Duration) { state.withLock { $0.offset += duration } }
}

final class FakePath: NetworkPathSource {
    let changes: AsyncStream<Void>
    private let sink: AsyncStream<Void>.Continuation

    init() { (changes, sink) = AsyncStream<Void>.makeStream() }
    func change() { sink.yield() }
}

/// Hands out a scripted sequence of attachments, or errors, one per connect call.
final class ScriptedConnect: Sendable {
    enum Step: Sendable {
        case attach(FakeTransport, TransportKind)
        case fail(any Error)
    }

    private let state = Mutex<(steps: [Step], sizes: [(Int, Int)])>(([], []))
    /// Runs inside the connect call, before it returns, to model a change during connecting.
    let during: (@Sendable () async -> Void)?

    init(_ steps: [Step], during: (@Sendable () async -> Void)? = nil) {
        state.withLock { $0.steps = steps }
        self.during = during
    }

    var sizes: [(Int, Int)] { state.withLock { $0.sizes } }

    var closure: LinkConnect {
        { [self] cols, rows, _ in
            let step = state.withLock { state in
                state.sizes.append((cols, rows))
                return state.steps.isEmpty ? nil : state.steps.removeFirst()
            }
            if let during { await during() }
            switch step {
            case .attach(let transport, let kind)?:
                try await transport.connect()
                return Attachment(transport: transport, kind: kind, notes: [], connection: Self.connection())
            case .fail(let error)?:
                throw error
            case nil:
                throw SSHError.timeout
            }
        }
    }

    static func connection() -> SSHConnection {
        SSHServerTests.connection(HostEndpoint(host: "127.0.0.1", port: 1, user: "u")).0
    }
}

/// Polls until `condition` holds; the link works on its own tasks.
func eventually(_ timeout: Duration = .seconds(3), _ condition: () async -> Bool) async -> Bool {
    let deadline = ContinuousClock.now + timeout
    while ContinuousClock.now < deadline {
        if await condition() { return true }
        try? await Task.sleep(for: .milliseconds(5))
    }
    return await condition()
}
