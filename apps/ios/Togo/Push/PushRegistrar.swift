import Foundation
import Observation
import UIKit
import UserNotifications

/// The parts of `UNUserNotificationCenter` the registrar uses, so tests can answer for iOS.
protocol NotificationPermissions: Sendable {
    func authorization() async -> PushRegistrar.Authorization
    /// Shows the system question; true when the user allowed notifications.
    func request() async throws -> Bool
}

struct SystemNotificationPermissions: NotificationPermissions {
    func authorization() async -> PushRegistrar.Authorization {
        switch await UNUserNotificationCenter.current().notificationSettings().authorizationStatus {
        case .notDetermined: .notDetermined
        case .denied: .denied
        case .provisional: .provisional
        case .authorized, .ephemeral: .authorized
        @unknown default: .denied
        }
    }

    func request() async throws -> Bool {
        try await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])
    }
}

/// Notification permission and the APNs token. The token is never logged or printed.
@MainActor @Observable final class PushRegistrar {
    enum Authorization: Equatable, Sendable {
        case notDetermined, denied, provisional, authorized

        /// Pushes reach this iPhone.
        var allowsPush: Bool { self == .authorized || self == .provisional }
    }

    private(set) var authorization: Authorization = .notDetermined
    /// Lowercase hex, as APNs and the helper expect it.
    private(set) var apnsToken: String?
    /// Called when a token arrives that differs from the last one this process saw.
    @ObservationIgnored var onTokenChange: @MainActor () async -> Void = {}

    @ObservationIgnored private let permissions: any NotificationPermissions
    @ObservationIgnored private let registerForRemote: @MainActor () -> Void

    init(
        permissions: any NotificationPermissions = SystemNotificationPermissions(),
        registerForRemote: @escaping @MainActor () -> Void = { UIApplication.shared.registerForRemoteNotifications() }
    ) {
        self.permissions = permissions
        self.registerForRemote = registerForRemote
    }

    /// Asks iOS once. After the first answer this only reads the state again; the user changes
    /// it in iOS Settings from then on.
    func requestAuthorization() async {
        authorization = await permissions.authorization()
        if authorization == .notDetermined {
            _ = try? await permissions.request()
        }
        await refresh()
    }

    /// Reads the current permission and, when pushes are allowed, registers with APNs. iOS
    /// hands back the same token or a new one each launch.
    func refresh() async {
        authorization = await permissions.authorization()
        DiagnosticsRecorder.shared.push(authorization.diagnosticWord)
        if authorization.allowsPush { registerForRemote() }
    }

    func didRegister(token: Data) {
        let hex = token.map { String(format: "%02x", $0) }.joined()
        guard hex != apnsToken else { return }
        apnsToken = hex
        DiagnosticsRecorder.shared.push("token_registered")
        let onTokenChange = self.onTokenChange
        Task { await onTokenChange() }
    }

    /// Registration fails on simulators without APNs and while offline. The last token stays;
    /// the next launch asks again.
    func didFail(_ error: Error) {
        DiagnosticsRecorder.shared.push("registration_failed", error: error)
    }
}
