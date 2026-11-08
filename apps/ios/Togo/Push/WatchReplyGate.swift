import Foundation
import TogoKit
import TogoPush

/// What the phone checks before it types anything the Watch sent. The message comes over
/// WatchConnectivity and is read as untrusted: `WatchReplyMessage(message:)` already refused
/// malformed ids and oversized text; here the target must be a machine in Togo and a session or
/// request on it that is waiting for words right now, as far as the phone knows.
enum WatchReplyGate {
    /// The handler the bridge calls for each reply the Watch sends.
    @MainActor static func handler(
        store: AppStore, pairings: PairingStore, pending: PendingStore, sender: ReplySender
    ) -> @MainActor (WatchReplyMessage) async -> ReplyOutcome {
        { message in
            guard let reply = Reply(message, pairings: pairings) else { return .refused(ReplyCopy.unknownMachine) }
            if let refusal = store.watchReplyRefusal(reply, pending: pending, pairings: pairings) { return refusal }
            return await sender.send(reply)
        }
    }

    /// The handler while no window has a store, such as when a reply from the Watch launched
    /// the app in the background. Only a typed answer to a request this device's push
    /// announced, not yet answered, from the machine it goes to, can be checked without the
    /// sessions; everything else needs the phone opened.
    @MainActor static func windowless(
        pairings: PairingStore, pending: PendingStore, sender: ReplySender
    ) -> @MainActor (WatchReplyMessage) async -> ReplyOutcome {
        { message in
            guard let reply = Reply(message, pairings: pairings) else { return .refused(ReplyCopy.unknownMachine) }
            guard windowlessAllows(reply, pending: pending, pairings: pairings) else { return .needsPhone }
            return await sender.send(reply)
        }
    }

    static func windowlessAllows(_ reply: Reply, pending: PendingStore, pairings: PairingStore) -> Bool {
        guard case .answer(let host, let requestID, _) = reply,
              let request = pending.request(requestID), request.allowText == true, request.answered == nil
        else { return false }
        return pairings.pairing(hostId: request.hostId)?.appHostID == host
    }
}

extension AppStore {
    /// Nil when the reply may go. A machine Togo knows whose sessions have not arrived yet (the
    /// phone woke for this message) needs the phone opened; anything else unknown is refused.
    func watchReplyRefusal(_ reply: Reply, pending: PendingStore, pairings: PairingStore) -> ReplyOutcome? {
        let known = services.hosts.contains { $0.id == reply.host }
        let section = controlCenter.machines.first { $0.id == reply.host }
        let name = replyLabel(reply)
        switch reply {
        case .prompt(_, let session, _):
            guard let section else { return known ? .needsPhone : .refused(ReplyCopy.unknownMachine) }
            guard let row = section.sessions.first(where: { $0.id == session }) else {
                return .refused(ReplyCopy.ended(name))
            }
            switch row.status {
            case .done, .idle: return nil
            case .failed where LifecycleCopy.agentWaiting(row.status, exit: row.exit): return nil
            case .needsYou: return .refused(ReplyCopy.asking(name))
            case .working: return .refused(ReplyCopy.working(name))
            default: return .refused(ReplyCopy.ended(name))
            }
        case .answer(let host, let requestID, _):
            if let card = controlCenter.requests.first(where: { $0.id == requestID && $0.hostID == host }) {
                return card.allowText && card.phase == .open ? nil : .refused(ReplyCopy.closed(name))
            }
            // The push said the request takes text, nothing has answered it here, and it came
            // from the machine the answer goes to.
            if let request = pending.request(requestID), request.allowText == true, request.answered == nil,
               pairings.pairing(hostId: request.hostId)?.appHostID == host {
                return nil
            }
            guard known else { return .refused(ReplyCopy.unknownMachine) }
            return section == nil ? .needsPhone : .refused(ReplyCopy.closed(name))
        }
    }
}
