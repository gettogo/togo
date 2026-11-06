import Foundation
import TogoPush
import WidgetKit

/// Removes what a request left behind once it is answered: on the `clear` background push the
/// helper sends when the approval closes, and when the app comes to the foreground.
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
        guard opened.payload.kind == .clear, let requestId = opened.payload.requestId else { return false }
        await clear([requestId])
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
