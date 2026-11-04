import Foundation

public enum KeyKind: String, Codable, Sendable {
    case secureEnclaveP256, ed25519, rsa, ecdsaP256
}

public struct KeyInfo: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var label: String
    public var kind: KeyKind
    public var publicKeyLine: String
    public var fingerprint: String
    public var createdAt: Date

    public init(id: String, label: String, kind: KeyKind, publicKeyLine: String, fingerprint: String, createdAt: Date) {
        self.id = id
        self.label = label
        self.kind = kind
        self.publicKeyLine = publicKeyLine
        self.fingerprint = fingerprint
        self.createdAt = createdAt
    }
}

public protocol KeyStore: Sendable {
    func list() async throws -> [KeyInfo]
    /// Secure Enclave P-256 when the device has one, else ed25519 in the Keychain.
    func generateDeviceKey(label: String) async throws -> KeyInfo
    func importKey(openSSH: String, passphrase: String?, label: String) async throws -> KeyInfo
    func delete(id: String) async throws
}
