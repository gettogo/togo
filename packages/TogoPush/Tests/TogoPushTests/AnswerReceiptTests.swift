import Foundation
import Testing
@testable import TogoPush

struct AnswerReceiptTests {
    @Test func theHelperAnswerFixtureDecodesWithoutInventingApproval() throws {
        // receipt.v1 fixture from the helper: title/body empty, Unix-millisecond timestamp.
        let data = Data("""
        {"v":1,"kind":"answer","sessionId":null,"bib":null,"agent":null,"title":"","body":"",
         "requestId":"r_abcdefghij","ts":1793707200000,"result":"expired"}
        """.utf8)
        let payload = try JSONDecoder().decode(PushPayload.self, from: data)
        let encoded = try #require(JSONSerialization.jsonObject(with: JSONEncoder().encode(payload)) as? [String: Any])
        #expect(encoded["result"] as? String == "expired")
    }
}

extension AnswerReceiptTests {
    private var now: Date { Date(timeIntervalSince1970: 1_793_707_200) }
    private func event(_ result: String, host: String = "h_one", delta: Int64 = 0) throws -> AnswerReceiptEvent {
        try #require(AnswerReceiptEvent(payload: PushPayload(kind: .answer, title: "", body: "",
            requestId: "r_abcdefghij", ts: 1_793_707_200_000 + delta, result: result), hostId: host, now: now))
    }

    @Test func aReceiptWinsOverTheLaterMailboxCompletion() throws {
        let dir = FileManager.default.temporaryDirectory.appending(path: "receipt-\(UUID())")
        defer { try? FileManager.default.removeItem(at: dir) }
        let current = now
        let store = PendingStore(directory: dir, clock: { current })
        #expect(store.claim("r_abcdefghij", hostId: "h_one"))
        #expect(store.receive(try event("applied"), now: now))
        store.markRelaySent("r_abcdefghij")
        #expect(store.request("r_abcdefghij")?.answered == .confirmed)
        #expect(LiveAnswerFeedback.resolve(store.request("r_abcdefghij"), local: .sent, now: now) == .applied)
        #expect(!store.receive(try event("applied"), now: now))
        #expect(!store.receive(try event("failed", delta: 10), now: now))
    }

    @Test func anotherHostAndAnOlderReceiptCannotChangeTheResult() throws {
        let dir = FileManager.default.temporaryDirectory.appending(path: "receipt-\(UUID())")
        defer { try? FileManager.default.removeItem(at: dir) }
        let current = now
        let store = PendingStore(directory: dir, clock: { current })
        #expect(store.claim("r_abcdefghij", hostId: "h_one"))
        store.markRelaySent("r_abcdefghij")
        #expect(!store.receive(try event("applied", host: "h_other"), now: now))
        #expect(store.receive(try event("unconfirmed", delta: 10), now: now))
        #expect(!store.receive(try event("failed"), now: now))
        #expect(store.request("r_abcdefghij")?.answerReceipt == .unconfirmed)
        #expect(!store.claim("r_abcdefghij", hostId: "h_one"))
    }

    @Test(arguments: ["applied", "answered", "expired", "prompt_gone", "unconfirmed", "failed"])
    func receiptsSurviveRelaunchWithoutReopeningAnUnknownRequest(_ result: String) throws {
        let dir = FileManager.default.temporaryDirectory.appending(path: "receipt-\(UUID())")
        defer { try? FileManager.default.removeItem(at: dir) }
        let current = now
        let store = PendingStore(directory: dir, clock: { current })
        #expect(store.receive(try event(result), now: now))
        let reopened = PendingStore(directory: dir, clock: { current })
        #expect(reopened.request("r_abcdefghij")?.answerReceipt?.rawValue == result)
        #expect(!reopened.claim("r_abcdefghij", hostId: "h_one"))
    }
}

extension NotificationServiceCoreTests {
    @Test(arguments: ["applied", "answered", "expired", "prompt_gone", "unconfirmed", "failed", "future_result"])
    func sealedAnswerReceiptsUseFixedCopyAndNeverOfferAnswerActions(_ result: String) throws {
        pair(Self.hostID, name: "machine")
        let p = PushPayload(kind: .answer, title: "Untrusted title", body: "Untrusted body",
            requestId: "r_abcdefghij", ts: 1_793_707_200_000, result: result)
        let response = core.process(try sealed(p))
        #expect(response.content.categoryIdentifier.isEmpty || response.content.categoryIdentifier == NotificationCategoryID.info)
        #expect(!response.content.body.contains("Untrusted"))
        if let known = AnswerReceiptResult(rawValue: result) {
            #expect(response.pendingChanged)
            #expect(pending.request("r_abcdefghij")?.answerReceipt == known)
            if let message = known.message { #expect(response.content.body == message) }
            else { #expect(response.content.sound == nil) }
        } else {
            #expect(!response.pendingChanged)
            #expect(pending.request("r_abcdefghij") == nil)
        }
    }
}
