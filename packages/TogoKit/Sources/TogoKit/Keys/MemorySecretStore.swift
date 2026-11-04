import Foundation
@preconcurrency import LocalAuthentication
import Synchronization

/// Tests and previews: a command-line test binary cannot reach the data-protection keychain.
public final class MemorySecretStore: SecretStore {
    private let items = Mutex<[String: Data]>([:])

    public init() {}

    public func set(_ data: Data, for account: String, requireUserPresence: Bool) throws {
        items.withLock { $0[account] = data }
    }

    public func get(_ account: String, context: LAContext?) throws -> Data? {
        items.withLock { $0[account] }
    }

    public func delete(_ account: String) throws {
        _ = items.withLock { $0.removeValue(forKey: account) }
    }
}
