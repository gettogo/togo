import Foundation
import TogoPush
import UserNotifications
import WidgetKit

/// Opens a sealed push on the Watch with the Watch's own keys and pairings, the same way the
/// phone's service does. Machines send the Watch silent pushes, so this runs only for an
/// alert that reaches the Watch directly; anything that fails leaves the generic alert.
final class NotificationService: UNNotificationServiceExtension {
    private var gate: DeliveryGate?

    override func didReceive(
        _ request: UNNotificationRequest,
        withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void
    ) {
        let deliver = UncheckedHandler(contentHandler)
        let gate = DeliveryGate { deliver.call($0) }
        self.gate = gate

        guard let directory = SharedGroup.storeDirectory(SharedGroup.watch) else {
            gate.expire()
            return
        }
        // Approve always needs the Watch unlocked.
        UNUserNotificationCenter.current().setNotificationCategories(
            NotificationCategorySet.make(approveWithoutUnlock: false)
        )
        let core = NotificationServiceCore(
            // No explicit group: the Watch's own keychain group is the only one entitled.
            keys: KeychainDeviceKeys(accessGroup: nil),
            pairings: PairingStore(directory: directory),
            pending: PendingStore(directory: directory, clock: { Date() }),
            clock: { Date() }
        )
        let result = core.process(request.content)
        if result.pendingChanged {
            WidgetCenter.shared.reloadTimelines(ofKind: "togo.watch.waiting")
        }
        gate.finish(result.content)
    }

    override func serviceExtensionTimeWillExpire() {
        gate?.expire()
    }
}

/// The system's content handler is called once, from whichever thread finishes first.
private struct UncheckedHandler: @unchecked Sendable {
    let handler: (UNNotificationContent) -> Void

    init(_ handler: @escaping (UNNotificationContent) -> Void) {
        self.handler = handler
    }

    func call(_ content: UNNotificationContent) {
        handler(content)
    }
}
