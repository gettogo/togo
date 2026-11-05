import Foundation
import TogoKit
import TogoPush

/// Pairing each machine's helper for pushes, and unpairing machines the user removed.
extension LiveServices {
    var pushAuthorization: PushRegistrar.Authorization {
        deps.registrar?.authorization ?? .notDetermined
    }

    func turnOnNotifications() async {
        await deps.registrar?.requestAuthorization()
        pairOnlineMachines()
    }

    /// The machine's monitor is connected, so an answer can go over SSH.
    func isOnline(_ id: HostID) -> Bool {
        latest[id]?.reach == .online && watches[id] != nil
    }

    /// Pairs the machine when its helper sends pushes and its pairing is out of date.
    func pair(_ record: HostRecord, capabilities: Set<String>) {
        guard let pairing = deps.pairing else { return }
        let helpers = deps.helpers
        Task {
            await pairing.machineOnline(hostID: record.id, name: record.profile.name, capabilities: capabilities) { params in
                try await helpers.call(record, prompter: nil) { try await $0.pair(params) }
            }
        }
    }

    /// Settings or tokens changed: every machine online now checks its pairing again.
    func pairOnlineMachines() {
        for (id, snapshot) in latest where snapshot.reach == .online {
            if let record = records[id], !record.noHelper { pair(record, capabilities: snapshot.capabilities) }
        }
    }

    /// Tells a removed machine's helper to forget this device, then closes its channel. Returns
    /// false when push is off, leaving the channel to the caller.
    func unpair(_ record: HostRecord, reachable: Bool) -> Bool {
        guard let pairing = deps.pairing else { return false }
        let helpers = deps.helpers
        let unpair = reachable && !record.noHelper ? HelperUnpair.now(record, helpers: helpers) : nil
        Task {
            await pairing.machineRemoved(hostID: record.id, connection: HelperUnpair.connection(record), unpair: unpair)
            await helpers.drop(record.id)
        }
        return true
    }

    /// Asks removed machines again to stop pushing. Only while the app is in front, and never
    /// with a question: the app's own connections sign in with saved credentials or fail.
    func retryUnpairs() {
        guard let pairing = deps.pairing, foreground else { return }
        let helpers = deps.helpers, current = Set(records.keys)
        Task { await pairing.retryUnpairs(HelperUnpair.retry(helpers: helpers, skipping: current)) }
    }

    /// The command text choice goes through each machine's relay at once; machines online now
    /// also pair again over SSH, which confirms it.
    func sendCommandChoice() {
        guard let pairing = deps.pairing else { return }
        Task { await pairing.commandChoiceChanged() }
    }

    func pushChanged() {
        guard let pairing = deps.pairing else { return }
        pairedMachines = pairing.pairedMachines
        confirmedCommands = pairing.confirmedCommands
        pendingCommands = pairing.pendingCommands
        pendingRemovals = pairing.pendingUnpairs.map { PendingRemoval(id: $0.id, name: $0.pairing.machineName) }
    }

    /// The saved password was kept for the unpair; it goes once the machine is not back in Togo.
    func unpaired(_ entry: PendingUnpair) {
        guard let id = entry.pairing.appHostID, records[id] == nil else { return }
        deps.forgetPassword(id)
    }

    func forgetRemoval(_ id: String) {
        deps.pairing?.forgetUnpair(hostId: id)
    }
}
