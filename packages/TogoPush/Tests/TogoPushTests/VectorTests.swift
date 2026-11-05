import CryptoKit
import Foundation
import Testing
@testable import TogoPush

/// Checks CryptoKit against the vectors the helper's Go code publishes, byte for byte.
struct VectorTests {
    struct SealVector: Decodable {
        var name, recipientPriv, recipientPub, ephPriv, ephPub, nonce, label, aad, plaintext: String
        var envelope: String
    }

    struct SealNegative: Decodable {
        var name, recipientPriv, label, aad, envelope, expect: String
    }

    struct SealFile: Decodable {
        var vectors: [SealVector]
        var negative: [SealNegative]
    }

    struct SignVector: Decodable {
        var name, seed, publicKey, hostId, requestId, decision, message, signature, expect: String
        var ts: Int64
    }

    struct SignFile: Decodable { var vectors: [SignVector] }

    struct HostIDVector: Decodable { var name, mailboxSecret, hostId: String }

    struct HostKeyVector: Decodable { var name, x25519Priv, x25519Pub, x25519PubBase64url: String }

    struct HostIDFile: Decodable {
        var hostIds: [HostIDVector]
        var x25519Keys: [HostKeyVector]
    }

    /// Tests/TogoPushTests/VectorTests.swift sits four directories below the repo root.
    static let directory: URL = {
        var url = URL(filePath: #filePath)
        for _ in 0..<5 { url.deleteLastPathComponent() }
        return url.appending(path: "protocol/fixtures/crypto", directoryHint: .isDirectory)
    }()

    static func load<T: Decodable>(_ name: String, as: T.Type) throws -> T {
        try JSONDecoder().decode(T.self, from: Data(contentsOf: directory.appending(path: name)))
    }

    @Test func sealMatchesEveryVector() throws {
        let file = try Self.load("seal.json", as: SealFile.self)
        #expect(file.vectors.count >= 3)
        for v in file.vectors {
            let eph = try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: hex(v.ephPriv))
            let recipient = try Curve25519.KeyAgreement.PublicKey(rawRepresentation: hex(v.recipientPub))
            #expect(eph.publicKey.rawRepresentation == hex(v.ephPub), "\(v.name)")
            let envelope = try Seal.seal(
                ephemeral: eph, nonce: hex(v.nonce), to: recipient, Data(v.plaintext.utf8),
                label: v.label, aad: Data(v.aad.utf8)
            )
            #expect(envelope == v.envelope, "\(v.name)")
        }
    }

