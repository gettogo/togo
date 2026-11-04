import Foundation
import Synchronization
@preconcurrency import LocalAuthentication

public protocol BiometricEvaluator: Sendable {
    func evaluate(_ context: LAContext, reason: String) async throws
}

public struct SystemBiometricEvaluator: BiometricEvaluator {
    public init() {}

    public func evaluate(_ context: LAContext, reason: String) async throws {
        try await context.evaluatePolicy(.deviceOwnerAuthentication, localizedReason: reason)
    }
}

/// One evaluated `LAContext` shared by the resume lock and key signing, so one unlock
/// covers both and a reconnect inside the reuse window shows no prompt at all.
///
/// The context is dropped when the device locks (`expire()`) and after
/// `backgroundLimit` seconds in the background. The background check also runs inside
/// `context(reason:)`, because a reconnect can ask for a signature before the app has
/// reported that it is in the foreground again.
public actor BiometricSession {
    public static let backgroundLimit: TimeInterval = 600

    private struct Expiry {
        var backgroundedAt: Date?
        var expired = false
    }

    private let evaluator: any BiometricEvaluator
    private let reuse: TimeInterval
    private let now: @Sendable () -> Date
    private let expiry = Mutex(Expiry())
    private var evaluated: LAContext?
    private var pending: Task<LAContext, any Error>?

    public init(
        evaluator: any BiometricEvaluator = SystemBiometricEvaluator(), reuse: TimeInterval = 300,
        now: @escaping @Sendable () -> Date = { Date() }
    ) {
        self.evaluator = evaluator
        self.reuse = reuse
        self.now = now
    }

    /// Synchronous, so it takes effect before any signature that follows the lock.
    public nonisolated func expire() {
        expiry.withLock { $0.expired = true }
    }

    public nonisolated func enteredBackground() {
        let date = now()
        expiry.withLock { if $0.backgroundedAt == nil { $0.backgroundedAt = date } }
    }

    public nonisolated func enteredForeground() {
        let date = now()
        expiry.withLock { state in
            if let since = state.backgroundedAt, date.timeIntervalSince(since) >= Self.backgroundLimit {
                state.expired = true
            }
            state.backgroundedAt = nil
        }
    }

    public func context(reason: String) async throws -> LAContext {
        if takeExpiry() { invalidate() }
        if let evaluated { return evaluated }
        // Concurrent callers (resume lock and a reconnect) wait on one prompt, not two.
        if let pending { return try await pending.value }
        let evaluator = self.evaluator
        let reuse = self.reuse
        let task = Task<LAContext, any Error> {
            let context = LAContext()
            context.touchIDAuthenticationAllowableReuseDuration = reuse
            try await evaluator.evaluate(context, reason: reason)
            return context
        }
        pending = task
        let result = await task.result
        // invalidate() during the prompt discards this evaluation for later callers.
        if pending == task {
            pending = nil
            evaluated = try? result.get()
        }
        return try result.get()
    }

    private func takeExpiry() -> Bool {
        let date = now()
        return expiry.withLock { state in
            var expired = state.expired
            if let since = state.backgroundedAt, date.timeIntervalSince(since) >= Self.backgroundLimit {
                expired = true
                // The context made now is fresh, so the background limit counts from here.
                state.backgroundedAt = date
            }
            state.expired = false
            return expired
        }
    }

    public func invalidate() {
        evaluated?.invalidate()
        evaluated = nil
        pending?.cancel()
        pending = nil
    }
}
