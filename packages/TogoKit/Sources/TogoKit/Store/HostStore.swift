import Foundation

/// The user's machines, one synced record per machine.
public actor HostStore {
    public nonisolated let changes: AsyncStream<[HostRecord]>

    private let records: SyncedRecordStore<HostRecord>
    private let pump: Task<Void, Never>

    public init(records: SyncedRecordStore<HostRecord>) {
        self.records = records
        let (stream, continuation) = AsyncStream<[HostRecord]>.makeStream()
        changes = stream
        let incoming = records.changes
        pump = Task {
            for await all in incoming {
                continuation.yield(Self.ordered(all))
            }
            continuation.finish()
        }
    }

    deinit {
        pump.cancel()
    }

    public func hosts() async -> [HostRecord] {
        Self.ordered(await records.all())
    }

    /// The machines saved on this device, read at once and in order. iCloud merges later.
    public nonisolated func localHosts() -> [HostRecord] {
        Self.ordered(records.localRecords())
    }

    public func save(_ record: HostRecord) async {
        await records.put(Self.key(record.id), record)
    }

    public func remove(_ id: UUID) async {
        await records.delete(Self.key(id))
    }

    /// What saving a machine did.
    public enum SaveOutcome: Equatable, Sendable {
        case saved(HostRecord)
        /// The machine was already stored under another record, which now holds what was saved.
        case merged(into: HostRecord)
    }

    /// Saves `record`, or folds it into the stored record for the same machine when one is
    /// there under another id. An edit, under its own id, is saved as it is.
    public func save(_ record: HostRecord, identity: @Sendable (HostRecord) -> MachineIdentity) async -> SaveOutcome {
        if let existing = MachineMerge.existing(for: record, in: await hosts(), identity: identity) {
            let merged = MachineMerge.merged(existing, absorbing: record)
            await records.put(Self.key(merged.id), merged)
            return .merged(into: merged)
        }
        await records.put(Self.key(record.id), record)
        return .saved(record)
    }

    /// Folds every group of records for one machine into its oldest record; the others are
    /// deleted here and, through sync, everywhere. Records never merge on a name or an address
    /// alone. Returns the merges made; safe to run every launch.
    @discardableResult
    public func mergeDuplicates(
        identity: @Sendable (HostRecord) -> MachineIdentity,
        older: @Sendable (HostRecord, HostRecord) -> Bool = MachineMerge.addedFirst
    ) async -> [MachineMerge.Duplicates] {
        let found = MachineMerge.duplicates(in: await hosts(), identity: identity, older: older)
        for duplicates in found {
            await records.put(Self.key(duplicates.kept.id), duplicates.kept, deleting: duplicates.dropped.map(Self.key))
        }
        return found
    }

    private static func key(_ id: UUID) -> String { id.uuidString }

    private static func ordered(_ all: [String: HostRecord]) -> [HostRecord] {
        all.values.sorted {
            let order = $0.profile.name.localizedCaseInsensitiveCompare($1.profile.name)
            return order == .orderedSame ? $0.id.uuidString < $1.id.uuidString : order == .orderedAscending
        }
    }
}
