import Foundation
import TogoKit
import TogoPush
import UserNotifications

/// A machine's open helper connection, used before the relay when the app has one.
@MainActor protocol LiveAnswering: AnyObject {
    /// Whether the app holds a live control channel to the machine this pairing belongs to.
    func hasLiveChannel(_ pairing: Pairing) -> Bool
    func answer(_ pairing: Pairing, requestId: String, decision: Decision) async throws
}

/// A delivered notification that belongs to a request.
struct DeliveredRequest: Hashable, Sendable {
    var requestId: String
    var hostId: String
}

/// The system notifications the action and clear handlers touch.
@MainActor protocol DeliveredNotifications: AnyObject {
    func delivered() async -> [DeliveredRequest]
    func removeDelivered(requestIds: Set<String>) async
    /// "Could not send. Open Togo to answer." in the request's thread.
    func postSendFailure(threadID: String) async
}

/// Answers a request from a notification action or a Live Activity button, in the app's
/// process and usually in the background.
///
/// The answer goes over the machine's live helper connection when the app has one, otherwise
/// signed and sealed through the relay mailbox. Each request is answered once: the shared
/// `PendingStore` claims it under its file lock before anything is sent, so a double tap, a
/// relaunch or another process answering the same request sends nothing more.
@MainActor final class NotificationActionHandler {
    enum Outcome: Equatable {
        case sent
        /// Already answered or being answered from this device; nothing was sent.
        case repeated
        /// Answered elsewhere, expired or refused by the helper; nothing was typed.
        case stale
        case failed
    }

    struct Dependencies {
        var pairings: PairingStore
        var pending: PendingStore
        /// A `RelayAnswerSender` given the same `pending` store, so option answers can be checked.
        var relay: any AnswerSending
        var live: (any LiveAnswering)?
        var notifications: any DeliveredNotifications
    }

    private let deps: Dependencies

    init(dependencies: Dependencies) {
        deps = dependencies
    }

    /// The user picked `actionIdentifier` on a notification. Returns the link to open for
    /// Open; answers Approve and Deny in place.
    @discardableResult
    func handle(actionIdentifier: String, userInfo: [AnyHashable: Any], target: String?) async -> DeepLink? {
        switch actionIdentifier {
        case NotificationActionID.approve, NotificationActionID.deny:
            guard let info = userInfo[NotificationServiceCore.infoKey] as? [String: Any],
                  let requestId = info["requestId"] as? String,
                  let hostId = info["hostId"] as? String
            else { return nil }
            let session = (info["sessionId"] as? String).flatMap { $0.isEmpty ? nil : $0 }
            let decision = actionIdentifier == NotificationActionID.approve ? "approve" : "deny"
            await answer(hostId: hostId, requestId: requestId, decision: decision, threadID: session ?? hostId)
            return nil
        default:
            return target.flatMap(URL.init(string:)).flatMap(DeepLink.init(url:))
        }
    }

    /// The single way to answer from outside the app's screens. `decision` is `approve`,
    /// `deny` or `option:<n>`; an option whose role is `approve_always` is refused, since that
    /// needs the app open.
    @discardableResult
    func answer(hostId: String, requestId: String, decision: String, threadID: String? = nil) async -> Outcome {
        let thread = threadID ?? hostId
        guard let wire = wireDecision(decision, requestId: requestId),
              let pairing = deps.pairings.pairing(hostId: hostId)
        else {
            await deps.notifications.postSendFailure(threadID: thread)
            return .failed
        }
        guard deps.pending.claim(requestId, hostId: hostId) else { return .repeated }

        do {
            if let live = deps.live, live.hasLiveChannel(pairing) {
                try await live.answer(pairing, requestId: requestId, decision: wire)
            } else {
                try await deps.relay.send(requestId: requestId, decision: decision, pairing: pairing)
            }
        } catch HelperError.conflict {
            deps.pending.mark(requestId, .elsewhere)
            await deps.notifications.removeDelivered(requestIds: [requestId])
            return .stale
        } catch {
            return await failed(requestId, thread: thread)
        }
        deps.pending.mark(requestId, wire == .deny ? .denied : .approved)
        await deps.notifications.removeDelivered(requestIds: [requestId])
        return .sent
    }

    private func failed(_ requestId: String, thread: String) async -> Outcome {
        // Nothing reached the machine, so the user may answer again from the app.
        deps.pending.release(requestId)
        await deps.notifications.postSendFailure(threadID: thread)
        return .failed
    }

    /// `approve`, `deny`, or an option the pending request lists with a role other than
    /// `approve_always`.
    private func wireDecision(_ decision: String, requestId: String) -> Decision? {
        switch decision {
        case "approve": return .approve
        case "deny": return .deny
        default:
            guard decision.hasPrefix("option:"), let n = Int(decision.dropFirst("option:".count)), n > 0,
                  "option:\(n)" == decision,
                  let option = deps.pending.request(requestId)?.options.first(where: { $0.n == n }),
                  option.role != "approve_always"
            else { return nil }
            return .option(n)
        }
    }
}

/// `DeliveredNotifications` over the app's notification center.
@MainActor final class SystemNotifications: DeliveredNotifications {
    private let center: UNUserNotificationCenter

    init(center: UNUserNotificationCenter = .current()) {
        self.center = center
    }

    func delivered() async -> [DeliveredRequest] {
        await center.deliveredNotifications().compactMap { notification in
            guard let info = notification.request.content.userInfo[NotificationServiceCore.infoKey] as? [String: Any],
                  let requestId = info["requestId"] as? String, let hostId = info["hostId"] as? String
            else { return nil }
            return DeliveredRequest(requestId: requestId, hostId: hostId)
        }
    }

    func removeDelivered(requestIds: Set<String>) async {
        guard !requestIds.isEmpty else { return }
        let ids = await center.deliveredNotifications().compactMap { notification -> String? in
            let info = notification.request.content.userInfo[NotificationServiceCore.infoKey] as? [String: Any]
            guard let requestId = info?["requestId"] as? String, requestIds.contains(requestId) else { return nil }
            return notification.request.identifier
        }
        center.removeDeliveredNotifications(withIdentifiers: ids)
    }

    func postSendFailure(threadID: String) async {
        let content = UNMutableNotificationContent()
        content.title = "Togo"
        content.body = String(localized: "notif.sendFailed", defaultValue: "Could not send. Open Togo to answer.")
        content.categoryIdentifier = NotificationCategoryID.info
        content.threadIdentifier = threadID
        let request = UNNotificationRequest(identifier: UUID().uuidString, content: content, trigger: nil)
        try? await center.add(request)
    }
}
