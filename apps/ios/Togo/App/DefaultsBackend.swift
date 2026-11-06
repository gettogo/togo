import Foundation
import Synchronization
import TogoKit

/// The on-device copy of synced records, in the app's user defaults. Nothing else writes
/// these keys, so there are never external changes to report.
/// `@unchecked Sendable`: its stored properties are immutable after init, and `UserDefaults`
/// is documented as thread safe, but the SDK does not mark it `Sendable`.
final class DefaultsBackend: KeyValueBackend, @unchecked Sendable {
    let externalChanges: AsyncStream<[String]>
    private let defaults: UserDefaults
    private let namespace: String

    init(defaults: UserDefaults = .standard, namespace: String = "store.") {
        self.defaults = defaults
        self.namespace = namespace
        let (stream, continuation) = AsyncStream<[String]>.makeStream()
        continuation.finish()
        externalChanges = stream
    }

    func data(forKey key: String) -> Data? {
        defaults.data(forKey: namespace + key)
    }

    func set(_ data: Data?, forKey key: String) {
        defaults.set(data, forKey: namespace + key)
    }

    func allKeys() -> [String] {
        defaults.dictionaryRepresentation().keys
            .filter { $0.hasPrefix(namespace) }
            .map { String($0.dropFirst(namespace.count)) }
    }
}

/// A backend made the first time it is used. iCloud key-value storage syncs as it is made,
/// so wrapping it keeps that off the launch: records are first used once the app is up.
final class FirstUseBackend: KeyValueBackend, Sendable {
    let externalChanges: AsyncStream<[String]>
    private let changes: AsyncStream<[String]>.Continuation
    private let make: @Sendable () -> any KeyValueBackend
    private let made = Mutex<(any KeyValueBackend)?>(nil)

    init(_ make: @escaping @Sendable () -> any KeyValueBackend) {
        self.make = make
        (externalChanges, changes) = AsyncStream<[String]>.makeStream()
    }

    private var backend: any KeyValueBackend {
        let (backend, isNew) = made.withLock { made -> (any KeyValueBackend, Bool) in
            if let made { return (made, false) }
            let backend = make()
            made = backend
            return (backend, true)
        }
        if isNew {
            let changes = self.changes
            let incoming = backend.externalChanges
            Task {
                for await keys in incoming { changes.yield(keys) }
                changes.finish()
            }
        }
        return backend
    }

    func data(forKey key: String) -> Data? { backend.data(forKey: key) }
    func set(_ data: Data?, forKey key: String) { backend.set(data, forKey: key) }
    func allKeys() -> [String] { backend.allKeys() }
}
