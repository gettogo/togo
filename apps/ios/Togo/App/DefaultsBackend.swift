import Foundation
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