    @Test func openRecoversPlaintext() throws {
        let file = try Self.load("seal.json", as: SealFile.self)
        for v in file.vectors {
            let key = try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: hex(v.recipientPriv))
            let plaintext = try Seal.open(v.envelope, with: key, label: v.label, aad: Data(v.aad.utf8))
            #expect(plaintext == Data(v.plaintext.utf8), "\(v.name)")
        }
        let key = Curve25519.KeyAgreement.PrivateKey()
        let envelope = try Seal.seal(to: key.publicKey, Data("hello".utf8), label: Labels.push, aad: Data("a".utf8))
        #expect(try Seal.open(envelope, with: key, label: Labels.push, aad: Data("a".utf8)) == Data("hello".utf8))
        #expect(throws: SealError.rejected) {
            try Seal.open(envelope, with: key, label: Labels.answer, aad: Data("a".utf8))
        }
    }

    @Test func negativeVectorsFail() throws {
        let file = try Self.load("seal.json", as: SealFile.self)
        #expect(file.negative.count >= 10)
        for v in file.negative {
            #expect(v.expect == "fail")
            let key = try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: hex(v.recipientPriv))
            #expect(throws: SealError.rejected, "\(v.name)") {
                try Seal.open(v.envelope, with: key, label: v.label, aad: Data(v.aad.utf8))
            }
        }
    }

    @Test func sealRefusesLowOrderRecipientAndEmptyLabel() throws {
        let zero = try Curve25519.KeyAgreement.PublicKey(rawRepresentation: Data(count: 32))
        #expect(throws: SealError.badKey) { try Seal.seal(to: zero, Data(), label: Labels.push, aad: Data()) }
        let key = Curve25519.KeyAgreement.PrivateKey()
        #expect(throws: SealError.badLabel) { try Seal.seal(to: key.publicKey, Data(), label: "", aad: Data()) }
    }

    /// CryptoKit adds randomness to Ed25519 signatures, so the check is that every `ok`
    /// signature verifies over the exact message bytes and every `fail` one does not.
    @Test func signMatchesVectors() throws {
        let file = try Self.load("sign.json", as: SignFile.self)
        for v in file.vectors {
            let key = try Curve25519.Signing.PrivateKey(rawRepresentation: hex(v.seed))
            let message = Signing.approvalBytes(hostID: v.hostId, requestID: v.requestId, decision: v.decision, ts: v.ts)
            #expect(message == Data(v.message.utf8), "\(v.name)")
            let pub = try Curve25519.Signing.PublicKey(rawRepresentation: Base64URL.decode(v.publicKey) ?? Data())
            let sig = Base64URL.decode(v.signature) ?? Data()
            let valid = pub.isValidSignature(sig, for: message)
            #expect(valid == (v.expect == "ok"), "\(v.name)")
            if v.expect == "ok" {
                #expect(Base64URL.encode(key.publicKey.rawRepresentation) == v.publicKey, "\(v.name)")
                #expect(pub.isValidSignature(try key.signature(for: message), for: message))
            }
        }
    }

    @Test func hostIDMatchesVector() throws {
        let file = try Self.load("hostid.json", as: HostIDFile.self)
        for v in file.hostIds {
            #expect(HostID.derive(secret: hex(v.mailboxSecret)) == v.hostId, "\(v.name)")
        }
        for k in file.x25519Keys {
            let key = try Curve25519.KeyAgreement.PrivateKey(rawRepresentation: hex(k.x25519Priv))
            #expect(key.publicKey.rawRepresentation == hex(k.x25519Pub), "\(k.name)")
            #expect(Base64URL.encode(key.publicKey.rawRepresentation) == k.x25519PubBase64url, "\(k.name)")
        }
    }

    @Test func registerBytesMatchGo() {
        let host = "h_mmg42klgyqzwneiskrelxms3j7", dev = "dev_test-1", ts: Int64 = 1_793_707_200_000
        let apns = String(repeating: "ab", count: 32)
        let live = String(repeating: "cd", count: 32)
        let start = String(repeating: "ef", count: 32)
        let all = Signing.registerBytes(
            hostID: host, deviceID: dev, apnsToken: apns, liveActivityToken: live, liveActivityStartToken: start,
            liveActivityCommand: true, ts: ts
        )
        let allWant = "togo-register/v2\nh_mmg42klgyqzwneiskrelxms3j7\ndev_test-1\n" + apns + "\n" + live + "\n" + start
            + "\ntrue\n1793707200000"
        #expect(all == Data(allWant.utf8))
        let one = Signing.registerBytes(
            hostID: host, deviceID: dev, apnsToken: nil, liveActivityToken: live, liveActivityStartToken: nil,
            liveActivityCommand: false, ts: ts
        )
        let oneWant = "togo-register/v2\nh_mmg42klgyqzwneiskrelxms3j7\ndev_test-1\n\n" + live + "\n\nfalse\n1793707200000"
        #expect(one == Data(oneWant.utf8))
    }

    /// The protocol's register fixture is signed by the Go side; the same bytes must verify here.
    @Test func registerFixtureVerifies() throws {
        struct File: Decodable { var plaintext: RegisterPlaintext }
        var url = Self.directory
        url.deleteLastPathComponent()
        url.deleteLastPathComponent()
        url.append(path: "v1/fixtures/plaintext/register.json")
        let p = try JSONDecoder().decode(File.self, from: Data(contentsOf: url)).plaintext
        let key = try Curve25519.Signing.PrivateKey(rawRepresentation: Data(repeating: 0x11, count: 32)).publicKey
        let message = Signing.registerBytes(
            hostID: "h_mmg42klgyqzwneiskrelxms3j7", deviceID: p.deviceId, apnsToken: p.apnsToken,
            liveActivityToken: p.liveActivityToken, liveActivityStartToken: p.liveActivityStartToken,
            liveActivityCommand: p.liveActivityCommand, ts: p.ts
        )
        #expect(key.isValidSignature(try #require(Base64URL.decode(p.sig)), for: message))
    }

    @Test func base64URLIsStrict() {
        #expect(Base64URL.decode("AQ") == Data([1]))
        #expect(Base64URL.decode("AQ==") == nil)
        #expect(Base64URL.decode("A+") == nil)
        #expect(Base64URL.decode("AR") == nil, "non-zero trailing bits")
        #expect(Base64URL.decode("A") == nil)
        #expect(Base64URL.decode("") == Data())
        #expect(Base64URL.encode(Data([0xfb, 0xff])) == "-_8")
    }
}

func hex(_ s: String) -> Data {
    var out = Data(capacity: s.count / 2)
    var index = s.startIndex
    while index < s.endIndex {
        let next = s.index(index, offsetBy: 2)
        out.append(UInt8(s[index..<next], radix: 16)!)
        index = next
    }
    return out
}
