import Foundation
import TogoKit

/// What can be done with a session a restart ended.
enum RestoreAction: Hashable, Sendable {
    case restore, restoreAndContinue
}

enum RestoreActions {
    /// "Restore and continue" is offered only when the helper judged the agent's command safe
    /// to resume.
    static func offered(for session: RestorableSession) -> [RestoreAction] {
        session.resumable ? [.restore, .restoreAndContinue] : [.restore]
    }

    static func title(_ action: RestoreAction) -> String {
        switch action {
        case .restore: String(localized: "restore.restore", defaultValue: "Restore")
        case .restoreAndContinue: String(localized: "restore.restoreContinue", defaultValue: "Restore and continue")
        }
    }
}

enum RestoreCopy {
    static let endedByRestart = String(localized: "restore.endedByRestart", defaultValue: "Ended by restart")
    static let dismiss = String(localized: "restore.dismiss", defaultValue: "Dismiss")

    static func label(_ session: RestorableSession) -> String {
        [session.bib, session.name].filter { !$0.isEmpty }.joined(separator: " ")
    }

    static func folderGone(_ path: String, machine: String) -> String {
        String(localized: "restore.folderGone", defaultValue: "The folder \(path) is gone on \(machine).")
    }

    static func notInstalled(_ agent: String, machine: String) -> String {
        String(localized: "restore.notInstalled", defaultValue: "\(agent) is not installed on \(machine).")
    }

    static func failed(_ label: String, reason: String) -> String {
        String(localized: "restore.failed", defaultValue: "Couldn't restore \(label): \(reason)")
    }

    static func dismissFailed(_ label: String, reason: String) -> String {
        String(localized: "restore.dismissFailed", defaultValue: "Couldn't dismiss \(label): \(reason)")
    }

    static func axLabel(_ action: String, _ session: String) -> String {
        String(localized: "restore.axLabel", defaultValue: "\(action), \(session)")
    }

    static func gone(_ label: String) -> String {
        String(localized: "restore.gone", defaultValue: "\(label) can no longer be restored.")
    }
}

/// Sessions a machine restart ended: fetched once per connection, started again in place or
/// dismissed.
extension AppStore {
    /// Machines that are online with a helper that offers `restore.v1`. The control center
    /// loads again when this changes.
    var restoreCandidates: Set<HostID> {
        Set(controlCenter.machines.filter { machine in
            guard case .online = machine.reachability, case .present = machine.helper else { return false }
            return machine.capabilities.contains(Capability.restore)
        }.map(\.id))
    }

    /// Fetches the list for every candidate machine that has not been asked on its current
    /// connection. A failed fetch is quiet and leaves the machine to be asked again on its next
    /// connection.
    func loadRestorable(tools: any MachineTools) async {
        let pending = restoreCandidates.subtracting(restoreLoaded)
        restoreLoaded.formUnion(pending)
        let loads = pending.map { host in Task { await loadRestorable(host, tools: tools) } }
        for load in loads { await load.value }
    }

    private func loadRestorable(_ host: HostID, tools: any MachineTools) async {
        do {
            let list = try await tools.call(host) { try await $0.restorable() }
            // The machine may have gone away and been asked about again while this was in flight.
            guard restoreLoaded.contains(host), let m = controlCenter.machines.firstIndex(where: { $0.id == host }) else { return }
            controlCenter.machines[m].restorable = list
        } catch {
            restoreLoaded.remove(host)
        }
    }

    func restore(_ session: RestorableSession, on host: HostID, resume: Bool, tools: any MachineTools) async {
        let id = TabID(hostID: host, sessionID: session.id)
        guard !restoring.contains(id) else { return }
        restoring.insert(id)
        defer { restoring.remove(id) }
        // The flag is only ever sent for a command the helper judged safe to resume.
        let resume = resume && session.resumable
        do {
            let started = try await tools.call(host) { try await $0.restore(id: session.id, resume: resume) }
            guard let m = controlCenter.machines.firstIndex(where: { $0.id == host }) else { return }
            controlCenter.machines[m].restorable.removeAll { $0.id == session.id }
            controlCenter.machines[m].sessions.removeAll { $0.id == started.id }
            controlCenter.machines[m].sessions.append(restoredRow(started, in: controlCenter.machines[m]))
            controlCenter.machines[m].sessions.sort(by: Self.bibOrder)
            if notice == restoreNotice { notice = nil }
            restoreNotice = nil
        } catch {
            fail(error, restoring: session, on: host)
        }
    }

    func dismissRestorable(_ session: RestorableSession, on host: HostID, tools: any MachineTools) async {
        let id = TabID(hostID: host, sessionID: session.id)
        guard !restoring.contains(id) else { return }
        restoring.insert(id)
        defer { restoring.remove(id) }
        do {
            try await tools.call(host) { try await $0.dismissRestorable(id: session.id) }
            remove(session, from: host)
        } catch HelperError.rpc(let error) where error.code == RPCErrorCode.notFound {
            remove(session, from: host)
        } catch {
            notice = Notice(
                text: RestoreCopy.dismissFailed(RestoreCopy.label(session), reason: ServiceErrors.message(error)),
                isError: true
            )
        }
    }

    private func remove(_ session: RestorableSession, from host: HostID) {
        guard let m = controlCenter.machines.firstIndex(where: { $0.id == host }) else { return }
        controlCenter.machines[m].restorable.removeAll { $0.id == session.id }
    }

    private func fail(_ error: any Error, restoring session: RestorableSession, on host: HostID) {
        let machine = controlCenter.machines.first { $0.id == host }?.name ?? services.hosts.first { $0.id == host }?.name ?? ""
        let label = RestoreCopy.label(session)
        let text: String
        switch error as? HelperError {
        case .rpc(let rpc)? where rpc.code == RPCErrorCode.invalidPath:
            text = RestoreCopy.folderGone(session.cwd, machine: machine)
        case .rpc(let rpc)? where rpc.code == RPCErrorCode.notFound:
            remove(session, from: host)
            text = RestoreCopy.gone(label)
        case .rpc(let rpc)? where rpc.code == RPCErrorCode.invalidParams && rpc.reason == "not_installed":
            let agent = SnapshotMapper.agentName(session.agent, names: [:])
            text = RestoreCopy.failed(label, reason: RestoreCopy.notInstalled(agent, machine: machine))
        default:
            text = RestoreCopy.failed(label, reason: ServiceErrors.message(error))
        }
        notice = Notice(text: text, isError: true)
        restoreNotice = notice
    }

    private func restoredRow(_ session: Session, in machine: MachineSection) -> SessionRow {
        let known = machine.sessions.first { $0.agent == session.agent }?.agentName
        return SessionRow(
            id: session.id, hostID: machine.id, bib: session.bib, name: session.name, agent: session.agent,
            agentName: known ?? SnapshotMapper.agentName(session.agent, names: [:]), status: session.status,
            activity: session.activity, cwd: session.cwd, muted: session.muted, managed: session.managed,
            meta: session.meta, statusSince: Date(), exit: nil
        )
    }
}
