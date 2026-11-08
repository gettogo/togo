import Foundation
@preconcurrency import LocalAuthentication
import Observation
import SwiftUI
import TogoKit

struct ResumeLock {
    /// A glance at Control Centre or a notification banner should not lock the app.
    static let grace: TimeInterval = 1

    static func needsUnlock(backgroundedAt: Date?, now: Date, enabled: Bool) -> Bool {
        guard enabled, let backgroundedAt else { return false }
        return now.timeIntervalSince(backgroundedAt) >= grace
    }
}

/// Tracks background time and drives the lock screen. Unlocking evaluates the shared
/// `BiometricSession`, so key signing right after an unlock does not prompt again.
@MainActor @Observable
final class ResumeLockController {
    private(set) var locked = false
    private var backgroundedAt: Date?
    private let biometrics: BiometricSession
    /// Read at each return to the foreground, so a change in settings applies to the next one.
    private let isEnabled: @MainActor () -> Bool

    init(biometrics: BiometricSession, isEnabled: @escaping @MainActor () -> Bool) {
        self.biometrics = biometrics
        self.isEnabled = isEnabled
    }

    func scenePhaseChanged(to phase: ScenePhase, now: Date = Date()) {
        switch phase {
        case .background:
            if backgroundedAt == nil { backgroundedAt = now }
            biometrics.enteredBackground()
        case .active:
            let since = backgroundedAt
            backgroundedAt = nil
            // The session drops its context after a long background on its own; this only
            // tells it the background is over.
            biometrics.enteredForeground()
            let lock = ResumeLock.needsUnlock(backgroundedAt: since, now: now, enabled: isEnabled())
            if lock {
                locked = true
                // A cached context would unlock without asking; the fresh one is then shared with signing.
                pending = Task {
                    await biometrics.invalidate()
                    await authenticate()
                }
            }
        default:
            break
        }
    }

    /// Coming back now would lock: the cover holds over the return instead of showing the
    /// screen for a frame before the lock goes up.
    var locksOnReturn: Bool {
        ResumeLock.needsUnlock(backgroundedAt: backgroundedAt, now: Date(), enabled: isEnabled())
    }

    /// The device locked: the next signature must ask again, even one already on its way.
    func protectedDataBecameUnavailable() {
        biometrics.expire()
    }

    func unlock() {
        pending = Task { await authenticate() }
    }

    /// The work started by the last phase change or unlock tap, for callers that must wait for it.
    private(set) var pending: Task<Void, Never>?

    private func authenticate() async {
        do {
            _ = try await biometrics.context(reason: "Unlock Togo")
            locked = false
        } catch {
            // Stays locked; the lock screen offers another try.
        }
    }
}
