import Foundation
import Synchronization
@testable import TogoKit

/// A clock whose sleepers wait until the test moves time past their deadline.
final class ManualClock: LinkClock {
    private struct Sleeper {
        let id: Int
        let deadline: Duration
        let continuation: CheckedContinuation<Void, any Error>
    }

    private struct State {
        var offset: Duration = .zero
        var nextID = 0
        var sleepers: [Sleeper] = []
    }

    private let state = Mutex(State())
    private let base = ContinuousClock.now

    var now: ContinuousClock.Instant { base + state.withLock { $0.offset } }

    func sleep(for duration: Duration) async throws {
        try Task.checkCancellation()
        guard duration > .zero else { return }
        let id = state.withLock { state in
            defer { state.nextID += 1 }
            return state.nextID
        }
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { continuation in
                let cancelled = state.withLock { state in
                    if Task.isCancelled { return true }
                    state.sleepers.append(Sleeper(id: id, deadline: state.offset + duration, continuation: continuation))
                    return false
                }
                if cancelled { continuation.resume(throwing: CancellationError()) }
            }
        } onCancel: {
            let sleeper = state.withLock { state in
                state.sleepers.firstIndex { $0.id == id }.map { state.sleepers.remove(at: $0) }
            }
            sleeper?.continuation.resume(throwing: CancellationError())
        }
    }

    /// How long each waiting sleeper has left, soonest first.
    var pending: [Duration] {
        state.withLock { state in state.sleepers.map { $0.deadline - state.offset }.sorted() }
    }

    func advance(_ duration: Duration) {
        let due = state.withLock { state in
            state.offset += duration
            let due = state.sleepers.filter { $0.deadline <= state.offset }
            state.sleepers.removeAll { $0.deadline <= state.offset }
            return due
        }
        for sleeper in due { sleeper.continuation.resume() }
    }

    /// Waits for a sleeper of exactly `duration` and lets it run.
    func runSleep(_ duration: Duration) async -> Bool {
        let waiting = await eventually { self.pending.first == duration }
        if waiting { advance(duration) }
        return waiting
    }
}

/// A source the test scripts: what `open` reports, the snapshots handed out in order
/// (the last one repeats), and the events pushed. `drop()` ends the event stream the way
/// a closed transport does.
final class FakeSource: SessionSource {
    private struct State {
        var snapshots: [EventSnapshot]
        var snapshotCalls = 0
        var foregroundFlags: [Bool] = []
        var ops: [String] = []
        var isClosed = false
        var snapshotError: (any Error)?
    }

    let events: AsyncStream<EventEnvelope>
    private let sink: AsyncStream<EventEnvelope>.Continuation
    private let presence: HelperPresence
    private let capabilities: Set<String>
    private let snapshotGate: Gate?
    private let state: Mutex<State>

    /// With a `snapshotGate`, every snapshot waits until the gate opens.
    init(
        snapshots: [EventSnapshot], capabilities: Set<String> = [Capability.events, Capability.approvals],
        presence: HelperPresence = .present(version: "0.3.0"), snapshotGate: Gate? = nil
    ) {
        (events, sink) = AsyncStream<EventEnvelope>.makeStream()
        self.presence = presence
        self.snapshotGate = snapshotGate
        self.capabilities = capabilities
        state = Mutex(State(snapshots: snapshots))
        if !capabilities.contains(Capability.events) { sink.finish() }
    }

    func open() async throws -> (HelperPresence, Set<String>) { (presence, capabilities) }

    func snapshot() async throws -> EventSnapshot {
        if let snapshotGate { await snapshotGate.passIfOpen() }
        return try state.withLock { state in
            state.snapshotCalls += 1
            if let error = state.snapshotError { throw error }
            return state.snapshots.count > 1 ? state.snapshots.removeFirst() : state.snapshots[0]
        }
    }

    func setForeground(_ on: Bool) async {
        state.withLock { $0.foregroundFlags.append(on) }
    }

    func call(_ op: SessionOp) async throws -> Session? {
        state.withLock { $0.ops.append("\(op)") }
        if case .create(let params) = op { return MonitorFixtures.session(params.name ?? "new") }
        return nil
    }

    func close() async {
        state.withLock { $0.isClosed = true }
        sink.finish()
    }

