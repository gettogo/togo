import Foundation

/// A value with the metadata needed to merge edits from several devices.
/// A nil `value` is a deletion marker.
public struct Stamped<Value: Codable & Sendable>: Codable, Sendable {
    public var value: Value?
    public var updatedAt: Date
    public var deviceID: String

    public init(value: Value?, updatedAt: Date, deviceID: String) {
        self.value = value
        self.updatedAt = updatedAt
        self.deviceID = deviceID
    }

    /// Newest edit wins; equal timestamps fall back to the larger device id so
    /// every device picks the same winner.
    func supersedes(_ other: Stamped) -> Bool {
        if updatedAt != other.updatedAt { return updatedAt > other.updatedAt }
        return deviceID > other.deviceID
    }
}

/// One record per key, merged per record across devices. Reads and writes go to
/// the local backend; with sync on, writes reach the remote backend after a
/// short delay so bursts of edits become one upload. With sync off, nothing is
/// read from or written to the remote backend, from the very first call.
public actor SyncedRecordStore<Value: Codable & Sendable & Equatable> {
    public static var tombstoneLifetime: TimeInterval { 30 * 24 * 60 * 60 }

    public nonisolated let changes: AsyncStream<[String: Value]>

    private let prefix: String
    private let local: any KeyValueBackend
    private let remote: (any KeyValueBackend)?
    private let deviceID: String
    private let clock: @Sendable () -> Date
    private let debounce: Duration
    private let continuation: AsyncStream<[String: Value]>.Continuation
    private var syncEnabled: Bool
    private var dirty: Set<String> = []
    private var flushTask: Task<Void, Never>?
    private var flushGeneration = 0
    private var listener: Task<Void, Never>?
    private var reconciled = false

    public init(
        prefix: String,
        local: any KeyValueBackend,
        remote: (any KeyValueBackend)?,
        deviceID: String,
        clock: @escaping @Sendable () -> Date,
        debounce: Duration = .seconds(1),
        syncEnabled: Bool = true
    ) {
        self.prefix = prefix
        self.local = local
        self.remote = remote
        self.deviceID = deviceID
        self.clock = clock
        self.debounce = debounce
        self.syncEnabled = remote != nil && syncEnabled
        (changes, continuation) = AsyncStream<[String: Value]>.makeStream()
    }

    deinit {
        listener?.cancel()
        flushTask?.cancel()
    }

    public func all() -> [String: Value] {
        prepare()
        var result: [String: Value] = [:]
        for key in recordKeys(in: local) {
            if let value = read(key, from: local)?.value {
                result[String(key.dropFirst(prefix.count))] = value
            }
        }
        return result
    }

    /// The records on this device as they are, without merging iCloud or changing anything,
    /// for drawing the first screen before the store is first used.
    public nonisolated func localRecords() -> [String: Value] {
        var result: [String: Value] = [:]
        for key in local.allKeys() where key.hasPrefix(prefix) {
            guard let data = local.data(forKey: key),
                  let value = (try? Self.decoder.decode(Stamped<Value>.self, from: data))?.value else { continue }
            result[String(key.dropFirst(prefix.count))] = value
        }
        return result
    }

    public func put(_ id: String, _ value: Value) {
        prepare()
        let key = prefix + id
        if read(key, from: local)?.value == value { return }
        write(Stamped(value: value, updatedAt: nextStamp(for: key), deviceID: deviceID), key: key)
    }

    public func delete(_ id: String) {
        prepare()
        let key = prefix + id
        guard read(key, from: local)?.value != nil else { return }
        write(Stamped(value: nil, updatedAt: nextStamp(for: key), deviceID: deviceID), key: key)
    }

    /// Turning sync on pulls what other devices wrote and uploads local records.
    public func setSyncEnabled(_ on: Bool) {
        prepare()
        guard on != syncEnabled, let remote else { return }
        syncEnabled = on
        guard on else {
            flushTask?.cancel()
            flushTask = nil
            dirty.removeAll()
            return
        }
        reconcileAll(with: remote)
        notify()
    }

    /// Uploads pending records now instead of waiting for the delay.
    public func flush() {
        flushTask?.cancel()
        flushTask = nil
        flushGeneration += 1
        upload()
    }

    private func upload() {
        guard syncEnabled, let remote else { return }
        for key in dirty where !isUndecodable(key, in: remote) {
            remote.set(local.data(forKey: key), forKey: key)
        }
        dirty.removeAll()
    }

    private func timerFired(_ generation: Int) {
        guard generation == flushGeneration else { return }
        flushTask = nil
        upload()
    }

    /// Never older than the record being replaced, so a local edit wins the next
    /// merge even when this device's clock runs behind the one that wrote it.
    private func nextStamp(for key: String) -> Date {
        let now = clock()
        guard let existing = read(key, from: local) else { return now }
        return max(now, existing.updatedAt.addingTimeInterval(0.001))
    }

    private func write(_ stamped: Stamped<Value>, key: String) {
        local.set(encode(stamped), forKey: key)
        markDirty(key)
        notify()
    }

    private func markDirty(_ key: String) {
        guard syncEnabled, remote != nil else { return }
        dirty.insert(key)
        guard flushTask == nil else { return }
        let delay = debounce
        let generation = flushGeneration
        flushTask = Task { [weak self] in
            try? await Task.sleep(for: delay)
            if Task.isCancelled { return }
            await self?.timerFired(generation)
        }
    }

    /// Runs the first merge with the remote backend. Actor initializers cannot
    /// touch isolated state, so this and the change listener start on first use.
    private func prepare() {
        guard !reconciled else { return }
        reconciled = true
        guard let remote else { return }
        if syncEnabled { reconcileAll(with: remote) }
        let incoming = remote.externalChanges
        listener = Task { [weak self] in
            for await keys in incoming {
                await self?.remoteChanged(keys)
            }
        }
    }

    private func remoteChanged(_ keys: [String]) {
        prepare()
        guard syncEnabled, let remote else { return }
        let changed = keys.filter { $0.hasPrefix(prefix) }
        guard !changed.isEmpty else { return }
        var visibleChange = false
        for key in changed where merge(key, from: remote) { visibleChange = true }
        if visibleChange { notify() }
    }

    private func reconcileAll(with remote: any KeyValueBackend) {
        let keys = Set(recordKeys(in: remote)).union(recordKeys(in: local))
        for key in keys { merge(key, from: remote) }
    }

    /// Merges one key and returns whether the local copy changed. When the local
    /// copy wins or is the only one, it is queued for upload.
    @discardableResult
    private func merge(_ key: String, from remote: any KeyValueBackend) -> Bool {
        // A record this build cannot decode may come from a newer app version;
        // leave it as it is on both sides.
        if isUndecodable(key, in: remote) || isUndecodable(key, in: local) { return false }
        let theirs = read(key, from: remote)
        let ours = read(key, from: local)
        switch (ours, theirs) {
        case (nil, nil):
            return false
        case let (ours?, nil):
            if !isExpired(ours) { markDirty(key) }
            return false
        case let (nil, theirs?):
            guard !isExpired(theirs) else { return false }
            local.set(remote.data(forKey: key), forKey: key)
            return true
        case let (ours?, theirs?):
            if theirs.supersedes(ours) {
                local.set(remote.data(forKey: key), forKey: key)
                return true
            }
            if ours.supersedes(theirs) { markDirty(key) }
            return false
        }
    }

    private func notify() {
        continuation.yield(all())
    }

    private func isExpired(_ stamped: Stamped<Value>) -> Bool {
        stamped.value == nil && clock().timeIntervalSince(stamped.updatedAt) > Self.tombstoneLifetime
    }

    private func recordKeys(in backend: any KeyValueBackend) -> [String] {
        backend.allKeys().filter { $0.hasPrefix(prefix) }
    }

    private func isUndecodable(_ key: String, in backend: any KeyValueBackend) -> Bool {
        guard let data = backend.data(forKey: key) else { return false }
        return (try? Self.decoder.decode(Stamped<Value>.self, from: data)) == nil
    }

    private func read(_ key: String, from backend: any KeyValueBackend) -> Stamped<Value>? {
        guard let data = backend.data(forKey: key) else { return nil }
        guard let stamped = try? Self.decoder.decode(Stamped<Value>.self, from: data) else { return nil }
        if isExpired(stamped) {
            backend.set(nil, forKey: key)
            return nil
        }
        return stamped
    }

    private func encode(_ stamped: Stamped<Value>) -> Data? {
        try? Self.encoder.encode(stamped)
    }

    private static var encoder: JSONEncoder {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.sortedKeys]
        return encoder
    }

    private static var decoder: JSONDecoder { JSONDecoder() }
}
