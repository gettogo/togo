import Foundation

/// A flat key-value store. `externalChanges` reports keys changed by someone
/// other than this process and has a single consumer.
public protocol KeyValueBackend: Sendable {
    func data(forKey key: String) -> Data?
    func set(_ data: Data?, forKey key: String)
    func allKeys() -> [String]
    var externalChanges: AsyncStream<[String]> { get }
}

/// iCloud key-value storage. Entries are capped at 1 MB each, far above a
/// machine record.
public final class UbiquitousBackend: KeyValueBackend, @unchecked Sendable {
    private let store: NSUbiquitousKeyValueStore
    public let externalChanges: AsyncStream<[String]>
    private nonisolated(unsafe) var observer: (any NSObjectProtocol)?

    public init(store: NSUbiquitousKeyValueStore = .default) {
        self.store = store
        let (stream, continuation) = AsyncStream<[String]>.makeStream()
        externalChanges = stream
        observer = NotificationCenter.default.addObserver(
            forName: NSUbiquitousKeyValueStore.didChangeExternallyNotification,
            object: store,
            queue: nil
        ) { note in
            let keys = note.userInfo?[NSUbiquitousKeyValueStoreChangedKeysKey] as? [String] ?? []
            if !keys.isEmpty { continuation.yield(keys) }
        }
        store.synchronize()
    }

    deinit {
        if let observer { NotificationCenter.default.removeObserver(observer) }
    }

    public func data(forKey key: String) -> Data? {
        store.data(forKey: key)
    }

    public func set(_ data: Data?, forKey key: String) {
        if let data {
            store.set(data, forKey: key)
        } else {
            store.removeObject(forKey: key)
        }
        store.synchronize()
    }

    public func allKeys() -> [String] {
        Array(store.dictionaryRepresentation.keys)
    }
}

/// In-process backend for tests and for running without iCloud.
public final class MemoryBackend: KeyValueBackend, @unchecked Sendable {
    private let lock = NSLock()
    private var values: [String: Data] = [:]
    private let continuation: AsyncStream<[String]>.Continuation
    public let externalChanges: AsyncStream<[String]>

    public init() {
        (externalChanges, continuation) = AsyncStream<[String]>.makeStream()
    }

    public func data(forKey key: String) -> Data? {
        lock.withLock { values[key] }
    }

    public func set(_ data: Data?, forKey key: String) {
        lock.withLock { values[key] = data }
    }

    public func allKeys() -> [String] {
        lock.withLock { Array(values.keys) }
    }

    /// Applies writes as if another device made them and reports the keys.
    public func simulateRemote(_ writes: [String: Data?]) {
        lock.withLock {
            for (key, data) in writes { values[key] = data }
        }
        continuation.yield(Array(writes.keys))
    }
}
