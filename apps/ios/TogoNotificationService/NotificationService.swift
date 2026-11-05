import Foundation
import TogoPush
import UserNotifications
import WidgetKit

/// Opens each sealed push on the device and replaces the relay's generic alert with the
/// request's structured text. Anything that fails leaves the generic alert.
final class NotificationService: UNNotificationServiceExtension {
    private var gate: DeliveryGate?

    override func didReceive(
        _ request: UNNotificationRequest,
        withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void
    ) {
        let deliver = UncheckedHandler(contentHandler)
        let gate = DeliveryGate { deliver.call($0) }
        self.gate = gate

        guard let directory = SharedGroup.storeDirectory(SharedGroup.ios) else {
            gate.expire()
            return
        }
        NotificationCategorySet.register()
        let core = NotificationServiceCore(
            // No explicit group: the only Keychain group in this extension's and the app's
            // entitlements is app.togo.shared, so both read and write the same keys.
            keys: KeychainDeviceKeys(accessGroup: nil),
            pairings: PairingStore(directory: directory),
            pending: PendingStore(directory: directory, clock: { Date() }),
            clock: { Date() }
        )
        let result = core.process(request.content)
        if result.pendingChanged {
            WidgetCenter.shared.reloadTimelines(ofKind: WidgetKinds.waiting)
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
