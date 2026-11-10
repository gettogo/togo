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

    /// Pairs the machine when its helper sends pushes and its pairing is out of date, or, just
    /// `reconnected`, when an activity of its runs here, so a restarted helper has its token.
    func pair(_ record: HostRecord, capabilities: Set<String>, reconnected: Bool = false) {
        guard let pairing = deps.pairing else { return }
        let helpers = deps.helpers
        Task {
            await pairing.machineOnline(
                hostID: record.id, name: record.profile.name, capabilities: capabilities,
                sshKey: pairing.deviceKey(record)?.publicKey, reconnected: reconnected
            ) { params in
                try await helpers.call(record, prompter: nil) { try await $0.pair(params) }
            }
            await self.learnHelper(record.id)
        }
    }

    /// Settings or tokens changed: every machine online now checks its pairing again.
    func pairOnlineMachines() {
        for (id, snapshot) in latest where snapshot.reach == .online {
            if let record = records[id], !record.noHelper { pair(record, capabilities: snapshot.capabilities) }
        }
    }

    /// This device's key on the machine when removing the machine takes it off: a key Togo
    /// made, and no other machine in Togo at the same address and user signs in with it.
    func removableKey(_ record: HostRecord) -> DeviceKeyLine? {
        guard let pairing = deps.pairing, let key = pairing.deviceKey(record), key.helperCanRemove,
              !pairing.stillUses(key, at: record, besides: record.id, in: Array(records.values))
        else { return nil }
        return key
    }

    /// Whether the remove question can promise the key goes: the machine has a helper that can
    /// take it off, as far as the app knows.
    func removesKey(_ id: HostID) -> Bool {
        guard let record = records[id], !record.noHelper, removableKey(record) != nil else { return false }
        let known = latest[id]?.capabilities ?? []
        return known.isEmpty || known.contains(Capability.authkeys)
    }

    /// Tells a removed machine's helper to forget this device, then takes this device's key
    /// off it over the same channel, then closes the channel. The key goes last so the machine
    /// can still be reached if the unpair has to be asked again. Returns false when push is
    /// off, leaving the channel to the caller.
    func unpair(_ record: HostRecord, reachable: Bool) -> Bool {
        guard let pairing = deps.pairing else { return false }
        let helpers = deps.helpers
        let live = reachable && !record.noHelper
        let unpair = live ? HelperUnpair.now(record, helpers: helpers) : nil
        // Capabilities are empty for a machine not seen since launch: a retry finds out.
        let known = latest[record.id]?.capabilities ?? []
        let step = removableKey(record).map { key in
            KeyRemovalStep(
                key: key, name: record.profile.name,
                supported: !record.noHelper && (known.isEmpty || known.contains(Capability.authkeys)),
                removeKey: live ? HelperUnpair.removeKey(record, helpers: helpers) : nil
            )
        }
        pairing.enqueue {
            await pairing.machineRemoved(
                hostID: record.id, connection: HelperUnpair.connection(record), key: step, unpair: unpair
            )
            await helpers.drop(record.id)
        }
        return true
    }

    /// Asks removed machines again to stop pushing, and to drop this device's key after. Only
    /// while the app is in front, and never with a question: the app's own connections sign in
    /// with saved credentials or fail. Returns the queued work, which tests wait on.
    @discardableResult func retryUnpairs() -> Task<Void, Never>? {
        guard let pairing = deps.pairing, foreground else { return nil }
        let helpers = deps.helpers, current = Set(records.keys), all = Array(records.values)
        pairing.enqueue {
            let opened = Set(
                pairing.pendingUnpairs.compactMap { HelperUnpair.machine($0.connection) }
                    + pairing.pendingKeyRemovals.filter { !$0.manual }.map(\.id)
            ).subtracting(current)
            await pairing.retryUnpairs(
                current: all, removeKey: HelperUnpair.retryKey(helpers: helpers),
                HelperUnpair.retry(helpers: helpers, skipping: current)
            )
            for id in opened { await helpers.drop(id) }
        }
        return Task { await pairing.settled() }
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
            + pairing.pendingKeyRemovals.map {
                PendingRemoval(
                    id: PendingRemoval.keyID($0.id), name: $0.machineName,
                    kind: .key(comment: $0.key.comment, manual: $0.manual)
                )
            }
    }

    /// The saved password was kept for the unpair; it goes once the machine is not back in Togo.
    func unpaired(_ entry: PendingUnpair) {
        guard let id = entry.pairing.appHostID, records[id] == nil else { return }
        deps.forgetPassword(id)
    }

    func forgetRemoval(_ id: String) {
        if let host = PendingRemoval.hostID(fromKeyID: id) {
            _ = deps.pairing?.forgetKeyRemoval(host)
        } else {
            deps.pairing?.forgetUnpair(hostId: id)
        }
    }
}
