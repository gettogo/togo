import Foundation
import os
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
        let core = NotificationServiceCore(
            // No explicit group: the only Keychain group in this extension's and the app's
            // entitlements is app.togo.shared, so both read and write the same keys.
            keys: KeychainDeviceKeys(accessGroup: nil),
            pairings: PairingStore(directory: directory),
            pending: PendingStore(directory: directory, clock: { Date() }),
            clock: { Date() }
        )
        let result = core.process(request.content)
        gate.prepare(result.content)
        // After the request is stored, so a question's own option buttons are registered
        // before the system shows it.
        NotificationCategorySet.register()
        if result.pendingChanged {
            WidgetCenter.shared.reloadTimelines(ofKind: WidgetKinds.waiting)
        }
        let info = result.pendingChanged ? result.content.userInfo[NotificationServiceCore.infoKey] as? [String: Any] : nil
        let receiptHost = info?["hostId"] as? String
        let receiptRequest = info?["requestId"] as? String
        let offlineHost = result.clearsOfflineOf
        let content = UncheckedContent(result.content)
        let receipts = LiveContextReceiptSender(
            keys: KeychainDeviceKeys(accessGroup: nil), pairings: PairingStore(directory: directory),
            pending: PendingStore(directory: directory, clock: { Date() })
        )
        Task {
            if let host = offlineHost { await OfflineNotifications.removeDelivered(hostId: host) }
            if let host = receiptHost, let id = receiptRequest {
                do { try await receipts.send(hostID: host, requestID: id) }
                catch { Logger(subsystem: "app.togo", category: "push").error("Request-data receipt failed; notification still delivered") }
            }
            gate.finish(content.value)
        }
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

private struct UncheckedContent: @unchecked Sendable {
    let value: UNNotificationContent

    init(_ value: UNNotificationContent) {
        self.value = value
    }
}
