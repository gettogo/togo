import Foundation
import UserNotifications

/// Settings the app writes to the shared app group for its extensions.
public enum SharedSettings {
    /// When true, Approve works from the lock screen without unlocking. Off by default.
    public static let approveWithoutUnlock = "approveWithoutUnlock"
    /// When true, a "done" arrives without a sound and without interrupting. Off by default.
    public static let doneSilent = "doneSilent"

    /// Whether "done" notifications are silent on this device.
    public static func readDoneSilent(_ defaults: UserDefaults? = SharedSettings.defaults) -> Bool {
        defaults?.bool(forKey: doneSilent) ?? false
    }

    /// Stores the choice where this process's notification extension reads it.
    public static func writeDoneSilent(_ on: Bool, to defaults: UserDefaults?) {
        defaults?.set(on, forKey: doneSilent)
    }

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
        // Running again starts the agent on the machine, so it needs the device unlocked and
        // the app open to show what happens.
        let runAgain = UNNotificationAction(
            identifier: NotificationActionID.runAgain,
            title: String(localized: "notif.runAgain", defaultValue: "Run again"),
            options: [.foreground, .authenticationRequired],
            icon: UNNotificationActionIcon(systemImageName: "arrow.clockwise")
        )
        // Reply types into the agent, so it needs the device unlocked, and opens the app to say
        // whether the words went.
        let reply = UNTextInputNotificationAction(
            identifier: NotificationActionID.reply,
            title: String(localized: "notif.reply", defaultValue: "Reply"),
            options: [.foreground, .authenticationRequired],
            icon: UNNotificationActionIcon(systemImageName: "arrowshape.turn.up.left"),
            textInputButtonTitle: String(localized: "notif.reply.send", defaultValue: "Send"),
            textInputPlaceholder: String(localized: "notif.reply.placeholder", defaultValue: "Message")
        )
        return [
            UNNotificationCategory(
                identifier: NotificationCategoryID.request, actions: [approve, deny, open], intentIdentifiers: []
            ),
            UNNotificationCategory(
                identifier: NotificationCategoryID.requestDenyOnly, actions: [deny, open], intentIdentifiers: []
            ),
            UNNotificationCategory(identifier: NotificationCategoryID.info, actions: [], intentIdentifiers: []),
            UNNotificationCategory(identifier: NotificationCategoryID.ended, actions: [runAgain], intentIdentifiers: []),
            UNNotificationCategory(identifier: NotificationCategoryID.prompt, actions: [reply], intentIdentifiers: []),
            // Open gives way to Reply beside the buttons; Reply opens the app anyway.
            UNNotificationCategory(
                identifier: NotificationCategoryID.requestReply, actions: [approve, deny, reply], intentIdentifiers: []
            ),
            UNNotificationCategory(
                identifier: NotificationCategoryID.requestDenyOnlyReply, actions: [deny, reply], intentIdentifiers: []
            ),
            UNNotificationCategory(identifier: NotificationCategoryID.requestText, actions: [reply, open], intentIdentifiers: []),
        ]
    }

    /// The categories for the current shared setting, with one choice category for each
    /// waiting question in `pending`. Unreadable defaults (before first unlock) mean Approve
    /// needs unlock.
    public static func current(
        defaults: UserDefaults? = SharedSettings.defaults, pending: [PendingRequest] = []
    ) -> Set<UNNotificationCategory> {
        let without = defaults?.bool(forKey: SharedSettings.approveWithoutUnlock) ?? false
        return make(approveWithoutUnlock: without).union(choices(pending, approveWithoutUnlock: without))
    }

    /// Registers the categories for the current shared setting and the waiting questions the
    /// shared pending store holds. The extension calls it for every push; the app calls it at
    /// launch and after the setting changes.
    public static func register(
        center: UNUserNotificationCenter = .current(), defaults: UserDefaults? = SharedSettings.defaults,
        pending: [PendingRequest]? = nil
    ) {
        let waiting = pending ?? SharedGroup.storeDirectory(SharedGroup.ios).map {
            PendingStore(directory: $0, clock: { Date() }).all()
        } ?? []
        center.setNotificationCategories(current(defaults: defaults, pending: waiting))
    }

    /// The most options a question gets buttons for; the lock screen shows four actions.
    public static let maxChoices = 4

    /// Whether a request's options are a question's own answers that buttons can pick: one to
    /// four, none of them approve, deny or approve always (those use the request categories,
    /// and approve always needs the app).
    public static func offersChoices(_ options: [PushOption]) -> Bool {
        (1...maxChoices).contains(options.count)
            && !options.contains { ["approve", "deny", "approve_always"].contains($0.role) }
    }

    /// One category per unanswered question in `pending`, a button per option. Picking one
    /// sends input to the agent, so it needs the device unlocked unless the user turned that
    /// off, like Approve. Reply joins them when the request takes a typed answer.
    public static func choices(_ pending: [PendingRequest], approveWithoutUnlock: Bool) -> Set<UNNotificationCategory> {
        var out = Set<UNNotificationCategory>()
        for request in pending where request.answered == nil && offersChoices(request.options) {
            var actions: [UNNotificationAction] = request.options.map { option in
                UNNotificationAction(
                    identifier: NotificationActionID.option(option.n),
                    title: PromptText.clean(option.label, maxLines: 1),
                    options: approveWithoutUnlock ? [] : .authenticationRequired
                )
            }
            if request.allowText == true { actions.append(replyAction()) }
            out.insert(UNNotificationCategory(
                identifier: NotificationCategoryID.choice(request.requestId), actions: actions, intentIdentifiers: []
            ))
        }
        return out
    }

    private static func replyAction() -> UNTextInputNotificationAction {
        UNTextInputNotificationAction(
            identifier: NotificationActionID.reply,
            title: String(localized: "notif.reply", defaultValue: "Reply"),
            options: [.foreground, .authenticationRequired],
            icon: UNNotificationActionIcon(systemImageName: "arrowshape.turn.up.left"),
            textInputButtonTitle: String(localized: "notif.reply.send", defaultValue: "Send"),
            textInputPlaceholder: String(localized: "notif.reply.placeholder", defaultValue: "Message")
        )
    }
}
