import CryptoKit
import Foundation
import Synchronization
import Testing
@testable import TogoPush

@Suite(.serialized)
struct StoresTests {
    static let now = Date(timeIntervalSince1970: 1_793_707_200)
    static let hostID = "h_mmg42klgyqzwneiskrelxms3j7"

    func directory() -> URL {
        FileManager.default.temporaryDirectory.appending(path: "togo-push-tests-\(UUID().uuidString)")
    }

    func pairing(hostKey: Curve25519.KeyAgreement.PublicKey) -> Pairing {
        Pairing(
            hostId: Self.hostID, hostKey: Base64URL.encode(hostKey.rawRepresentation),
            relayURL: URL(string: "https://relay.example")!, machineName: "studio", appHostID: nil,
            pairedAt: Self.now, tokenHash: "a1b2c3", liveActivityCommand: false
        )
    }

    func request(_ id: String, receivedAt: Date, options: [PushOption] = []) -> PendingRequest {
        PendingRequest(
            requestId: id, hostId: Self.hostID, sessionId: "s_abcdefghij", bib: "01", agent: "claude", host: nil,
            title: "Run npm test?", subject: PushSubject(kind: "command", text: "npm test"), options: options,
            receivedAt: receivedAt, answered: nil
        )
    }

    @Test func pairingRoundTrip() {
        let dir = directory()
        let store = PairingStore(directory: dir)
        let p = pairing(hostKey: Curve25519.KeyAgreement.PrivateKey().publicKey)
        store.put(p)
        #expect(PairingStore(directory: dir).all() == [p])
        #expect(store.pairing(hostId: Self.hostID) == p)
        var renamed = p
        renamed.machineName = "laptop"
        store.put(renamed)
        #expect(store.all() == [renamed])
        store.remove(hostId: Self.hostID)
        #expect(store.all().isEmpty)
        #expect(store.pairing(hostId: Self.hostID) == nil)
    }

    @Test func pendingPrunesOld() {
        let clock = Mutex(Self.now)
        let store = PendingStore(directory: directory(), clock: { clock.withLock { $0 } })
        store.upsert(request("r_aaaaaaaaaa", receivedAt: Self.now.addingTimeInterval(-601)))
        store.upsert(request("r_bbbbbbbbbb", receivedAt: Self.now.addingTimeInterval(-30)))
        store.mark("r_bbbbbbbbbb", .sending)
        #expect(store.all().count == 2)
        store.prune()
        #expect(store.all().map(\.requestId) == ["r_bbbbbbbbbb"])
        #expect(store.all().first?.answered == .sending)
        clock.withLock { $0 = Self.now.addingTimeInterval(600) }
        store.prune()
        #expect(store.all().isEmpty)
        store.upsert(request("r_cccccccccc", receivedAt: Self.now))
        store.remove("r_cccccccccc")
        #expect(store.all().isEmpty)
    }

    @Test func upsertKeepsAnsweredState() {
        let store = PendingStore(directory: directory(), clock: { Self.now })
        store.upsert(request("r_aaaaaaaaaa", receivedAt: Self.now.addingTimeInterval(-100)))
        store.mark("r_aaaaaaaaaa", .approved)
        store.upsert(request("r_aaaaaaaaaa", receivedAt: Self.now))
        let kept = store.request("r_aaaaaaaaaa")
        #expect(kept?.answered == .approved, "a replayed push does not reopen an answered request")
        #expect(kept?.receivedAt == Self.now.addingTimeInterval(-100), "nor makes it look new")
    }

    @Test func pendingReportsEveryWrite() {
        let writes = Mutex(0)
        let store = PendingStore(directory: directory(), clock: { Self.now }, onChange: { writes.withLock { $0 += 1 } })
        store.upsert(request("r_aaaaaaaaaa", receivedAt: Self.now))
        _ = store.claim("r_aaaaaaaaaa", hostId: Self.hostID)
        store.release("r_aaaaaaaaaa")
        store.mark("r_aaaaaaaaaa", .approved)
        store.prune()
        store.remove("r_aaaaaaaaaa")
        _ = store.all()
        #expect(writes.withLock { $0 } == 6)
    }

