import CryptoKit
import Foundation

/// HKDF labels. A push envelope can never be opened as an answer or the other way round.
public enum Labels {
    public static let push = "push"
    public static let answer = "answer"
}

public enum SealError: Error, Equatable, Sendable {
    /// The recipient key is low order or otherwise unusable.
    case badKey
    case badLabel
    /// Every `open` failure. Callers learn that an envelope was refused, never why.
    case rejected
}

/// Sealed envelopes shared with the helper: X25519, HKDF-SHA256 and ChaCha20-Poly1305.
///
/// Envelope: `0x01 || ephemeral public key || nonce || ciphertext || tag`, base64url without
/// padding. The HKDF salt is `ephemeral public key || recipient public key` and the info is
/// `"togo/v1/" + label`. The layout is frozen by `protocol/fixtures/crypto/seal.json`.
public enum Seal {
    static let version: UInt8 = 0x01
    static let keySize = 32
    static let nonceSize = 12
    static let tagSize = 16
    static let headerSize = 1 + keySize + nonceSize

    /// Seals with a fresh ephemeral key and nonce, so no AEAD key is ever used twice.
    public static func seal(
        to recipient: Curve25519.KeyAgreement.PublicKey, _ plaintext: Data, label: String, aad: Data
    ) throws -> String {
        // SystemRandomNumberGenerator is the platform CSPRNG.
        let nonce = Data((0..<nonceSize).map { _ in UInt8.random(in: .min ... .max) })
        return try seal(ephemeral: Curve25519.KeyAgreement.PrivateKey(), nonce: nonce, to: recipient, plaintext,
                        label: label, aad: aad)
    }

    /// `seal` with the randomness supplied, for reproducing the published vectors only.
    static func seal(
        ephemeral: Curve25519.KeyAgreement.PrivateKey, nonce: Data, to recipient: Curve25519.KeyAgreement.PublicKey,
        _ plaintext: Data, label: String, aad: Data
    ) throws -> String {
        guard !label.isEmpty else { throw SealError.badLabel }
        guard nonce.count == nonceSize else { throw SealError.badKey }
        let ephemeralPublic = ephemeral.publicKey.rawRepresentation
        guard let key = messageKey(
            self: ephemeral, peer: recipient, ephemeralPublic: ephemeralPublic,
            recipientPublic: recipient.rawRepresentation, label: label
        ) else { throw SealError.badKey }
        let box = try ChaChaPoly.seal(plaintext, using: key, nonce: ChaChaPoly.Nonce(data: nonce), authenticating: aad)
        var out = Data([version])
        out.append(ephemeralPublic)
        out.append(nonce)
        out.append(box.ciphertext)
        out.append(box.tag)
        return Base64URL.encode(out)
    }

    /// Opens an envelope addressed to `key`. Throws `SealError.rejected` for every failure:
    /// bad encoding, wrong version, short input, low-order ephemeral key, wrong label or aad,
    /// and tampered bytes.
    public static func open(
        _ envelope: String, with key: Curve25519.KeyAgreement.PrivateKey, label: String, aad: Data
    ) throws -> Data {
        guard !label.isEmpty, let raw = Base64URL.decode(envelope),
              raw.count >= headerSize + tagSize, raw.first == version
        else { throw SealError.rejected }
        let bytes = [UInt8](raw)
        let ephemeralPublic = Data(bytes[1..<1 + keySize])
        let nonce = Data(bytes[1 + keySize..<headerSize])
        let ciphertext = Data(bytes[headerSize..<bytes.count - tagSize])
        let tag = Data(bytes[(bytes.count - tagSize)...])
        guard let peer = try? Curve25519.KeyAgreement.PublicKey(rawRepresentation: ephemeralPublic),
              let messageKey = messageKey(
                  self: key, peer: peer, ephemeralPublic: ephemeralPublic,
                  recipientPublic: key.publicKey.rawRepresentation, label: label
              ),
              let chachaNonce = try? ChaChaPoly.Nonce(data: nonce),
              let box = try? ChaChaPoly.SealedBox(nonce: chachaNonce, ciphertext: ciphertext, tag: tag),
              let plaintext = try? ChaChaPoly.open(box, using: messageKey, authenticating: aad)
        else { throw SealError.rejected }
        return plaintext
    }

    /// X25519 then HKDF-SHA256. Returns nil for the all-zero shared secret a low-order peer
    /// point produces.
    private static func messageKey(
        self: Curve25519.KeyAgreement.PrivateKey, peer: Curve25519.KeyAgreement.PublicKey,
        ephemeralPublic: Data, recipientPublic: Data, label: String
    ) -> SymmetricKey? {
        guard let shared = try? self.sharedSecretFromKeyAgreement(with: peer) else { return nil }
        let isZero = shared.withUnsafeBytes { bytes in bytes.reduce(UInt8(0)) { $0 | $1 } == 0 }
        guard !isZero else { return nil }
        let ikm = shared.withUnsafeBytes { SymmetricKey(data: Data($0)) }
        return HKDF<SHA256>.deriveKey(
            inputKeyMaterial: ikm, salt: ephemeralPublic + recipientPublic, info: Data(("togo/v1/" + label).utf8),
            outputByteCount: keySize
        )
    }
}

/// base64url without padding. Decoding is strict, like Go's `RawURLEncoding.Strict()`: no
/// padding, no standard-alphabet characters and no stray bits in the last character.
public enum Base64URL {
    public static func encode(_ data: Data) -> String {
        data.base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }

    public static func decode(_ string: String) -> Data? {
        guard string.utf8.count % 4 != 1,
              string.utf8.allSatisfy({ isAlphabet($0) })
        else { return nil }
        var standard = string.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        standard += String(repeating: "=", count: (4 - standard.utf8.count % 4) % 4)
        guard let data = Data(base64Encoded: standard), encode(data) == string else { return nil }
        return data
    }

    private static func isAlphabet(_ c: UInt8) -> Bool {
        switch c {
        case UInt8(ascii: "A")...UInt8(ascii: "Z"), UInt8(ascii: "a")...UInt8(ascii: "z"),
             UInt8(ascii: "0")...UInt8(ascii: "9"), UInt8(ascii: "-"), UInt8(ascii: "_"):
            true
        default:
            false
        }
    }
}
