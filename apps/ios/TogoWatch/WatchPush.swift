import Foundation
import TogoPush
import UserNotifications
import WatchKit

/// What only the Watch's application delegate hears: its push token, the silent pushes that
/// keep the request list and complication current, and the actions on a phone notification
/// shown on the Watch: Approve or Deny, Reply, and Run again, which the phone carries out.
///
/// Request alerts usually come from the phone and are mirrored here with their actions. While
/// the phone's Live Activity stands in for its alert there is nothing to mirror, so the machine
/// sends the Watch the alert itself, opened by the Watch's notification service; its clear then
/// takes that alert away here. Approve carries `authenticationRequired`, so watchOS asks for
/// the Watch to be unlocked first.
@MainActor final class WatchAppDelegate: NSObject, WKApplicationDelegate, UNUserNotificationCenterDelegate {
    let store: WatchStore
    let router = WatchRouter()
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
        core = NotificationServiceCore(keys: keys, pairings: pairings, pending: pending, clock: { Date() },
            doneSilent: { SharedSettings.readDoneSilent(UserDefaults(suiteName: SharedGroup.watch)) })
        link = WatchConnectivityClient(store: store, pairings: pairings, keys: keys)
        super.init()
        store.sendReply = { [link] in await link.sendReply($0) }
        store.sendRunAgain = { [link] in await link.sendRunAgain($0) }
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
        if opened.payload.kind == .clear, let requestId = opened.payload.requestId {
            await Self.removeDelivered(requestId: requestId)
        }
        return .newData
    }

    /// Takes away the Watch's own alert for a request answered elsewhere.
    private static func removeDelivered(requestId: String) async {
        let center = UNUserNotificationCenter.current()
        let ids = await center.deliveredNotifications().compactMap { notification -> String? in
            let info = notification.request.content.userInfo[NotificationServiceCore.infoKey] as? [String: Any]
            return info?["requestId"] as? String == requestId ? notification.request.identifier : nil
        }
        if !ids.isEmpty { center.removeDeliveredNotifications(withIdentifiers: ids) }
    }

    // MARK: UNUserNotificationCenterDelegate

    nonisolated func userNotificationCenter(
        _ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse
    ) async {
        let action = response.actionIdentifier
        let content = response.notification.request.content
        let info = content.userInfo[NotificationServiceCore.infoKey] as? [String: Any]
        if action == UNNotificationDefaultActionIdentifier,
           let route = ReplyRoute(userInfo: content.userInfo[ReplyRoute.key] as? [String: String]) {
            // The notice for a reply that did not go: open its reply screen, words and all.
            await open(route)
            return
        }
        if action == UNNotificationDefaultActionIdentifier || action == NotificationActionID.open {
            // The body or Open: land on what the notification was about, not the oldest request.
            if let requestId = info?["requestId"] as? String, WatchLink.isRequestID(requestId) {
                await land(.request(requestId))
            } else if (info?["sessionId"] as? String).map({ !$0.isEmpty }) == true {
                await land(.sessions)
            }
            return
        }
        if action == NotificationActionID.reply {
            let text = (response as? UNTextInputNotificationResponse)?.userText ?? ""
            let hostId = info?["hostId"] as? String, requestId = info?["requestId"] as? String
            let sessionId = (info?["sessionId"] as? String).flatMap { $0.isEmpty ? nil : $0 }
            guard let hostId else { return }
            // The phone resolves the helper's host id to its machine.
            let target: WatchReplyMessage.Target? = requestId.map { .answer(hostId: hostId, requestId: $0) }
                ?? sessionId.map { .prompt(machineId: hostId, sessionId: $0) }
            guard let target else { return }
            let route = ReplyRoute(target: target, title: content.title)
            await reply(route, text: text, notification: response.notification.request.identifier)
            return
        }
        if action == NotificationActionID.runAgain {
            // The phone starts it over its own connection, as it does for its own notification.
            guard let hostId = info?["hostId"] as? String,
                  let sessionId = (info?["sessionId"] as? String).flatMap({ $0.isEmpty ? nil : $0 })
            else { return }
            await runAgain(hostId: hostId, sessionId: sessionId, notification: response.notification.request.identifier)
            return
        }
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

    /// A reply typed on a notification goes through the phone. When it does not go, the words
    /// wait in its reply screen and a notice says why; tapping the notice opens that screen.
    private func reply(_ route: ReplyRoute, text: String, notification: String) async {
        guard let notice = await store.replyFromNotification(target: route.target, text: text, notification: notification)
        else { return }
        let content = UNMutableNotificationContent()
        content.title = WatchCopy.noticeTitle
        content.body = notice
        content.categoryIdentifier = NotificationCategoryID.info
        content.userInfo = [ReplyRoute.key: route.userInfo]
        try? await UNUserNotificationCenter.current().add(
            UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil)
        )
    }

    private func runAgain(hostId: String, sessionId: String, notification: String) async {
        guard let notice = await store.runAgainFromNotification(
            hostId: hostId, sessionId: sessionId, notification: notification
        ) else { return }
        await post(notice: notice)
    }

    private func post(notice: String) async {
        let content = UNMutableNotificationContent()
        content.title = WatchCopy.noticeTitle
        content.body = notice
        content.categoryIdentifier = NotificationCategoryID.info
        try? await UNUserNotificationCenter.current().add(
            UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil)
        )
    }

    private func open(_ route: ReplyRoute) {
        store.openReply = route
    }

    private func land(_ link: WatchLink) {
        store.openReply = nil
        router.open(link)
    }

    private func answer(hostId: String, requestId: String, decision: String) async {
        await store.answerFromNotification(hostId: hostId, requestId: requestId, decision: decision)
        if let notice = store.failureNotice(requestId) {
            await post(notice: notice)
            return
        }
        // Nothing went: the desk or the phone answered first, or the window closed. Say so
        // rather than leave the tap looking like it worked.
        switch store.state(of: requestId) {
        case .answeredElsewhere: await post(notice: WatchCopy.answeredElsewhere)
        case .expired: await post(notice: WatchCopy.expired)
        default: break
        }
    }
}
