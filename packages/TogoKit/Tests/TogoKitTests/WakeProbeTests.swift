import Foundation
import Synchronization
import NIOCore
import Testing
@testable import TogoKit

struct WakeProbeTests {
    final class Counter: Sendable {
        let state = Mutex<(answers: [Bool], attempts: Int, waking: [String])>(([], 0, []))
    }

    @Test func wakeProbeRetriesThenSucceeds() async throws {
        let clock = TestClock()
        let counter = Counter()
        counter.state.withLock { $0.answers = [false, false, true] }
        try await WakeProbe.reach(
            host: "h", port: 22, clock: clock,
            onWaking: { host in counter.state.withLock { $0.waking.append(host) } },
            attempt: { counter.state.withLock { $0.attempts += 1; return $0.answers.removeFirst() } }
        )
        #expect(counter.state.withLock { $0.waking } == ["h"])
        #expect(counter.state.withLock { $0.attempts } == 3)
    }

    @Test func wakeProbeGivesUpAfterThirtySeconds() async {
        let clock = TestClock()
        await #expect(throws: SSHError.unreachable("h did not answer for 30 s; it may be asleep")) {
            try await WakeProbe.reach(host: "h", port: 22, clock: clock, onWaking: { _ in }, attempt: { false })
        }
        #expect(clock.sleeps.reduce(.zero, +) == .seconds(30))
    }

    @Test func firstAnswerSkipsWaking() async throws {
        let waking = Mutex(0)
        try await WakeProbe.reach(
            host: "h", port: 22, clock: TestClock(), onWaking: { _ in waking.withLock { $0 += 1 } }, attempt: { true }
        )
        #expect(waking.withLock { $0 } == 0)
    }

    /// A machine that turns the port away is awake: the probe stops waiting, and the SSH
    /// connect that follows says Remote Login is off instead of "it may be asleep".
    @Test func tcpConnectToARefusingPortCountsAsAwake() async {
        #expect(await WakeProbe.tcpConnect(host: "127.0.0.1", port: 1, timeout: .seconds(2)) == true)
    }

    @Test func refusedConnectsAreFoundInsideWrappedErrors() {
        struct PerAddress: Error { let error: any Error }
        struct Wrapped: Error { let connectionErrors: [PerAddress] }
        let refused = IOError(errnoCode: ECONNREFUSED, reason: "connect")
        #expect(SSHConnection.isRefused(refused))
        #expect(SSHConnection.isRefused(Wrapped(connectionErrors: [PerAddress(error: refused)])))
        #expect(!SSHConnection.isRefused(Wrapped(connectionErrors: [PerAddress(error: IOError(errnoCode: ETIMEDOUT, reason: "connect"))])))
        #expect(SSHConnection.networkError(refused) == .refused)
    }
}
