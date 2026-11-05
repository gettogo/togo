import Foundation
import TogoPush
import UserNotifications
import WatchKit

/// What only the Watch's application delegate hears: its push token, the silent pushes that
/// keep the request list and complication current, and Approve or Deny on a phone
/// notification shown on the Watch.
///
/// Request alerts come from the phone and are mirrored here with their actions; the Watch
/// gets no alerts of its own. Approve carries `authenticationRequired`, so watchOS asks for
/// the Watch to be unlocked first.
@MainActor final class WatchAppDelegate: NSObject, WKApplicationDelegate, UNUserNotificationCenterDelegate {
    let store: WatchStore
    private let core: NotificationServiceCore
    private let link: WatchConnectivityClient

    override init() {
        let directory = SharedGroup.storeDirectory(SharedGroup.watch)
            ?? URL.applicationSupportDirectory.appending(path: "Push", directoryHint: .isDirectory)
        // Without an access group the keychain uses the only one entitled, the Watch's own.
        let keys = KeychainDeviceKeys(accessGroup: nil)
        let pairings = PairingStore(directory: directory)
        let pending = PendingStore(directory: directory, clock: { Date() })
        store = WatchStore(dependencies: .init(
            pending: pending, pairings: pairings,
            sender: RelayAnswerSender(keys: keys, pending: pending, clock: { Date() })
        ))
        core = NotificationServiceCore(keys: keys, pairings: pairings, pending: pending, clock: { Date() })
        link = WatchConnectivityClient(store: store, pairings: pairings, keys: keys)
        super.init()
    }

    func applicationDidFinishLaunching() {
        let center = UNUserNotificationCenter.current()
        center.delegate = self
        // Approve always needs the Watch unlocked; the phone's lock-screen setting does not apply.
        center.setNotificationCategories(NotificationCategorySet.make(approveWithoutUnlock: false))
        link.start()
        WKApplication.shared().registerForRemoteNotifications()
    }

    func applicationDidBecomeActive() {
        store.reload()
    }

    func didRegisterForRemoteNotifications(withDeviceToken deviceToken: Data) {
        let token = deviceToken.map { String(format: "%02x", $0) }.joined()
        Task { await link.tokenChanged(token) }
    }

    func didFailToRegisterForRemoteNotificationsWithError(_ error: any Error) {}

    /// A silent push from a machine: a new request or the clear for one.
    func didReceiveRemoteNotification(_ userInfo: [AnyHashable: Any]) async -> WKBackgroundFetchResult {
        guard let opened = core.open(userInfo: userInfo) else { return .noData }
        store.ingest(opened.payload, hostId: opened.pairing.hostId)
        return .newData
    }

    // MARK: UNUserNotificationCenterDelegate

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse
    ) async {
        let action = response.actionIdentifier
        let info = response.notification.request.content.userInfo[NotificationServiceCore.infoKey] as? [String: Any]
        guard action == NotificationActionID.approve || action == NotificationActionID.deny,
              let requestId = info?["requestId"] as? String, let hostId = info?["hostId"] as? String
        else { return }
        let decision = action == NotificationActionID.approve ? "approve" : "deny"
        await answer(hostId: hostId, requestId: requestId, decision: decision)
    }

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter, willPresent notification: UNNotification
    ) async -> UNNotificationPresentationOptions {
        [.banner, .list, .sound]
    }

    private func answer(hostId: String, requestId: String, decision: String) async {
        await store.answerFromNotification(hostId: hostId, requestId: requestId, decision: decision)
        guard let notice = store.failureNotice(requestId) else { return }
        let content = UNMutableNotificationContent()
        content.title = "Togo"
        content.body = notice
        content.categoryIdentifier = NotificationCategoryID.info
        try? await UNUserNotificationCenter.current().add(
            UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil)
        )
    }
}