    func emit(_ envelope: EventEnvelope) { sink.yield(envelope) }
    func drop() { sink.finish() }
    func failSnapshots(with error: any Error) { state.withLock { $0.snapshotError = error } }

    var snapshotCalls: Int { state.withLock { $0.snapshotCalls } }
    var foregroundFlags: [Bool] { state.withLock { $0.foregroundFlags } }
    var isClosed: Bool { state.withLock { $0.isClosed } }
}

/// Hands out one scripted source or error per connect; past the script every connect fails.
final class ScriptedFactory: Sendable {
    enum Step: Sendable {
        case source(FakeSource)
        case fail(any Error)
        /// Fails once the gate opens, to model a connect that is still running.
        case failLater(Gate, any Error)
    }

    private let state: Mutex<(steps: [Step], calls: Int)>

    init(_ steps: [Step]) {
        state = Mutex((steps, 0))
    }

    var calls: Int { state.withLock { $0.calls } }

    var factory: SourceFactory {
        { [self] _ in
            let step = state.withLock { state in
                state.calls += 1
                return state.steps.isEmpty ? nil : state.steps.removeFirst()
            }
            switch step {
            case .source(let source)?: return source
            case .fail(let error)?: throw error
            case .failLater(let gate, let error)?:
                await gate.passIfOpen()
                throw error
            case nil: throw SSHError.timeout
            }
        }
    }
}

/// Keeps the newest snapshot a monitor published.
final class SnapshotLog: Sendable {
    private let latest = Mutex<HostSnapshot?>(nil)

    init(_ monitor: HostMonitor) {
        let stream = monitor.snapshots
        Task { [self] in for await snapshot in stream { latest.withLock { $0 = snapshot } } }
    }

    var last: HostSnapshot? { latest.withLock { $0 } }
    var reach: HostSnapshot.Reach? { last?.reach }
    var sessionIDs: [String] { last?.sessions.map(\.id) ?? [] }
}

enum MonitorFixtures {
    static let record = HostRecord(
        id: UUID(),
        profile: HostProfile(
            id: UUID(), name: "work-mac", endpoint: HostEndpoint(host: "work-mac.local", user: "dev"), auth: .password
        ),
        symbol: "laptopcomputer"
    )
    static let epoch = Date(timeIntervalSince1970: 1_790_000_000)

    static func session(_ id: String, name: String? = nil, status: SessionStatus = .working) -> Session {
        Session(
            id: id, bib: nil, name: name ?? id, agent: "claude", cwd: "/Users/dev/src", createdAt: epoch,
            status: status, activity: nil, attachedClients: 0, managed: true
        )
    }

    static func approval(_ requestId: String, title: String = "Run tests?") -> Approval {
        Approval(
            requestId: requestId, sessionId: "s1", bib: nil, agent: "claude", source: .hook, title: title,
            detail: nil, snippet: "npm test", options: [ApprovalOption(n: 1, label: "Yes", role: .approve)],
            allowText: false, createdAt: epoch, expiresAt: nil
        )
    }

    static func event(_ seq: Int, _ event: HelperEvent, sessionId: String? = nil) -> EventEnvelope {
        EventEnvelope(seq: seq, ts: epoch, sessionId: sessionId, event: event)
    }

    static func monitor(
        _ factory: ScriptedFactory, clock: ManualClock, path: FakePath = FakePath(), wall: WallClock = WallClock()
    ) -> HostMonitor {
        HostMonitor(
            record: record, factory: factory.factory, path: path, clock: clock,
            probe: { _, _, _ in }, now: { wall.date }
        )
    }
}

/// The wall-clock time a monitor stamps on `lastSeen`; the test moves it.
final class WallClock: Sendable {
    private let current = Mutex(MonitorFixtures.epoch)

    var date: Date { current.withLock { $0 } }
    func set(_ date: Date) { current.withLock { $0 = date } }
}

/// True if `condition` stays false for a short while; for asserting that nothing happens.
func stays(_ condition: () async -> Bool) async -> Bool {
    for _ in 0..<20 {
        if await condition() { return false }
        try? await Task.sleep(for: .milliseconds(5))
    }
    return true
}

extension MonitorFixtures {
}
