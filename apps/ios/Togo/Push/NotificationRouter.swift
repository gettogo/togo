import Foundation
import TogoPush
import UserNotifications

/// A notification the user acted on, with only the values the app needs from it.
struct NotificationResponse: Sendable, Equatable {
    var actionIdentifier: String
    /// `togo://session/<bib>?host=<machine>`, set by the notification service.
    var link: String?
    /// `userInfo["togo"]`: request, host and session ids. Never command text.
    var requestId: String?
    var hostId: String?
    var sessionId: String?
}

/// Decides how notifications show while Togo is open and where a tap goes.
@MainActor final class NotificationRouter: NSObject, UNUserNotificationCenterDelegate {
    /// The session on screen, as its bib and machine name, or nil when no session is in front.
    var frontSession: @MainActor () -> (bib: String, machine: String)? = { nil }
    /// Opens a `togo://` link.
    var open: @MainActor (DeepLink) -> Void = { _ in }
    /// Answers from a notification action. Tapping the notification itself is handled here.
    var onAction: @MainActor (NotificationResponse) async -> Void = { _ in }

    /// A banner for the session the user is looking at would only repeat the screen.
    func presentation(for link: String?) -> UNNotificationPresentationOptions {
        if let link, let url = URL(string: link), case .session(let bib, let host)? = DeepLink(url: url),
           let front = frontSession(), front.bib == bib,
           host.map({ $0.localizedCaseInsensitiveCompare(front.machine) == .orderedSame }) ?? true {
            return [.list]
        }
        return [.banner, .list, .sound]
    }

    func handle(_ response: NotificationResponse) async {
        if response.actionIdentifier == UNNotificationDefaultActionIdentifier || response.actionIdentifier == NotificationActionID.open {
            if let link = response.link, let url = URL(string: link), let deepLink = DeepLink(url: url) {
                open(deepLink)
            }
            return
        }
        guard response.actionIdentifier != UNNotificationDismissActionIdentifier else { return }
        await onAction(response)
    }

    // MARK: UNUserNotificationCenterDelegate

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter, willPresent notification: UNNotification
    ) async -> UNNotificationPresentationOptions {
        let link = notification.request.content.targetContentIdentifier
        return await presentation(for: link)
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let content = response.notification.request.content
        let togo = content.userInfo["togo"] as? [String: Any]
        let values = NotificationResponse(
            actionIdentifier: response.actionIdentifier, link: content.targetContentIdentifier,
            requestId: togo?["requestId"] as? String, hostId: togo?["hostId"] as? String,
            sessionId: togo?["sessionId"] as? String
        )
        await handle(values)
    }
}
