import Foundation
import TogoPush
import UserNotifications

/// Registers the lock-screen actions. Call at launch and again whenever "approve without
/// unlocking" changes; the notification service extension registers the same set.
enum NotificationCategories {
    static func register(
        center: UNUserNotificationCenter = .current(), defaults: UserDefaults? = SharedSettings.defaults
    ) {
        NotificationCategorySet.register(center: center, defaults: defaults)
    }

    /// Stores the setting where the extension can read it and registers the matching actions.
    static func setApproveWithoutUnlock(_ on: Bool, defaults: UserDefaults? = SharedSettings.defaults) {
        defaults?.set(on, forKey: SharedSettings.approveWithoutUnlock)
        register(defaults: defaults)
    }
}