    /// Two stores on one directory stand in for two processes answering the same request.
    @Test func claimIsSingleUse() {
        let dir = directory()
        let a = PendingStore(directory: dir, clock: { Self.now })
        let b = PendingStore(directory: dir, clock: { Self.now })
        a.upsert(request("r_aaaaaaaaaa", receivedAt: Self.now))
        #expect(a.claim("r_aaaaaaaaaa", hostId: Self.hostID))
        #expect(!b.claim("r_aaaaaaaaaa", hostId: Self.hostID))
        #expect(b.request("r_aaaaaaaaaa")?.answered == .sending)
        a.release("r_aaaaaaaaaa")
        #expect(b.request("r_aaaaaaaaaa")?.answered == nil, "a failed send can be retried")
        #expect(b.claim("r_aaaaaaaaaa", hostId: Self.hostID))
        b.mark("r_aaaaaaaaaa", .approved)
        a.release("r_aaaaaaaaaa")
        #expect(!a.claim("r_aaaaaaaaaa", hostId: Self.hostID), "release only undoes a send in flight")
        #expect(a.claim("r_unknownxyz", hostId: Self.hostID), "an id the store never saw is claimed once")
        #expect(!b.claim("r_unknownxyz", hostId: Self.hostID))
    }

    /// A process stopped mid-send leaves its claim behind: past `claimTimeout` it no longer
    /// holds the request on "Sending", another process may answer, and launch gives it back.
    @Test func abandonedClaimIsTakenOverAndGivenBack() {
        let dir = directory()
        let clock = Mutex(Self.now)
        let a = PendingStore(directory: dir, clock: { clock.withLock { $0 } })
        let b = PendingStore(directory: dir, clock: { clock.withLock { $0 } })
        a.upsert(request("r_aaaaaaaaaa", receivedAt: Self.now))
        #expect(a.claim("r_aaaaaaaaaa", hostId: Self.hostID))
        #expect(b.request("r_aaaaaaaaaa")?.claimedAt == Self.now)
        clock.withLock { $0 = Self.now.addingTimeInterval(PendingRequest.claimTimeout - 1) }
        #expect(!b.claim("r_aaaaaaaaaa", hostId: Self.hostID), "a claim inside its time still holds")
        #expect(RequestLifetime.waiting(b.all(), now: clock.withLock { $0 }).isEmpty)
        clock.withLock { $0 = Self.now.addingTimeInterval(PendingRequest.claimTimeout) }
        #expect(RequestLifetime.waiting(b.all(), now: clock.withLock { $0 }).map(\.requestId) == ["r_aaaaaaaaaa"])
        #expect(b.claim("r_aaaaaaaaaa", hostId: Self.hostID), "an abandoned claim is taken over")
        b.mark("r_aaaaaaaaaa", .approved)
        #expect(b.request("r_aaaaaaaaaa")?.claimedAt == nil)

        // Given back at launch: a real request opens again, a placeholder goes.
        a.upsert(request("r_bbbbbbbbbb", receivedAt: Self.now))
        #expect(a.claim("r_bbbbbbbbbb", hostId: Self.hostID))
        #expect(a.claim("r_placehold", hostId: Self.hostID))
        a.releaseAbandonedClaims()
        #expect(a.request("r_bbbbbbbbbb")?.answered == .sending, "a fresh claim is left alone")
        clock.withLock { $0 = $0.addingTimeInterval(PendingRequest.claimTimeout) }
        a.releaseAbandonedClaims()
        #expect(a.request("r_bbbbbbbbbb")?.answered == nil)
        #expect(a.request("r_bbbbbbbbbb")?.claimedAt == nil)
        #expect(a.request("r_placehold") == nil)
        #expect(a.request("r_aaaaaaaaaa")?.answered == .approved)
    }

    /// Records written before claims had a time still decode; one left sending is abandoned.
    @Test func olderRecordWithoutClaimTimeDecodes() throws {
        let json = #"{"requestId":"r_aaaaaaaaaa","hostId":"h","title":"Run?","options":[],"receivedAt":0,"answered":"sending"}"#
        let old = try JSONDecoder().decode(PendingRequest.self, from: Data(json.utf8))
        #expect(old.claimedAt == nil)
        #expect(old.claimAbandoned(at: Self.now))
        #expect(old.isOpen(at: Self.now))
        var answered = old
        answered.answered = .approved
        #expect(!answered.isOpen(at: Self.now))
    }

    @Test func pairingRoundTripOnDisk() {
        let dir = directory()
        let p = pairing(hostKey: Curve25519.KeyAgreement.PrivateKey().publicKey)
        PairingStore(directory: dir).put(p)
        #expect(PairingStore(directory: dir).all() == [p])
        PairingStore(directory: dir).remove(hostId: Self.hostID)
        #expect(PairingStore(directory: dir).all().isEmpty)
    }

