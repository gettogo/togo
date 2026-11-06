import CryptoKit
import Foundation
@preconcurrency import LocalAuthentication
@preconcurrency import NIOSSH
import Security
import Synchronization

/// SSH keys whose private halves never leave the device: Secure Enclave keys by
/// construction, everything else in the Keychain with `ThisDeviceOnly` and user presence.
public final class KeychainKeyStore: KeyStore {
    private static let indexAccount = "keys.index"

    private let secrets: any SecretStore
    private let biometrics: BiometricSession
    // Serialises read-modify-write of the index; the secret store itself is thread safe.
    private let indexLock = Mutex(())

    public init(secrets: any SecretStore, biometrics: BiometricSession) {
        self.secrets = secrets
        self.biometrics = biometrics
    }

    public func list() async throws -> [KeyInfo] {
        try readIndex()
    }

    public func generateDeviceKey(label: String) async throws -> KeyInfo {
        if SecureEnclave.isAvailable {
            var error: Unmanaged<CFError>?
            guard let access = SecAccessControlCreateWithFlags(
                nil, kSecAttrAccessibleWhenUnlockedThisDeviceOnly, [.privateKeyUsage, .userPresence], &error
            ) else {
                throw SecretStoreError(status: errSecParam)
            }
            let key = try SecureEnclave.P256.Signing.PrivateKey(accessControl: access)
            // The data representation is an opaque handle the Secure Enclave alone can use,
            // so storing it without a presence check exposes nothing.
            let material = KeyMaterial(kind: .secureEnclaveP256, data: key.dataRepresentation)
            return try add(material, publicKey: NIOSSHPrivateKey(secureEnclaveP256Key: key).publicKey, label: label)
        }
        let key = Curve25519.Signing.PrivateKey()
        let material = KeyMaterial(kind: .ed25519, data: key.rawRepresentation)
        return try add(material, publicKey: NIOSSHPrivateKey(ed25519Key: key).publicKey, label: label)
    }

    public func importKey(openSSH: String, passphrase: String?, label: String) async throws -> KeyInfo {
        let (key, kind) = try OpenSSHImport.parse(openSSH, passphrase: passphrase)
        let material: KeyMaterial
        switch kind {
        case .ed25519:
            let raw = try Curve25519.Signing.PrivateKey(sshEd25519: openSSH, decryptionKey: passphrase.map { Data($0.utf8) })
            material = KeyMaterial(kind: .ed25519, data: raw.rawRepresentation)
        default:
            // Citadel cannot serialise an RSA key back out, so keep the file and its passphrase.
            material = KeyMaterial(kind: kind, data: Data(openSSH.utf8), passphrase: passphrase)
        }
        return try add(material, publicKey: key.publicKey, label: label)
    }

    public func delete(id: String) async throws {
        try indexLock.withLock { _ in
            var index = try readIndex()
            index.removeAll { $0.id == id }
            try secrets.delete(Self.account(id))
            try writeIndex(index)
        }
    }

    /// Loads a key for signing with an already evaluated context, so the handshake that
    /// signs with it later never needs to show a prompt.
    func privateKey(id: String, context: LAContext) throws -> NIOSSHPrivateKey {
        guard let data = try secrets.get(Self.account(id), context: context) else { throw KeyError.missing }
        guard let material = try? JSONDecoder().decode(KeyMaterial.self, from: data) else { throw KeyError.malformed }
        switch material.kind {
        case .secureEnclaveP256:
            let key = try SecureEnclave.P256.Signing.PrivateKey(
                dataRepresentation: material.data, authenticationContext: context
            )
            return NIOSSHPrivateKey(secureEnclaveP256Key: key)
        case .ed25519:
            return NIOSSHPrivateKey(ed25519Key: try Curve25519.Signing.PrivateKey(rawRepresentation: material.data))
        case .rsa, .ecdsaP256:
            return try OpenSSHImport.parse(String(decoding: material.data, as: UTF8.self), passphrase: material.passphrase).0
        }
    }

    /// Reads only the index, which needs no unlock.
    public func info(id: String) throws -> KeyInfo? {
        try readIndex().first { $0.id == id }
    }

    private func add(_ material: KeyMaterial, publicKey: NIOSSHPublicKey, label: String) throws -> KeyInfo {
        let info = KeyInfo(
            id: UUID().uuidString,
            label: label,
            kind: material.kind,
            publicKeyLine: PublicKeyLine.line(for: publicKey, comment: PublicKeyLine.deviceComment(for: publicKey)),
            fingerprint: PublicKeyLine.fingerprint(for: publicKey),
            createdAt: Date()
        )
        try indexLock.withLock { _ in
            try secrets.set(
                try JSONEncoder().encode(material),
                for: Self.account(info.id),
                requireUserPresence: material.kind != .secureEnclaveP256
            )
            try writeIndex(try readIndex() + [info])
        }
        return info
    }

    private func readIndex() throws -> [KeyInfo] {
        guard let data = try secrets.get(Self.indexAccount, context: nil) else { return [] }
        return try JSONDecoder().decode([KeyInfo].self, from: data)
    }

    private func writeIndex(_ index: [KeyInfo]) throws {
        try secrets.set(try JSONEncoder().encode(index), for: Self.indexAccount, requireUserPresence: false)
    }

    private static func account(_ id: String) -> String { "key:\(id)" }
}

/// Private key bytes as stored. Never logged: the description hides everything but the kind.
struct KeyMaterial: Codable, CustomStringConvertible {
    var kind: KeyKind
    var data: Data
    var passphrase: String?

    init(kind: KeyKind, data: Data, passphrase: String? = nil) {
        self.kind = kind
        self.data = data
        self.passphrase = passphrase
    }

    var description: String { "KeyMaterial(\(kind.rawValue), <redacted>)" }
}
