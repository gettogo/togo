import Foundation
import TogoKit
import TogoPush

/// What the phone checks before it starts a session again for the Watch. The message comes
/// over WatchConnectivity and is read as untrusted: `WatchRunAgainMessage(message:)` already
/// refused malformed ids. Here the machine must be in Togo and the session one the phone's own
/// Run again would start right now. The Watch's action asks for the Watch to be unlocked, as
/// the phone's asks for the phone; the phone then uses a channel that opens without asking.
enum WatchRunAgainGate {
    /// The handler the bridge calls for each Run again the Watch sends.
    @MainActor static func handler(
        store: AppStore, pairings: PairingStore, tools: any MachineTools
    ) -> @MainActor (WatchRunAgainMessage) async -> ReplyOutcome {
        { message in
            guard let host = UUID(uuidString: message.machineId) ?? pairings.pairing(hostId: message.machineId)?.appHostID
            else { return .refused(LifecycleCopy.runAgainUnknownMachine) }
            switch store.watchRunAgainCheck(host: host, session: message.sessionId) {
            case .refused(let outcome): return outcome
            case .allowed(let label):
                return await store.runAgainFromWatch(hostID: host, sessionID: message.sessionId, label: label, tools: tools)
            }
        }
    }
}

extension AppStore {
    enum WatchRunAgainCheck: Equatable {
        case allowed(label: String)
        case refused(ReplyOutcome)
    }

    /// The same rules as the row's Run again. A machine Togo knows whose sessions have not
    /// arrived yet (the phone woke for this message) needs the phone opened.
    func watchRunAgainCheck(host: HostID, session: String) -> WatchRunAgainCheck {
        guard let section = controlCenter.machines.first(where: { $0.id == host }) else {
            let known = services.hosts.contains { $0.id == host }
            return .refused(known ? .needsPhone : .refused(LifecycleCopy.runAgainUnknownMachine))
        }
        let row = section.sessions.first { $0.id == session }
            ?? controlCenter.archived.first { $0.hostID == host && $0.id == session }
        guard let row else { return .refused(.refused(LifecycleCopy.runAgainGone)) }
        let label = SessionActions.label(row)
        let actions = SessionActions.actions(for: row, on: section)
        if actions.allows(.runAgain) { return .allowed(label: label) }
        let stopped = LifecycleCopy.canRunAgain(row.status, exit: row.exit)
        let reason = stopped
            ? actions.note ?? ServiceErrors.message(HelperError.unsupported(capability: Capability.launch))
            : ServiceErrors.message(HelperError.conflict("running"))
        return .refused(.refused(LifecycleCopy.runAgainFailed(label, reason: reason)))
    }
}
