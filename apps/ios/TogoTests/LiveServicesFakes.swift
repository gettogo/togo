import Foundation
import Synchronization
import TogoKit
@testable import Togo

/// Stands in for a machine's monitor: tests publish its snapshots and read what it was told.
final class FakeWatch: HostWatch {
    let snapshots: AsyncStream<HostSnapshot>
    let sink: AsyncStream<HostSnapshot>.Continuation
    let foregrounds = Mutex<[Bool]>([])
    let ops = Mutex<[String]>([])
    let stopped = Mutex(false)
    let record: HostRecord

    init(record: HostRecord) {
        self.record = record
        (snapshots, sink) = AsyncStream.makeStream(of: HostSnapshot.self)
    }

    func start() async {}
    func stop() async {
        stopped.withLock { $0 = true }
        sink.finish()
    }
    let refreshes = Mutex(0)

    func refresh() async {
        refreshes.withLock { $0 += 1 }
    }

    func setForeground(_ on: Bool) async {
        foregrounds.withLock { $0.append(on) }
    }

    func perform(_ op: SessionOp) async throws -> Session? {
        if case .answer(let id, _) = op { ops.withLock { $0.append("answer \(id)") } }
        return nil
    }
}

/// Machines whose saved password the services removed.
final class ForgottenPasswords: Sendable {
    let ids = Mutex<[HostID]>([])
}

/// The watches made so far, by machine.
final class Watches: Sendable {
    let made = Mutex<[UUID: FakeWatch]>([:])

    let history = Mutex<[FakeWatch]>([])

    subscript(id: UUID) -> FakeWatch? { made.withLock { $0[id] } }
    var all: [FakeWatch] { made.withLock { Array($0.values) } }
}

/// Never reaches a helper; records which machines' cached channels were dropped and whether
/// each call could ask the user anything. With `asks`, a call that may ask puts a password
/// question first, as a channel that has to sign in again would.
final class NoHelper: HelperCalling {
    struct Unreachable: Error {}
    let dropped = Mutex<[UUID]>([])
    let mayAsk = Mutex<[Bool]>([])
    let asks: Bool

    init(asks: Bool = false) {
        self.asks = asks
    }

    func call<T: Sendable>(
        _ record: HostRecord, prompter: (any ConnectionPrompter)?, _ body: @Sendable (HelperClient) async throws -> T
    ) async throws -> T {
        mayAsk.withLock { $0.append(prompter != nil) }
        if asks, let prompter {
            _ = await prompter.answer(.password(user: record.profile.endpoint.user, host: record.profile.endpoint.host))
        }
        throw Unreachable()
    }

    func drop(_ id: UUID) async {
        dropped.withLock { $0.append(id) }
    }

    func dropAll() async {}
}

final class CountingPrompter: ConnectionPrompter {
    let asked = Mutex(0)

    func answer(_ prompt: AuthPrompt) async -> AuthAnswer? {
        asked.withLock { $0 += 1 }
        return .secret("pw")
    }

    func trustNewHostKey(host: String, keyType: String, fingerprint: String) async -> Bool { false }
}
