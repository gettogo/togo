import Foundation
import os
import TogoKit

/// One record per machine. Saving a machine that is already here under another record merges
/// into that record, and each launch folds records that turn out to be one machine. A machine
/// is the same only by an SSH host key or a helper id, never by its name.
extension LiveServices {
    private static let duplicatesLog = Logger(subsystem: "app.togo", category: "machines")

    func saveHost(_ record: HostRecord) async throws {
        _ = try await addHost(record)
    }

    func removeHost(_ id: HostID) async throws {
        await hostStore.remove(id)
        reconcile(await hostStore.hosts())
    }

    /// Saves a machine and says where it went: its own record, or the record already here for
    /// the same machine, which then also signs in with this device's key.
    func addHost(_ record: HostRecord) async throws -> HostStore.SaveOutcome {
        let stored = await hostStore.hosts()
        var record = record
        let current = stored.first { $0.id == record.id }
        if let current { record = Self.keepingIdentity(deviceMachines.editedVersion(of: record), from: current) }
        record.helperHostID = record.helperHostID ?? deps.pairing?.helperHostID(for: record.id)
        record.hostKeys = Self.union(record.hostKeys, await trustedHostKeys(for: record))
        // An edit saves under its own id and is never merged, so only a new machine is compared.
        let identity = current == nil ? await identities(stored + [record]) : [:]
        let outcome = await hostStore.save(record) { identity[$0.id] ?? MachineIdentity($0) }
        if case .merged(let kept) = outcome, case .key(let keyID) = record.profile.auth, kept.profile.auth != record.profile.auth {
            deviceMachines.setUp(kept.id, keyID: keyID)
        }
        reconcile(await hostStore.hosts())
        return outcome
    }

    /// Folds the records that are one machine into the oldest, then writes down what tells
    /// each machine apart, so the other devices can do the same. Safe to run every launch.
    func mergeDuplicates() async {
        let stored = await hostStore.hosts()
        guard !stored.isEmpty else { return }
        let identity = await identities(stored)
        // Records from before `addedAt` was kept: one that signs in with this device's own key
        // was added here, after the ones that synced in from elsewhere.
        let addedHere = Set(stored.filter { deviceMachines.addedHere($0) }.map(\.id))
        let merges = await hostStore.mergeDuplicates(identity: { identity[$0.id] ?? MachineIdentity($0) }) { a, b in
            let first = a.addedAt ?? .distantPast, second = b.addedAt ?? .distantPast
            if first != second { return first < second }
            let mine = addedHere.contains(a.id), theirs = addedHere.contains(b.id)
            if mine != theirs { return theirs }
            return a.id.uuidString < b.id.uuidString
        }
        for merge in merges {
            for dropped in stored where merge.dropped.contains(dropped.id) {
                deviceMachines.keepKey(of: dropped, for: merge.kept.id)
            }
        }
        if !merges.isEmpty {
            Self.duplicatesLog.notice("Merged \(merges.map(\.dropped.count).reduce(0, +), privacy: .public) duplicate machine records")
        }
        for var record in await hostStore.hosts() {
            let known = identity[record.id]
            let keys = Self.union(record.hostKeys, known.map { Array($0.hostKeys) })
            let helper = record.helperHostID ?? known?.helperHostID
            guard keys != record.hostKeys || helper != record.helperHostID else { continue }
            record.hostKeys = keys
            record.helperHostID = helper
            await hostStore.save(record)
        }
    }

    /// The machine's pairing names its helper: the record keeps that, for telling it apart.
    func learnHelper(_ id: HostID) async {
        guard let helper = deps.pairing?.helperHostID(for: id), records[id]?.helperHostID != helper,
              var current = await hostStore.hosts().first(where: { $0.id == id }), current.helperHostID != helper
        else { return }
        current.helperHostID = helper
        try? await saveHost(current)
    }

    /// A machine saved without the helper turned out to have one. The record is saved with its
    /// path, which restarts the machine's watch on the helper connection.
    func helperFound(_ id: HostID, path: String) async {
        guard var current = await hostStore.hosts().first(where: { $0.id == id }), current.noHelper else { return }
        current.noHelper = false
        current.helperPath = path
        do {
            try await saveHost(current)
        } catch {
            Self.duplicatesLog.error("Could not save the helper found on a machine: \(error.localizedDescription, privacy: .public)")
        }
    }

    /// Records gone from `stored` that live on in a record still there, by the id they went
    /// into, after handing what this device keeps for each to that record. Those were merged,
    /// not removed: their machine keeps this device's key and pairing.
    func handOverMerged(_ stored: [HostRecord]) -> [HostID: HostID] {
        let merged = mergedInto(stored)
        for (dropped, kept) in merged {
            deviceMachines.merged(dropped, into: kept)
        }
        return merged
    }

    private func mergedInto(_ stored: [HostRecord]) -> [HostID: HostID] {
        let present = Set(stored.map(\.id))
        var result: [HostID: HostID] = [:]
        for record in stored {
            for id in record.mergedIDs ?? [] where !present.contains(id) {
                result[id] = record.id
            }
        }
        // A deletion from a merge names the record it went into, which may not be here yet: the
        // machine is still in Togo, so nothing of it is unpaired or removed, and it is adopted
        // under that id when it arrives.
        for (id, kept) in hostStore.mergedAway() where !present.contains(id) && result[id] == nil {
            result[id] = kept
        }
        // A deletion from an older build that syncs in before its merged record.
        for (id, gone) in records where !present.contains(id) && result[id] == nil {
            let identity = MachineIdentity(gone)
            if let kept = stored.first(where: { MachineIdentity($0).isSameMachine(as: identity) }) {
                result[id] = kept.id
            }
        }
        return result
    }

    // MARK: Identity

    /// Fingerprints this device trusts for the record's address. A machine behind a jump host
    /// shares its address with others, such as `localhost`, so it counts on its record alone.
    private func trustedHostKeys(for record: HostRecord) async -> [String] {
        guard record.profile.jumps.isEmpty else { return [] }
        return await deps.trustedHostKeys(record.profile.endpoint)
    }

    private func identities(_ records: [HostRecord]) async -> [HostID: MachineIdentity] {
        var result: [HostID: MachineIdentity] = [:]
        for record in records {
            result[record.id] = MachineIdentity(
                record, trusted: await trustedHostKeys(for: record), helperHostID: deps.pairing?.helperHostID(for: record.id)
            )
        }
        return result
    }

    /// An edit keeps what the stored record knew about the machine and when it was added.
    static func keepingIdentity(_ record: HostRecord, from current: HostRecord) -> HostRecord {
        var record = record
        record.hostKeys = union(record.hostKeys, current.hostKeys)
        record.helperHostID = record.helperHostID ?? current.helperHostID
        record.addedAt = current.addedAt
        var seen = Set<UUID>()
        let merged = ((current.mergedIDs ?? []) + (record.mergedIDs ?? [])).filter { seen.insert($0).inserted }
        record.mergedIDs = merged.isEmpty ? nil : merged
        return record
    }

    private static func union(_ first: [String]?, _ second: [String]?) -> [String]? {
        let all = Set(first ?? []).union(second ?? [])
        guard !all.isEmpty else { return nil }
        // An unchanged list keeps its order, so it is not saved again.
        if let first, Set(first) == all { return first }
        return all.sorted()
    }
}
