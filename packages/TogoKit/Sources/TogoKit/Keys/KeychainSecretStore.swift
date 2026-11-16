import Foundation
@preconcurrency import LocalAuthentication
import Security

/// Generic passwords in the data-protection keychain, never synced and never restored to
/// another device, so a backup cannot carry an SSH key off the phone.
public final class KeychainSecretStore: SecretStore {
    private let service: String
    private let operations: KeychainSecretOperations

    public init(service: String = "app.togo.keys") {
        self.service = service
        operations = .live
    }

    init(service: String, operations: KeychainSecretOperations) {
        self.service = service
        self.operations = operations
    }

    public func set(_ data: Data, for account: String, requireUserPresence: Bool) throws {
        var attributes: [CFString: Any] = [kSecValueData: data]
        if requireUserPresence {
            var error: Unmanaged<CFError>?
            guard let access = SecAccessControlCreateWithFlags(
                nil, kSecAttrAccessibleWhenUnlockedThisDeviceOnly, .userPresence, &error
            ) else {
                throw SecretStoreError(status: errSecParam)
            }
            attributes[kSecAttrAccessControl] = access
        } else {
            attributes[kSecAttrAccessible] = kSecAttrAccessibleWhenUnlockedThisDeviceOnly
        }
        let query = baseQuery(account)
        var status = operations.update(query as CFDictionary, attributes as CFDictionary)
        if status == errSecItemNotFound {
            status = operations.add(query.merging(attributes, uniquingKeysWith: { _, new in new }) as CFDictionary)
            // Another writer may have inserted the same account after our lookup. Retry
            // only that atomic update, never delete an existing value to make room.
            if status == errSecDuplicateItem {
                status = operations.update(query as CFDictionary, attributes as CFDictionary)
            }
        }
        guard status == errSecSuccess else { throw SecretStoreError(status: status) }
    }

    public func get(_ account: String, context: LAContext?) throws -> Data? {
        var query = baseQuery(account)
        query[kSecReturnData] = true
        query[kSecMatchLimit] = kSecMatchLimitOne
        if let context { query[kSecUseAuthenticationContext] = context }
        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        switch status {
        case errSecSuccess: return result as? Data
        case errSecItemNotFound: return nil
        default: throw SecretStoreError(status: status)
        }
    }

    public func delete(_ account: String) throws {
        let status = operations.delete(baseQuery(account) as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else { throw SecretStoreError(status: status) }
    }

    private func baseQuery(_ account: String) -> [CFString: Any] {
        [
            kSecClass: kSecClassGenericPassword,
            kSecAttrService: service,
            kSecAttrAccount: account,
            kSecUseDataProtectionKeychain: true,
        ]
    }
}

/// System calls are kept together so error paths can be exercised without touching a
/// developer's real keychain. Calls remain synchronous and preserve Security's statuses.
struct KeychainSecretOperations: Sendable {
    var add: @Sendable (CFDictionary) -> OSStatus
    var update: @Sendable (CFDictionary, CFDictionary) -> OSStatus
    var delete: @Sendable (CFDictionary) -> OSStatus

    static let live = Self(
        add: { SecItemAdd($0, nil) }, update: { SecItemUpdate($0, $1) }, delete: { SecItemDelete($0) }
    )
}
