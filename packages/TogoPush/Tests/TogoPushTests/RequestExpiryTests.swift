import Foundation
import Testing
@testable import TogoPush

struct RequestExpiryTests {
    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    @Test func requestExpirySurvivesThePushAndLiveContextDecoders() throws {
        let payload = Data("""
        {"v":1,"kind":"needs_you","title":"Check?","body":"","ts":1800000000000,"expires":1800000900}
        """.utf8)
        let decoded = try JSONDecoder().decode(PushPayload.self, from: payload)
        let roundTrip = try #require(JSONSerialization.jsonObject(with: JSONEncoder().encode(decoded)) as? [String: Any])
        #expect(roundTrip["expires"] as? Int64 == 1_800_000_900)
        let context = try JSONDecoder().decode(LiveContext.self, from: Data("{\"answer\":\"approve_deny\",\"expires\":1800000900}".utf8))
        let contextTrip = try #require(JSONSerialization.jsonObject(with: JSONEncoder().encode(context)) as? [String: Any])
        #expect(contextTrip["expires"] as? Int64 == 1_800_000_900)
    }

    private func request(received: Date, deadline: Date) throws -> PendingRequest {
        let old = PendingRequest(requestId: "r_expiry", hostId: "h_expiry", sessionId: nil, bib: nil,
                                 agent: nil, host: nil, title: "Check?", subject: nil, options: [],
                                 receivedAt: received, answered: nil)
        var object = try #require(JSONSerialization.jsonObject(with: JSONEncoder().encode(old)) as? [String: Any])
        object["expiresAt"] = deadline.timeIntervalSinceReferenceDate
        return try JSONDecoder().decode(PendingRequest.self, from: JSONSerialization.data(withJSONObject: object))
    }

    @Test(arguments: [false, true])
    func theHelperDeadlineOverridesTheLegacyWindow(_ extended: Bool) throws {
        let dir = FileManager.default.temporaryDirectory.appending(path: "expiry-\(UUID())")
        defer { try? FileManager.default.removeItem(at: dir) }
        let current = now
        let store = PendingStore(directory: dir, clock: { current })
        let r = try request(received: now - (extended ? 900 : 1), deadline: now + (extended ? 60 : -1))
        store.upsert(r)
        #expect(store.isExpired(r.id) == !extended)
        #expect(store.claim(r.id, hostId: r.hostId) == extended)
    }

    @Test func anExtensionIsMonotonicAndSurvivesReopeningTheStore() throws {
        let dir = FileManager.default.temporaryDirectory.appending(path: "expiry-\(UUID())")
        defer { try? FileManager.default.removeItem(at: dir) }
        let current = now
        let store = PendingStore(directory: dir, clock: { current })
        let original = try request(received: now - 500, deadline: now + 30)
        store.upsert(original)
        store.upsert(try request(received: now, deadline: now + 900))
        store.upsert(try request(received: now, deadline: now - 1))
        let reopened = PendingStore(directory: dir, clock: { current })
        #expect(reopened.request(original.id)?.receivedAt == original.receivedAt)
        #expect(reopened.request(original.id)?.expiry == now + 900)
        reopened.close(original.id, hostId: original.hostId)
        reopened.upsert(try request(received: now, deadline: now + 1000))
        #expect(!reopened.claim(original.id, hostId: original.hostId))
    }

    @Test func pruningKeepsAnExtendedRequestPastTheOldRetentionCutoff() throws {
        let dir = FileManager.default.temporaryDirectory.appending(path: "expiry-\(UUID())")
        defer { try? FileManager.default.removeItem(at: dir) }
        let current = now
        let store = PendingStore(directory: dir, clock: { current })
        let r = try request(received: now - 2000, deadline: now + 60)
        store.upsert(r)
        store.prune()
        #expect(store.request(r.id) != nil)
        #expect(RequestLifetime.waiting(store.all(), now: now).count == 1)
        #expect(LiveContext(pending: r, now: now).expires == 1_800_000_060)
    }
}

extension NotificationServiceCoreTests {
    @Test func delayedPushHonorsItsExplicitDeadline() throws {
        pair(Self.hostID, name: "machine")
        var p = payload(ts: 1_793_706_300_000)
        p.expires = 1_793_707_260
        let result = core.process(try sealed(p))
        #expect(result.pendingChanged)
        #expect(pending.request("r_aaaaaaaaaa")?.expiry == Self.now + 60)
        #expect(!pending.isExpired("r_aaaaaaaaaa"))
    }

    @Test func anExplicitlyExpiredPushHasNoActions() throws {
        pair(Self.hostID, name: "machine")
        var p = payload()
        p.expires = 1_793_707_199
        let result = core.process(try sealed(p))
        expectQuiet(result.content, "The helper deadline passed")
        #expect(result.content.categoryIdentifier.isEmpty)
    }
}
