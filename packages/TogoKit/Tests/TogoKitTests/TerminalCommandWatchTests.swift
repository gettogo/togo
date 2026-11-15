import Foundation
import Testing
@testable import TogoKit

struct TerminalCommandWatchTests {
    private let conversation = UUID()

    private func note(_ id: UUID, session: String = "session", epoch: String = "epoch") -> HarnessNotification {
        .init(sessionId: session, kind: .menu, ts: WireDate(.now), epoch: epoch, requestId: id.uuidString)
    }

    @Test func noticeBeforeReceiptWaitsForSubmissionAndIsConsumedOnce() {
        var watch = TerminalCommandWatch()
        let id = UUID()
        watch.begin(requestId: id, conversation: conversation, epoch: "epoch")
        let accepted1 = watch.receive(note(id), sessionId: "session", conversation: conversation, epoch: "epoch")
        #expect(accepted1)
        #expect(watch.ready(conversation: conversation, epoch: "epoch") == nil)
        watch.finish(requestId: id, submitted: true)
        #expect(watch.ready(conversation: conversation, epoch: "epoch") == id)
        let accepted2 = watch.receive(note(id), sessionId: "session", conversation: conversation, epoch: "epoch")
        #expect(!accepted2)
        let accepted3 = watch.claim(requestId: id, conversation: conversation, epoch: "epoch")
        #expect(accepted3)
        let accepted4 = watch.claim(requestId: id, conversation: conversation, epoch: "epoch")
        #expect(!accepted4)
    }

    @Test func deviceSessionEpochAndConversationMustMatch() {
        var watch = TerminalCommandWatch()
        let id = UUID()
        watch.begin(requestId: id, conversation: conversation, epoch: "epoch")
        watch.finish(requestId: id, submitted: true)
        let accepted5 = watch.receive(note(UUID()), sessionId: "session", conversation: conversation, epoch: "epoch")
        #expect(!accepted5)
        let accepted6 = watch.receive(note(id, session: "other"), sessionId: "session", conversation: conversation, epoch: "epoch")
        #expect(!accepted6)
        let accepted7 = watch.receive(note(id, epoch: "old"), sessionId: "session", conversation: conversation, epoch: "epoch")
        #expect(!accepted7)
        let accepted8 = watch.receive(note(id), sessionId: "session", conversation: UUID(), epoch: "epoch")
        #expect(!accepted8)
        #expect(watch.ready(conversation: conversation, epoch: "epoch") == nil)
    }

    @Test func replacementAttemptRejectsAnOldReceiptAndMenu() {
        var watch = TerminalCommandWatch()
        let old = UUID(), current = UUID()
        watch.begin(requestId: old, conversation: conversation, epoch: "epoch")
        watch.begin(requestId: current, conversation: conversation, epoch: "epoch")
        watch.finish(requestId: old, submitted: true)
        let accepted9 = watch.receive(note(old), sessionId: "session", conversation: conversation, epoch: "epoch")
        #expect(!accepted9)
        let accepted10 = watch.receive(note(current), sessionId: "session", conversation: conversation, epoch: "epoch")
        #expect(accepted10)
        #expect(watch.ready(conversation: conversation, epoch: "epoch") == nil)
        watch.finish(requestId: current, submitted: true)
        #expect(watch.ready(conversation: conversation, epoch: "epoch") == current)
    }

    @Test func expiryAndHandbackNeverCreateReadiness() {
        var watch = TerminalCommandWatch()
        let id = UUID(), now = ContinuousClock.now
        watch.begin(requestId: id, conversation: conversation, epoch: "epoch", now: now)
        watch.finish(requestId: id, submitted: true, now: now)
        let accepted11 = watch.receive(note(id), sessionId: "session", conversation: conversation, epoch: "epoch", now: now + .seconds(61))
        #expect(!accepted11)
        #expect(watch.ready(conversation: conversation, epoch: "epoch", now: now + .seconds(61)) == nil)
        watch.cancel()
        let accepted12 = watch.receive(note(id), sessionId: "session", conversation: conversation, epoch: "epoch", now: now)
        #expect(!accepted12)
    }

    @Test func observedOutcomeDiscardsAnEarlyMenu() {
        var watch = TerminalCommandWatch()
        let id = UUID()
        watch.begin(requestId: id, conversation: conversation, epoch: "epoch")
        watch.receive(note(id), sessionId: "session", conversation: conversation, epoch: "epoch")
        watch.finish(requestId: id, submitted: false)
        #expect(watch.ready(conversation: conversation, epoch: "epoch") == nil)
    }

    @Test func notificationCorrelationIsAdditiveAndMalformedIdentityIsIgnored() throws {
        let id = UUID()
        let original = note(id)
        let decoded = try JSONDecoder().decode(HarnessNotification.self, from: JSONEncoder().encode(original))
        #expect(decoded.requestId == id.uuidString && decoded.kind == .menu)
        #expect(decoded.sessionId == original.sessionId && decoded.epoch == original.epoch)
        let raw = #"{"sessionId":"session","kind":"menu","epoch":"epoch","ts":"2026-11-14T12:00:00Z","requestId":42}"#
        let malformed = try JSONDecoder().decode(HarnessNotification.self, from: Data(raw.utf8))
        #expect(malformed.kind == .menu && malformed.requestId == nil)
        let legacy = HarnessNotification(sessionId: "session", kind: .commands, ts: WireDate(.now), epoch: "epoch")
        #expect(try JSONDecoder().decode(HarnessNotification.self, from: JSONEncoder().encode(legacy)).requestId == nil)
    }
}
