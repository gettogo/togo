import Foundation
import TogoPush
import WidgetKit

/// Removes what a request left behind once it is answered: on the `clear` background push the
/// helper sends when the approval closes, and when the app comes to the foreground. Also records
/// a request whose notification a running Live Activity replaced, from the silent copy the
/// helper sends alongside, so the widget, Siri and Watch replies know it is waiting.
@MainActor final class ClearHandler {
    struct Dependencies {
        /// Opens background pushes; the same code the notification service extension uses.
        var core: NotificationServiceCore
        var pending: PendingStore
        var notifications: any DeliveredNotifications
        var reloadWidgets: () -> Void = { WidgetCenter.shared.reloadTimelines(ofKind: WidgetKinds.waiting) }
        /// Tells the Watch the request is gone, when it is reachable.
        var cleared: (String) -> Void = { _ in }
    }

    private let deps: Dependencies

    init(dependencies: Dependencies) {
        deps = dependencies
    }

    /// `application(_:didReceiveRemoteNotification:)`. Returns whether anything changed.
    @discardableResult
    func didReceiveRemoteNotification(_ userInfo: [AnyHashable: Any]) async -> Bool {
        guard let opened = deps.core.open(userInfo: userInfo) else { return false }
        // Anything the machine sends means it is answering again.
        if opened.payload.kind != .offline {
            deps.pending.clearOfflineMark(hostId: opened.pairing.hostId)
            await deps.notifications.removeOffline(hostId: opened.pairing.hostId)
        }
        if opened.payload.kind == .needsYou {
            return record(opened.payload, hostId: opened.pairing.hostId, machine: opened.pairing.machineName)
        }
        guard opened.payload.kind == .clear, let requestId = opened.payload.requestId else { return false }
        await clear([requestId])
        return true
    }

    /// The same record the notification service keeps for a request it shows. Its window runs
    /// from when the machine sent it, since a silent push can arrive late.
    private func record(_ p: PushPayload, hostId: String, machine: String) -> Bool {
        let now = Date()
        let sent = Date(timeIntervalSince1970: Double(p.ts) / 1000)
        let age = now.timeIntervalSince(sent)
        guard let requestId = p.requestId, age <= NotificationServiceCore.maxAge,
              age >= -NotificationServiceCore.maxSkew, deps.pending.request(requestId) == nil
        else { return false }
        deps.pending.upsert(PendingRequest(
            requestId: requestId, hostId: hostId, sessionId: p.sessionId, bib: p.bib, agent: p.agent,
            host: p.host ?? machine, title: PromptText.clean(p.title, maxLines: 1), subject: p.subject,
            options: p.options ?? [], receivedAt: min(now, sent), answered: nil,
            allowText: p.allowText == true ? true : nil
        ))
        deps.reloadWidgets()
        return true
    }

    /// The app came to the foreground. Old requests are pruned, and a delivered notification
    /// is removed when its machine reported and no longer lists its request.
    ///
    /// - Parameters:
    ///   - open: request ids the machines in `reported` still list as pending.
    ///   - reported: host ids (`h_…`) whose live snapshot arrived.
    func didEnterForeground(open: Set<String>, reported: Set<String>) async {
        deps.pending.prune()
        for hostId in reported {
            deps.pending.clearOfflineMark(hostId: hostId)
            await deps.notifications.removeOffline(hostId: hostId)
        }
        let gone = await deps.notifications.delivered()
            .filter { reported.contains($0.hostId) && !open.contains($0.requestId) }
            .map(\.requestId)
        let stale = deps.pending.all()
            .filter { reported.contains($0.hostId) && !open.contains($0.requestId) }
            .map(\.requestId)
        let all = Set(gone + stale)
        if !all.isEmpty { await clear(all) }
    }

    private func clear(_ requestIds: Set<String>) async {
        await deps.notifications.removeDelivered(requestIds: requestIds)
        for id in requestIds {
            deps.pending.remove(id)
            deps.cleared(id)
        }
        deps.reloadWidgets()
    }
}
