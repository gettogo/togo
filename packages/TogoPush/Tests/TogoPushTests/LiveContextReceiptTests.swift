import CryptoKit
import Foundation
import Testing
@testable import TogoPush

extension StoresTests {
    @Test func receiptDecodesGoProducerVector() throws {
        let raw = #"""
{
  "description": "Device dev_test-1 confirms saved request r_abcdefghij for host h_mmg42klgyqzwneiskrelxms3j7. Signed by the test Ed25519 seed of 32 bytes 0x11; seal with label answer and aad hostId. This receipt never answers the request.",
  "kind": "live_context",
  "plaintext": {
    "type": "live_context",
    "deviceId": "dev_test-1",
    "requestId": "r_abcdefghij",
    "tokenHash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "ts": 1793707200000,
    "sig": "PjjBM3zi-K1Wg-fZ1PYxAsKL9qNCNyKdiQyyNQms4rHDXx08_ZuGkLE4Ky--KfzbRC0ly1sunTho9QcJnDLoAA"
  }
}
"""#
        let fixture = try JSONDecoder().decode(ReceiptVector.self, from: Data(raw.utf8))
        let receipt = fixture.plaintext
        let key = try Curve25519.Signing.PublicKey(rawRepresentation: #require(Base64URL.decode("0EqyMnQrtKs6E2i9RhXk5tAiSrcaAWuvhSCjMsl3hzc")))
        let bytes = Signing.liveContextBytes(hostID: Self.hostID, deviceID: receipt.deviceId,
                                             requestID: receipt.requestId, tokenHash: receipt.tokenHash, ts: receipt.ts)
        #expect(key.isValidSignature(try #require(Base64URL.decode(receipt.sig)), for: bytes))
    }

    @Test func receiptRequiresSavedMatchingOpenRequestAndSupportingHelper() async throws {
        let dir = directory()
        defer { try? FileManager.default.removeItem(at: dir) }
        let host = Curve25519.KeyAgreement.PrivateKey()
        let pairings = PairingStore(directory: dir)
        let pending = PendingStore(directory: dir, clock: { Self.now })
        var paired = pairing(hostKey: host.publicKey)
        pairings.put(paired)
        let sender = LiveContextReceiptSender(
            keys: InMemoryDeviceKeys(), pairings: pairings, pending: pending,
            session: StubProtocol.session(), clock: { Self.now }
        )
        StubProtocol.reset(status: 202)
        pending.upsert(request("r_abcdefghij", receivedAt: Self.now))
        #expect(try await !sender.send(hostID: Self.hostID, requestID: "r_abcdefghij"))
        paired.liveContextReceipts = true
        paired.apnsTokenHash = String(repeating: "a", count: 64)
        pairings.put(paired)
        #expect(try await !sender.send(hostID: Self.hostID, requestID: "r_bbbbbbbbbb"))
        #expect(try await !sender.send(hostID: "h_other", requestID: "r_abcdefghij"))
        pending.mark("r_abcdefghij", .approved)
        #expect(try await !sender.send(hostID: Self.hostID, requestID: "r_abcdefghij"))
        #expect(StubProtocol.requests().isEmpty)
    }

    @Test func receiptPostsSealedSignedWithoutClaimingAnswer() async throws {
        let dir = directory()
        defer { try? FileManager.default.removeItem(at: dir) }
        let host = Curve25519.KeyAgreement.PrivateKey()
        let keys = InMemoryDeviceKeys()
        let pairings = PairingStore(directory: dir)
        let pending = PendingStore(directory: dir, clock: { Self.now })
        var paired = pairing(hostKey: host.publicKey)
        paired.liveContextReceipts = true
        paired.apnsTokenHash = String(repeating: "a", count: 64)
        pairings.put(paired)
        pending.upsert(request("r_abcdefghij", receivedAt: Self.now))
        StubProtocol.reset(status: 202)
        let sender = LiveContextReceiptSender(
            keys: keys, pairings: pairings, pending: pending, session: StubProtocol.session(), clock: { Self.now }
        )
        #expect(try await sender.send(hostID: Self.hostID, requestID: "r_abcdefghij"))
        let sent = try #require(StubProtocol.requests().first)
        let body = String(decoding: sent.body, as: UTF8.self)
        #expect(!body.contains("r_abcdefghij") && !body.contains("live_context"))
        let outer = try JSONDecoder().decode([String: String].self, from: sent.body)
        let plain = try Seal.open(try #require(outer["ciphertext"]), with: host, label: Labels.answer, aad: Data(Self.hostID.utf8))
        let receipt = try JSONDecoder().decode(LiveContextPlaintext.self, from: plain)
        #expect(receipt.type == "live_context" && receipt.requestId == "r_abcdefghij")
        let deviceID = try keys.deviceID()
        let canonical = Data("togo-live-context/v1\n\(Self.hostID)\n\(deviceID)\nr_abcdefghij\n\(String(repeating: "a", count: 64))\n1793707200000".utf8)
        #expect(try keys.signingKey().publicKey.isValidSignature(try #require(Base64URL.decode(receipt.sig)), for: canonical))
        #expect(pending.request("r_abcdefghij")?.answered == nil)
    }

    @Test func receiptRefusesExpiredAndFutureRecords() async throws {
        for offset in [-600.0, 61.0] {
            let dir = directory()
            defer { try? FileManager.default.removeItem(at: dir) }
            let pairings = PairingStore(directory: dir)
            var paired = pairing(hostKey: Curve25519.KeyAgreement.PrivateKey().publicKey)
            paired.liveContextReceipts = true
        paired.apnsTokenHash = String(repeating: "a", count: 64)
            pairings.put(paired)
            let pending = PendingStore(directory: dir, clock: { Self.now })
            pending.upsert(request("r_abcdefghij", receivedAt: Self.now.addingTimeInterval(offset)))
            StubProtocol.reset(status: 202)
            let sender = LiveContextReceiptSender(keys: InMemoryDeviceKeys(), pairings: pairings, pending: pending,
                                                  session: StubProtocol.session(), clock: { Self.now })
            #expect(try await !sender.send(hostID: Self.hostID, requestID: "r_abcdefghij"))
            #expect(StubProtocol.requests().isEmpty)
        }
    }
}

private struct ReceiptVector: Decodable { var plaintext: LiveContextPlaintext }
