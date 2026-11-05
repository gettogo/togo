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

    public func save(_ record: HostRecord) async {
        await records.put(Self.key(record.id), record)
    }

    public func remove(_ id: UUID) async {
        await records.delete(Self.key(id))
    }

    private static func key(_ id: UUID) -> String { id.uuidString }

    private static func ordered(_ all: [String: HostRecord]) -> [HostRecord] {
        all.values.sorted {
            let order = $0.profile.name.localizedCaseInsensitiveCompare($1.profile.name)
            return order == .orderedSame ? $0.id.uuidString < $1.id.uuidString : order == .orderedAscending
        }
    }
}
