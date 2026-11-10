import Foundation

/// What makes two machine records the same machine: an SSH host key both presented, or the
/// Togo helper answering on both. A name or an address never decides it: two machines can
/// share a name, and one machine answers on many addresses.
public struct MachineIdentity: Equatable, Sendable {
    public var hostKeys: Set<String>
    public var helperHostID: String?
    /// The login. A machine has one host key for every account on it, so a host key match
    /// counts only for the same account.
    public var user: String

    public init(hostKeys: Set<String> = [], helperHostID: String? = nil, user: String) {
        self.hostKeys = hostKeys
        self.helperHostID = helperHostID
        self.user = user
    }

    /// The record's own identity plus what this device knows about it: the fingerprints it
    /// trusts for the record's address, and the helper it paired with.
    public init(_ record: HostRecord, trusted: [String] = [], helperHostID: String? = nil) {
        hostKeys = Set(record.hostKeys ?? []).union(trusted)
        self.helperHostID = record.helperHostID ?? helperHostID
        user = record.profile.endpoint.user
    }

    /// Two known helper ids decide on their own: a different helper is a different machine,
    /// even behind the same host key. Otherwise one host key in common, for the same account.
    public func isSameMachine(as other: MachineIdentity) -> Bool {
        if let mine = helperHostID, let theirs = other.helperHostID { return mine == theirs }
        return user == other.user && !hostKeys.isDisjoint(with: other.hostKeys)
    }

    /// Whether both name a helper and the helpers differ.
    func conflicts(with other: MachineIdentity) -> Bool {
        guard let mine = helperHostID, let theirs = other.helperHostID else { return false }
        return mine != theirs
    }
}

/// Folding records for one machine into one.
public enum MachineMerge {
    /// Duplicate records of one machine: `kept` already holds what the others had.
    public struct Duplicates: Equatable, Sendable {
        public var kept: HostRecord
        public var dropped: [UUID]

        public init(kept: HostRecord, dropped: [UUID]) {
            self.kept = kept
            self.dropped = dropped
        }
    }

    /// The stored machine that `record` is a second record of, or nil. A record already
    /// stored under its own id is an edit of that machine, never a duplicate.
    public static func existing(
        for record: HostRecord, in stored: [HostRecord], identity: (HostRecord) -> MachineIdentity
    ) -> HostRecord? {
        guard !stored.contains(where: { $0.id == record.id }) else { return nil }
        let mine = identity(record)
        return stored.first { identity($0).isSameMachine(as: mine) }
    }

    /// `kept` with what `other` knew added: its addresses, host keys, helper and wake target.
    /// The id, name, address and sign-in stay `kept`'s. `other` may be `kept` as set up on
    /// another device, under the same id.
    public static func merged(_ kept: HostRecord, absorbing other: HostRecord) -> HostRecord {
        var result = kept
        // Routes share the saved address's port, so another port's addresses stay out.
        if other.profile.endpoint.port == kept.profile.endpoint.port {
            var seen = Set(kept.profile.routes.map(\.id))
            seen.insert(kept.profile.endpoint.host.lowercased())
            let theirs = [HostRoute.manual(other.profile.endpoint.host)] + other.profile.routes
            result.profile.routes += theirs.filter { seen.insert($0.id).inserted }
            if result.profile.lastRoute == nil { result.profile.lastRoute = other.profile.lastRoute }
        }
        let keys = Set(kept.hostKeys ?? []).union(other.hostKeys ?? [])
        result.hostKeys = keys.isEmpty ? nil : keys.sorted()
        result.helperHostID = kept.helperHostID ?? other.helperHostID
        if kept.noHelper, !other.noHelper {
            result.noHelper = false
            result.helperPath = other.helperPath
        } else if result.helperPath == nil {
            result.helperPath = other.helperPath
        }
        result.profile.etAvailable = kept.profile.etAvailable || other.profile.etAvailable
        result.wake = kept.wake ?? other.wake
        result.addedAt = [kept.addedAt, other.addedAt].compactMap(\.self).min()
        var ids = (kept.mergedIDs ?? []) + [other.id] + (other.mergedIDs ?? [])
        ids.removeAll { $0 == kept.id }
        var seenIDs = Set<UUID>()
        ids = ids.filter { seenIDs.insert($0).inserted }
        result.mergedIDs = ids.isEmpty ? nil : ids
        return result
    }

    /// The groups of records that are one machine, each folded into its oldest record.
    /// `older(a, b)` says whether `a` was added before `b`. A record joins a group when it is
    /// the same machine as one of its records and no record there names a different helper.
    public static func duplicates(
        in records: [HostRecord], identity: (HostRecord) -> MachineIdentity,
        older: (HostRecord, HostRecord) -> Bool
    ) -> [Duplicates] {
        var groups: [[(record: HostRecord, identity: MachineIdentity)]] = []
        for record in records.sorted(by: older) {
            let mine = identity(record)
            let index = groups.firstIndex { group in
                group.contains { $0.identity.isSameMachine(as: mine) } && !group.contains { $0.identity.conflicts(with: mine) }
            }
            if let index {
                groups[index].append((record, mine))
            } else {
                groups.append([(record, mine)])
            }
        }
        return groups.filter { $0.count > 1 }.map { group in
            let kept = group.dropFirst().reduce(group[0].record) { merged($0, absorbing: $1.record) }
            return Duplicates(kept: kept, dropped: group.dropFirst().map(\.record.id))
        }
    }

    /// Records added earlier first; records from before `addedAt` was kept count as earliest.
    public static func addedFirst(_ a: HostRecord, _ b: HostRecord) -> Bool {
        let first = a.addedAt ?? .distantPast, second = b.addedAt ?? .distantPast
        return first != second ? first < second : a.id.uuidString < b.id.uuidString
    }
}