    /// The app, the notification service and the action handler each open their own store on
    /// the shared container. Two instances stand in for two processes: each has its own
    /// in-process lock, so only the file lock keeps their read-modify-write cycles apart.
    @Test func interleavedWritersKeepEveryWrite() {
        let dir = directory()
        let stores = [PendingStore(directory: dir, clock: { Self.now }), PendingStore(directory: dir, clock: { Self.now })]
        DispatchQueue.concurrentPerform(iterations: 120) { i in
            let store = stores[i % 2]
            let id = "r_" + String(repeating: "a", count: 7) + String(format: "%03d", i)
            store.upsert(request(id, receivedAt: Self.now))
            store.mark(id, i % 3 == 0 ? .sending : .approved)
        }
        let all = PendingStore(directory: dir, clock: { Self.now }).all()
        #expect(all.count == 120)
        #expect(all.allSatisfy { $0.answered != nil })

        let pairings = [PairingStore(directory: dir), PairingStore(directory: dir)]
        DispatchQueue.concurrentPerform(iterations: 40) { i in
            var p = pairing(hostKey: Curve25519.KeyAgreement.PrivateKey().publicKey)
            p.hostId = "h_" + String(repeating: "b", count: 23) + String(format: "%03d", i)
            pairings[i % 2].put(p)
        }
        #expect(PairingStore(directory: dir).all().count == 40)
    }

    @Test func answerSenderRefusesText() async throws {
        let host = Curve25519.KeyAgreement.PrivateKey()
        StubProtocol.reset(status: 202)
        let sender = RelayAnswerSender(keys: InMemoryDeviceKeys(), session: StubProtocol.session(), clock: { Self.now })
        await #expect(throws: AnswerError.notAllowed("text:yes")) {
            try await sender.send(requestId: "r_abcdefghij", decision: "text:yes", pairing: pairing(hostKey: host.publicKey))
        }
        await #expect(throws: AnswerError.notAllowed("maybe")) {
            try await sender.send(requestId: "r_abcdefghij", decision: "maybe", pairing: pairing(hostKey: host.publicKey))
        }
        #expect(StubProtocol.requests().isEmpty)
    }

    @Test func answerSenderRefusesApproveAlways() async throws {
        let host = Curve25519.KeyAgreement.PrivateKey()
        let pending = PendingStore(directory: directory(), clock: { Self.now })
        pending.upsert(request("r_abcdefghij", receivedAt: Self.now, options: [
            PushOption(n: 1, label: "Yes", role: "approve"),
            PushOption(n: 2, label: "Yes, and don't ask again", role: "approve_always"),
            PushOption(n: 3, label: "No", role: "deny"),
        ]))
        StubProtocol.reset(status: 202)
        let sender = RelayAnswerSender(
            keys: InMemoryDeviceKeys(), session: StubProtocol.session(), pending: pending, clock: { Self.now }
        )
        let p = pairing(hostKey: host.publicKey)
        await #expect(throws: AnswerError.notAllowed("option:2")) {
            try await sender.send(requestId: "r_abcdefghij", decision: "option:2", pairing: p)
        }
        await #expect(throws: AnswerError.notAllowed("option:9")) {
            try await sender.send(requestId: "r_abcdefghij", decision: "option:9", pairing: p)
        }
        #expect(StubProtocol.requests().isEmpty)
        try await sender.send(requestId: "r_abcdefghij", decision: "option:3", pairing: p)
        try await sender.send(requestId: "r_abcdefghij", decision: "approve", pairing: p)
        try await sender.send(requestId: "r_abcdefghij", decision: "deny", pairing: p)
        #expect(StubProtocol.requests().count == 3)
    }

    @Test func answerSenderPostsSealedSigned() async throws {
        let host = Curve25519.KeyAgreement.PrivateKey()
        let keys = InMemoryDeviceKeys()
        StubProtocol.reset(status: 202)
        let sender = RelayAnswerSender(keys: keys, session: StubProtocol.session(), clock: { Self.now })
        try await sender.send(requestId: "r_abcdefghij", decision: "approve", pairing: pairing(hostKey: host.publicKey))

        let sent = try #require(StubProtocol.requests().first)
        #expect(sent.method == "POST")
        #expect(sent.url == URL(string: "https://relay.example/v1/mailbox/\(Self.hostID)"))
        let body = String(decoding: sent.body, as: UTF8.self)
        #expect(!body.contains("approve") && !body.contains("r_abcdefghij"))
        let outer = try JSONDecoder().decode([String: String].self, from: sent.body)
        #expect(outer.keys.sorted() == ["ciphertext"])

        let plaintext = try Seal.open(
            try #require(outer["ciphertext"]), with: host, label: Labels.answer, aad: Data(Self.hostID.utf8)
        )
        let fields = try #require(try JSONSerialization.jsonObject(with: plaintext) as? [String: Any])
        #expect(fields["type"] == nil, "answers never carry a type")
        let answer = try JSONDecoder().decode(AnswerPlaintext.self, from: plaintext)
        #expect(answer.requestId == "r_abcdefghij" && answer.decision == "approve")
        #expect(answer.ts == 1_793_707_200_000)
        #expect(try answer.deviceId == keys.deviceID())
        let message = Signing.approvalBytes(hostID: Self.hostID, requestID: answer.requestId, decision: "approve", ts: answer.ts)
        let sig = try #require(Base64URL.decode(answer.sig))
        #expect(try keys.signingKey().publicKey.isValidSignature(sig, for: message))
    }

    @Test func answerSenderMapsRelayFailures() async throws {
        let host = Curve25519.KeyAgreement.PrivateKey()
        let sender = RelayAnswerSender(keys: InMemoryDeviceKeys(), session: StubProtocol.session(), clock: { Self.now })
        StubProtocol.reset(status: 507)
        await #expect(throws: AnswerError.relay(507)) {
            try await sender.send(requestId: "r_abcdefghij", decision: "deny", pairing: pairing(hostKey: host.publicKey))
        }
        StubProtocol.reset(status: nil)
        await #expect(throws: AnswerError.offline) {
            try await sender.send(requestId: "r_abcdefghij", decision: "deny", pairing: pairing(hostKey: host.publicKey))
        }
    }

    @Test func registerCarriesTypeAndSignature() async throws {
        let host = Curve25519.KeyAgreement.PrivateKey()
        let keys = InMemoryDeviceKeys()
        StubProtocol.reset(status: 202)
        let sender = RelayRegisterSender(keys: keys, session: StubProtocol.session(), clock: { Self.now })
        let live = String(repeating: "cd", count: 32)
        try await sender.send(
            (apns: nil, liveActivity: live, liveActivityStart: nil), liveActivityCommand: true,
            pairing: pairing(hostKey: host.publicKey)
        )

        let sent = try #require(StubProtocol.requests().first)
        let outer = try JSONDecoder().decode([String: String].self, from: sent.body)
        let plaintext = try Seal.open(
            try #require(outer["ciphertext"]), with: host, label: Labels.answer, aad: Data(Self.hostID.utf8)
        )
        let fields = try #require(try JSONSerialization.jsonObject(with: plaintext) as? [String: Any])
        #expect(fields["type"] as? String == "register")
        #expect(fields["apnsToken"] == nil && fields["liveActivityStartToken"] == nil)
        #expect(fields["liveActivityCommand"] as? Bool == true)
        let register = try JSONDecoder().decode(RegisterPlaintext.self, from: plaintext)
        let message = Signing.registerBytes(
            hostID: Self.hostID, deviceID: try keys.deviceID(), apnsToken: nil, liveActivityToken: live,
            liveActivityStartToken: nil, liveActivityCommand: true, ts: register.ts
        )
        #expect(try keys.signingKey().publicKey.isValidSignature(try #require(Base64URL.decode(register.sig)), for: message))
    }

    @Test func inMemoryKeysAreStableUntilReset() throws {
        let keys = InMemoryDeviceKeys()
        let id = try keys.deviceID()
        #expect(id.count == 28 && (id.hasPrefix("d_") || id.hasPrefix("w_")))
        #expect(id.allSatisfy { $0 == "_" || $0.isLowercase || ("2"..."7").contains($0) })
        #expect(try keys.deviceID() == id)
        let agreement = try keys.agreementKey().publicKey.rawRepresentation
        try keys.reset()
        #expect(try keys.deviceID() != id)
        #expect(try keys.agreementKey().publicKey.rawRepresentation != agreement)
    }
}

