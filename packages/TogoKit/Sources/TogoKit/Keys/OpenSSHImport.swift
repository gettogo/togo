@preconcurrency import Citadel
import Crypto
import Foundation
@preconcurrency import NIOSSH

public enum KeyError: Error, Equatable, LocalizedError {
    case badPassphrase
    case unsupportedKeyType(String)
    case malformed
    /// The key a machine record names is not on this device, as when the record synced
    /// from another device: private keys never leave the device that made or imported them.
    case missing

    public var errorDescription: String? {
        switch self {
        case .badPassphrase: "The passphrase is wrong."
        case .unsupportedKeyType(let type): "Keys of type \(type) are not supported."
        case .malformed: "This is not an OpenSSH private key."
        case .missing: "This key is not on this device."
        }
    }
}

/// Reads an `openssh-key-v1` private key file far enough to learn its type and whether it
/// is encrypted, then hands decryption to Citadel.
enum OpenSSHImport {
    struct Header: Equatable {
        var keyType: String
        var encrypted: Bool
    }

    static func header(of text: String) throws(KeyError) -> Header {
        let body = text
            .replacingOccurrences(of: "-----BEGIN OPENSSH PRIVATE KEY-----", with: "")
            .replacingOccurrences(of: "-----END OPENSSH PRIVATE KEY-----", with: "")
            .filter { !$0.isWhitespace }
        guard text.contains("BEGIN OPENSSH PRIVATE KEY"), let data = Data(base64Encoded: body) else {
            throw .malformed
        }
        var reader = SSHWireReader(data)
        let magic = Data("openssh-key-v1\0".utf8)
        guard data.starts(with: magic) else { throw .malformed }
        reader.skip(magic.count)
        guard let cipher = reader.string(), reader.string() != nil, reader.bytes() != nil,
              let count = reader.uint32(), count == 1,
              let publicBlob = reader.bytes() else { throw .malformed }
        var publicReader = SSHWireReader(publicBlob)
        guard let type = publicReader.string() else { throw .malformed }
        return Header(keyType: type, encrypted: cipher != "none")
    }

    /// The parsed key ready to sign, plus its kind for the key list.
    static func parse(_ text: String, passphrase: String?) throws(KeyError) -> (NIOSSHPrivateKey, KeyKind) {
        let header = try header(of: text)
        let decryptionKey = header.encrypted ? passphrase.map { Data($0.utf8) } : nil
        if header.encrypted, decryptionKey == nil { throw .badPassphrase }
        do {
            switch header.keyType {
            case "ssh-ed25519":
                let key = try Curve25519.Signing.PrivateKey(sshEd25519: text, decryptionKey: decryptionKey)
                return (NIOSSHPrivateKey(ed25519Key: key), .ed25519)
            case "ssh-rsa":
                let key = try Insecure.RSA.PrivateKey(sshRsa: text, decryptionKey: decryptionKey)
                return (NIOSSHPrivateKey(custom: key), .rsa)
            default:
                // Citadel 0.12.1 parses only ed25519 and RSA OpenSSH files.
                throw KeyError.unsupportedKeyType(header.keyType)
            }
        } catch let error as KeyError {
            throw error
        } catch {
            // A wrong passphrase decrypts to garbage, which fails the check bytes or layout.
            throw header.encrypted ? .badPassphrase : .malformed
        }
    }
}

/// RFC 4251 wire primitives: big-endian uint32 and length-prefixed strings.
struct SSHWireReader {
    private let data: Data
    private var offset: Int

    init(_ data: Data) {
        self.data = Data(data)
        offset = 0
    }

    mutating func skip(_ count: Int) { offset += count }

    mutating func uint32() -> UInt32? {
        guard offset + 4 <= data.count else { return nil }
        let value = data[offset..<offset + 4].reduce(UInt32(0)) { $0 << 8 | UInt32($1) }
        offset += 4
        return value
    }

    mutating func bytes() -> Data? {
        guard let length = uint32().map(Int.init), offset + length <= data.count else { return nil }
        defer { offset += length }
        return data[offset..<offset + length]
    }

    mutating func string() -> String? {
        bytes().flatMap { String(data: $0, encoding: .utf8) }
    }
}
