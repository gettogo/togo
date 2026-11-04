import Foundation
@preconcurrency import LocalAuthentication
import Synchronization
import Testing
@testable import TogoKit

final class CountingEvaluator: BiometricEvaluator {
    let calls = Mutex(0)
    let fails: Bool

    init(fails: Bool = false) {
        self.fails = fails
    }

    func evaluate(_ context: LAContext, reason: String) async throws {
        calls.withLock { $0 += 1 }
        if fails { throw LAError(.userCancel) }
    }
}

struct BiometricSessionTests {
    @Test func biometricEvaluatedOnce() async throws {
        let evaluator = CountingEvaluator()
        let session = BiometricSession(evaluator: evaluator)
        let first = try await session.context(reason: "a")
        let second = try await session.context(reason: "b")
        #expect(first === second)
        #expect(evaluator.calls.withLock { $0 } == 1)
        await session.invalidate()
        let third = try await session.context(reason: "c")
        #expect(third !== first)
        #expect(evaluator.calls.withLock { $0 } == 2)
    }

    @Test func concurrentCallersShareOnePrompt() async throws {
        let evaluator = CountingEvaluator()
        let session = BiometricSession(evaluator: evaluator)
        async let a = session.context(reason: "a")
        async let b = session.context(reason: "b")
        _ = try await (a, b)
        #expect(evaluator.calls.withLock { $0 } == 1)
    }

    @Test func failedEvaluationIsNotCached() async {
        let evaluator = CountingEvaluator(fails: true)
        let session = BiometricSession(evaluator: evaluator)
        await #expect(throws: LAError.self) { try await session.context(reason: "a") }
        await #expect(throws: LAError.self) { try await session.context(reason: "a") }
        #expect(evaluator.calls.withLock { $0 } == 2)
    }

    final class ManualDate: Sendable {
        private let value = Mutex(Date(timeIntervalSince1970: 1_000_000))
        var now: Date { value.withLock { $0 } }
        func advance(_ seconds: TimeInterval) { value.withLock { $0 += seconds } }
    }

    @Test func longBackgroundClearsContextBeforeForegroundIsReported() async throws {
        let evaluator = CountingEvaluator()
        let date = ManualDate()
        let session = BiometricSession(evaluator: evaluator, now: { date.now })
        _ = try await session.context(reason: "sign")
        session.enteredBackground()
        date.advance(BiometricSession.backgroundLimit + 1)
        // A reconnect can ask for a signature before the app reports the foreground.
        _ = try await session.context(reason: "sign")
        #expect(evaluator.calls.withLock { $0 } == 2)
        session.enteredForeground()
        _ = try await session.context(reason: "sign")
        #expect(evaluator.calls.withLock { $0 } == 2)
    }

    @Test func shortBackgroundKeepsContext() async throws {
        let evaluator = CountingEvaluator()
        let date = ManualDate()
        let session = BiometricSession(evaluator: evaluator, now: { date.now })
        _ = try await session.context(reason: "sign")
        session.enteredBackground()
        date.advance(60)
        session.enteredForeground()
        date.advance(BiometricSession.backgroundLimit)
        _ = try await session.context(reason: "sign")
        #expect(evaluator.calls.withLock { $0 } == 1)
    }

    @Test func deviceLockClearsContext() async throws {
        let evaluator = CountingEvaluator()
        let session = BiometricSession(evaluator: evaluator)
        _ = try await session.context(reason: "sign")
        session.expire()
        _ = try await session.context(reason: "sign")
        #expect(evaluator.calls.withLock { $0 } == 2)
    }

    @Test func secretsRedacted() {
        #expect(!String(describing: AuthAnswer.secret("hunter2")).contains("hunter2"))
        #expect(!String(reflecting: AuthAnswer.responses(["123456"])).contains("123456"))
        #expect(!"\(AuthAnswer.secret("hunter2"))".contains("hunter2"))
    }
}
