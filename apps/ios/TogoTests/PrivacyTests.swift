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

    @Test func resumeLockIsOnByDefault() {
        #expect(AppSettings().resumeLock)
    }
}

private final class CountingEvaluator: BiometricEvaluator {
    let calls = Mutex(0)

    func evaluate(_ context: LAContext, reason: String) async throws {
        calls.withLock { $0 += 1 }
    }
}

@MainActor private final class Setting {
    var on = false
}

@MainActor
struct ResumeLockControllerTests {
    @Test func engagingLockPromptsEvenWithCachedContext() async throws {
        let evaluator = CountingEvaluator()
        let biometrics = BiometricSession(evaluator: evaluator)
        _ = try await biometrics.context(reason: "earlier signing")
        let controller = ResumeLockController(biometrics: biometrics, isEnabled: { true })
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
        let controller = ResumeLockController(biometrics: biometrics, isEnabled: { true })
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

    @Test func lockFollowsTheSettingAtEachReturn() async throws {
        let setting = Setting()
        let controller = ResumeLockController(
            biometrics: BiometricSession(evaluator: CountingEvaluator()), isEnabled: { setting.on }
        )
        let now = Date()
        controller.scenePhaseChanged(to: .background, now: now)
        controller.scenePhaseChanged(to: .active, now: now.addingTimeInterval(5))
        #expect(!controller.locked)
        setting.on = true
        controller.scenePhaseChanged(to: .background, now: now.addingTimeInterval(10))
        controller.scenePhaseChanged(to: .active, now: now.addingTimeInterval(15))
        #expect(controller.locked)
        await controller.pending?.value
    }

    @Test func shortBackgroundDoesNotLock() async throws {
        let evaluator = CountingEvaluator()
        let controller = ResumeLockController(biometrics: BiometricSession(evaluator: evaluator), isEnabled: { true })
        let now = Date()
        controller.scenePhaseChanged(to: .background, now: now)
        controller.scenePhaseChanged(to: .active, now: now.addingTimeInterval(0.5))
        await controller.pending?.value
        #expect(!controller.locked)
        #expect(evaluator.calls.withLock { $0 } == 0)
    }
}
