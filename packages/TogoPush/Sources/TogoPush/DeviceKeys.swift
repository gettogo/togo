import CryptoKit
import Foundation
import Security
import Synchronization

/// The device's push identity: an id, an X25519 key that pushes are sealed to and an Ed25519
/// key that signs answers. Created on first use; `reset` replaces all three.
public protocol DeviceKeyStore: Sendable {
    func deviceID() throws -> String
    func agreementKey() throws -> Curve25519.KeyAgreement.PrivateKey
    func signingKey() throws -> Curve25519.Signing.PrivateKey
    func reset() throws
}

public enum DeviceKeyError: Error, Equatable, Sendable {
    case keychain(OSStatus)
    case corrupt
}

enum DeviceIdentity {
    /// `d_` on iPhone, `w_` on the Watch, then 26 random lowercase base32 characters.
    static var prefix: String {
        #if os(watchOS)
        "w_"
        #else
        "d_"
        #endif
    }

    static func newID() -> String {
        let random = Data((0..<20).map { _ in UInt8.random(in: .min ... .max) })
        return prefix + Base32.lowercase(random).prefix(26)
    }
}

/// Keys in the Keychain, readable after first unlock on this device only, with no user
/// presence so the notification service extension can open pushes on a locked phone. The
/// app and its extensions share them through `accessGroup`.
public final class KeychainDeviceKeys: DeviceKeyStore {
    private enum Account: String, CaseIterable {
        case deviceID = "device-id"
        case agreement = "x25519"
        case signing = "ed25519"
    }

    private let accessGroup: String?
    private let service: String
    private let lock = Mutex(())

    public init(accessGroup: String?, service: String = "app.togo.push") {
        self.accessGroup = accessGroup
        self.service = service
    }

    public func deviceID() throws -> String {
        let data = try value(.deviceID) { Data(DeviceIdentity.newID().utf8) }
        guard let id = String(data: data, encoding: .utf8) else { throw DeviceKeyError.corrupt }
        return id
    }

    public func agreementKey() throws -> Curve25519.KeyAgreement.PrivateKey {
        let data = try value(.agreement) { Curve25519.KeyAgreement.PrivateKey().rawRepresentation }
        guard let key = try? Curve25519.KeyAgreement.PrivateKey(rawRepresentation: data) else {
            throw DeviceKeyError.corrupt
        }
        return key
    }

    public func signingKey() throws -> Curve25519.Signing.PrivateKey {
        let data = try value(.signing) { Curve25519.Signing.PrivateKey().rawRepresentation }
        guard let key = try? Curve25519.Signing.PrivateKey(rawRepresentation: data) else {
            throw DeviceKeyError.corrupt
        }
        return key
    }

    public func reset() throws {
        try lock.withLock { _ in
            for account in Account.allCases {
                let status = SecItemDelete(query(account) as CFDictionary)
                guard status == errSecSuccess || status == errSecItemNotFound else {
                    throw DeviceKeyError.keychain(status)
                }
            }
        }
    }

    /// Reads one item, creating it when absent. Another process (the app or an extension)
    /// may add the same item first; the duplicate add then reads theirs.
    private func value(_ account: Account, create: () -> Data) throws -> Data {
        try lock.withLock { _ in
            if let existing = try read(account) { return existing }
            var attributes = query(account)
            attributes[kSecValueData as String] = create()
            attributes[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
            let status = SecItemAdd(attributes as CFDictionary, nil)
            if status == errSecDuplicateItem, let existing = try read(account) { return existing }
            guard status == errSecSuccess else { throw DeviceKeyError.keychain(status) }
            guard let stored = try read(account) else { throw DeviceKeyError.corrupt }
            return stored
        }
    }

    private func read(_ account: Account) throws -> Data? {
        var request = query(account)
        request[kSecReturnData as String] = true
        request[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        let status = SecItemCopyMatching(request as CFDictionary, &result)
        switch status {
        case errSecSuccess:
            guard let data = result as? Data else { throw DeviceKeyError.corrupt }
            return data
        case errSecItemNotFound:
            return nil
        default:
            throw DeviceKeyError.keychain(status)
        }
    }

    private func query(_ account: Account) -> [String: Any] {
        var query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account.rawValue,
            kSecUseDataProtectionKeychain as String: true,
            kSecAttrSynchronizable as String: false,
        ]
        if let accessGroup { query[kSecAttrAccessGroup as String] = accessGroup }
        return query
    }
}

/// Keys held in memory only, for tests and previews.
public final class InMemoryDeviceKeys: DeviceKeyStore {
    private struct Keys {
        var id = DeviceIdentity.newID()
        var agreement = Curve25519.KeyAgreement.PrivateKey()
        var signing = Curve25519.Signing.PrivateKey()
    }

    private let keys = Mutex(Keys())

    public init() {}

    public func deviceID() throws -> String { keys.withLock { $0.id } }
    public func agreementKey() throws -> Curve25519.KeyAgreement.PrivateKey { keys.withLock { $0.agreement } }
    public func signingKey() throws -> Curve25519.Signing.PrivateKey { keys.withLock { $0.signing } }
    public func reset() throws { keys.withLock { $0 = Keys() } }
}
