import Foundation
@preconcurrency import LocalAuthentication
import Synchronization
import Testing
import TogoKit
@testable import Togo

struct PrivacyTests {
    @Test func biometryWording() {
        #expect(BiometryName.label(for: .touchID) == "Touch ID")
        #expect(BiometryName.label(for: .faceID) == "Face ID")
        #expect(BiometryName.label(for: .opticID) == "Optic ID")
        #expect(BiometryName.label(for: .none) == "passcode")
    }

    @Test func resumeLockRule() {
        let now = Date()
        #expect(!ResumeLock.needsUnlock(backgroundedAt: now.addingTimeInterval(-5), now: now, enabled: false))
        #expect(!ResumeLock.needsUnlock(backgroundedAt: now.addingTimeInterval(-0.5), now: now, enabled: true))
        #expect(ResumeLock.needsUnlock(backgroundedAt: now.addingTimeInterval(-2), now: now, enabled: true))
        #expect(!ResumeLock.needsUnlock(backgroundedAt: nil, now: now, enabled: true))
    }

    @Test func resumeLockDefaultsOff() throws {
        let defaults = try #require(UserDefaults(suiteName: "privacy-test-\(UUID().uuidString)"))
        #expect(!defaults.bool(forKey: ResumeLock.defaultsKey))
    }
}

private final class CountingEvaluator: BiometricEvaluator {
    let calls = Mutex(0)

    func evaluate(_ context: LAContext, reason: String) async throws {
        calls.withLock { $0 += 1 }
    }
}

@MainActor
struct ResumeLockControllerTests {
    @Test func engagingLockPromptsEvenWithCachedContext() async throws {
        let defaults = try #require(UserDefaults(suiteName: "privacy-test-\(UUID().uuidString)"))
        defaults.set(true, forKey: ResumeLock.defaultsKey)
        let evaluator = CountingEvaluator()
        let biometrics = BiometricSession(evaluator: evaluator)
        _ = try await biometrics.context(reason: "earlier signing")
        let controller = ResumeLockController(biometrics: biometrics, defaults: defaults)
        let now = Date()
        controller.scenePhaseChanged(to: .background, now: now)
        controller.scenePhaseChanged(to: .active, now: now.addingTimeInterval(5))
        #expect(controller.locked)
        await controller.pending?.value
        #expect(!controller.locked)
        #expect(evaluator.calls.withLock { $0 } == 2)
    }

    @Test func deviceLockClearsTheSigningContext() async throws {
        let evaluator = CountingEvaluator()
        let biometrics = BiometricSession(evaluator: evaluator)
        let controller = ResumeLockController(biometrics: biometrics)
        _ = try await biometrics.context(reason: "sign")
        controller.protectedDataBecameUnavailable()
        _ = try await biometrics.context(reason: "sign")
        #expect(evaluator.calls.withLock { $0 } == 2)
    }

    #if DEBUG
    @Test func signingUsesTheResumeLockSession() {
        #expect(DebugComposition.shared.env.biometrics === BiometricSession.app)
    }
    #endif

    @Test func shortBackgroundDoesNotLock() async throws {
        let defaults = try #require(UserDefaults(suiteName: "privacy-test-\(UUID().uuidString)"))
        defaults.set(true, forKey: ResumeLock.defaultsKey)
        let evaluator = CountingEvaluator()
        let controller = ResumeLockController(biometrics: BiometricSession(evaluator: evaluator), defaults: defaults)
        let now = Date()
        controller.scenePhaseChanged(to: .background, now: now)
        controller.scenePhaseChanged(to: .active, now: now.addingTimeInterval(0.5))
        await controller.pending?.value
        #expect(!controller.locked)
        #expect(evaluator.calls.withLock { $0 } == 0)
    }
}
