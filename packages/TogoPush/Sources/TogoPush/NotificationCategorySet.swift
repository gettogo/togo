import Foundation
import UserNotifications

/// Settings the app writes to the shared app group for its extensions.
public enum SharedSettings {
    /// When true, Approve works from the lock screen without unlocking. Off by default.
    public static let approveWithoutUnlock = "approveWithoutUnlock"

    public static var defaults: UserDefaults? { UserDefaults(suiteName: SharedGroup.ios) }
}

/// The notification categories and their lock-screen actions, registered by the app at
/// launch and by the notification service extension.
public enum NotificationCategorySet {
    /// Approve sends input to the agent, so it needs the device unlocked unless the user
    /// turned that off. Deny only stops the agent and never does.
    public static func make(approveWithoutUnlock: Bool) -> Set<UNNotificationCategory> {
        let approve = UNNotificationAction(
            identifier: NotificationActionID.approve,
            title: String(localized: "notif.approve", defaultValue: "Approve"),
            options: approveWithoutUnlock ? [] : .authenticationRequired,
            icon: UNNotificationActionIcon(systemImageName: "checkmark")
        )
        let deny = UNNotificationAction(
            identifier: NotificationActionID.deny,
            title: String(localized: "notif.deny", defaultValue: "Deny"),
            options: .destructive,
            icon: UNNotificationActionIcon(systemImageName: "xmark")
        )
        let open = UNNotificationAction(
            identifier: NotificationActionID.open,
            title: String(localized: "notif.open", defaultValue: "Open"),
            options: .foreground
        )
        return [
            UNNotificationCategory(
                identifier: NotificationCategoryID.request, actions: [approve, deny, open], intentIdentifiers: []
            ),
            UNNotificationCategory(
                identifier: NotificationCategoryID.requestDenyOnly, actions: [deny, open], intentIdentifiers: []
            ),
            UNNotificationCategory(identifier: NotificationCategoryID.info, actions: [], intentIdentifiers: []),
        ]
    }

    /// The categories for the current shared setting. Unreadable defaults (before first
    /// unlock) mean Approve needs unlock.
    public static func current(defaults: UserDefaults? = SharedSettings.defaults) -> Set<UNNotificationCategory> {
        make(approveWithoutUnlock: defaults?.bool(forKey: SharedSettings.approveWithoutUnlock) ?? false)
    }

    /// Registers the categories for the current shared setting. The extension calls it for
    /// every push; the app calls it at launch and after the setting changes.
    public static func register(
        center: UNUserNotificationCenter = .current(), defaults: UserDefaults? = SharedSettings.defaults
    ) {
        center.setNotificationCategories(current(defaults: defaults))
    }
}