/// Answers every request with a fixed status, or fails as offline when the status is nil.
final class StubProtocol: URLProtocol, @unchecked Sendable {
    struct Captured: Sendable {
        var method: String?
        var url: URL?
        var body: Data
    }

    private static let state = Mutex<(status: Int?, requests: [Captured])>((202, []))

    static func reset(status: Int?) { state.withLock { $0 = (status, []) } }
    static func requests() -> [Captured] { state.withLock { $0.requests } }

    static func session() -> URLSession {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [StubProtocol.self]
        return URLSession(configuration: config)
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        var body = request.httpBody ?? Data()
        if body.isEmpty, let stream = request.httpBodyStream {
            stream.open()
            var buffer = [UInt8](repeating: 0, count: 4096)
            while stream.hasBytesAvailable {
                let n = stream.read(&buffer, maxLength: buffer.count)
                if n <= 0 { break }
                body.append(buffer, count: n)
            }
            stream.close()
        }
        let captured = Captured(method: request.httpMethod, url: request.url, body: body)
        let status = Self.state.withLock { state -> Int? in
            state.requests.append(captured)
            return state.status
        }
        guard let status, let url = request.url,
              let response = HTTPURLResponse(url: url, statusCode: status, httpVersion: nil, headerFields: nil)
        else {
            client?.urlProtocol(self, didFailWithError: URLError(.notConnectedToInternet))
            return
        }
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data("{}".utf8))
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}
