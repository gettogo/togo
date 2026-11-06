import Foundation
import TogoKit
import TogoPush

/// Run again: the helper starts a stopped session's command in the same pane, keeping its id,
/// bib, name and folder, so the row stays where it is and only its status changes.
extension AppStore {
    func runAgain(_ row: SessionRow, tools: any MachineTools) async {
        await runAgain(hostID: row.hostID, sessionID: row.id, label: SessionActions.label(row), tools: tools)
    }

    /// From a notification, which may arrive before the machine has reported its sessions.
    func runAgain(hostID: HostID, sessionID: String, tools: any MachineTools) async {
        let label = session(hostID: hostID, sessionID: sessionID).map(SessionActions.label) ?? sessionID
        await runAgain(hostID: hostID, sessionID: sessionID, label: label, tools: tools)
    }

    private func runAgain(hostID: HostID, sessionID: String, label: String, tools: any MachineTools) async {
        do {
            try await relaunch(hostID: hostID, sessionID: sessionID, tools: tools)
        } catch {
            notice = Notice(text: LifecycleCopy.runAgainFailed(label, reason: ServiceErrors.message(error)), isError: true)
        }
    }

    /// Run again for the Watch, with tools that never ask the user anything. A channel that
    /// would have to ask, or that dropped, means the phone is needed.
    func runAgainFromWatch(hostID: HostID, sessionID: String, label: String, tools: any MachineTools) async
        -> ReplyOutcome {
        do {
            try await relaunch(hostID: hostID, sessionID: sessionID, tools: tools)
            return .sent
        } catch let error as HelperError {
            switch error {
            case .closed, .timeout, .notReady: return .needsPhone
            case .conflict: return .refused(LifecycleCopy.runAgainFailed(label, reason: ServiceErrors.message(error)))
            default: return .failed(LifecycleCopy.runAgainFailed(label, reason: ServiceErrors.message(error)))
            }
        } catch let error as LiveServiceError {
            return .failed(LifecycleCopy.runAgainFailed(label, reason: ServiceErrors.message(error)))
        } catch {
            return .needsPhone
        }
    }

    /// Starts the session again and shows it running. A second start while one runs does nothing.
    private func relaunch(hostID: HostID, sessionID: String, tools: any MachineTools) async throws {
        let id = TabID(hostID: hostID, sessionID: sessionID)
        guard !relaunching.contains(id) else { return }
        relaunching.insert(id)
        defer { relaunching.remove(id) }
        let session = try await tools.call(hostID) { try await $0.relaunch(id: sessionID) }
        let now = Date()
        func started(_ row: inout SessionRow) {
            row.status = session.status
            row.exit = session.exit
            row.activity = session.activity
            row.statusSince = now
        }
        if let row = self.session(hostID: hostID, sessionID: sessionID) {
            update(row, started)
        } else if let index = controlCenter.archived.firstIndex(where: { $0.hostID == hostID && $0.id == sessionID }),
                  let m = controlCenter.machines.firstIndex(where: { $0.id == hostID }) {
            // Stopped long enough to be archived: running again brings it back beside the live rows.
            var row = controlCenter.archived.remove(at: index)
            started(&row)
            controlCenter.machines[m].sessions.append(row)
            controlCenter.machines[m].sessions.sort(by: Self.bibOrder)
        }
    }

    func session(hostID: HostID, sessionID: String) -> SessionRow? {
        controlCenter.machines.first { $0.id == hostID }?.sessions.first { $0.id == sessionID }
    }
}
