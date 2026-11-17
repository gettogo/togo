import Foundation
import Testing
import TogoPush
@testable import TogoWatch

private struct FeedbackSender: AnswerSending {
    func send(requestId: String, decision: String, pairing: Pairing) async throws {}
}

@MainActor struct WatchAnswerFeedbackTests {
    @Test func receiptWaitIsBoundedWithoutChangingTheLegacyHold() {
        #expect(WatchHome.answerHold(expectsReceipt: true) == .seconds(10))
        #expect(WatchHome.answerHold(expectsReceipt: false) == WatchHome.holdAfterAnswer)
    }

    @Test(arguments: ["approve", "deny"])
    func receiptConfirmsBeforeThePageCompletes(decision: String) async {
        let fixture = Fixture(receipts: true)
        var feedback = WatchAnswerFeedback()
        #expect(feedback.observe(fixture.store.state(of: "r"), expectsReceipt: true) == nil)
        await fixture.store.answer("r", decision)
        #expect(fixture.store.state(of: "r") == .sent)
        #expect(feedback.observe(
            .sent, expectsReceipt: fixture.store.expectsAnswerReceipt("r"), decision: decision
        ) == nil)
        fixture.receipt("applied")
        let state = fixture.store.state(of: "r")
        #expect(state == .answered)
        #expect(feedback.observe(state, expectsReceipt: true, decision: decision)
            == (decision == "deny" ? .denied : .answered))
        #expect(feedback.observe(state, expectsReceipt: true, decision: decision) == nil)
    }

    @Test(arguments: [false, nil] as [Bool?])
    func legacyRelayAcknowledgementCompletesThePage(receipts: Bool?) async {
        let fixture = Fixture(receipts: receipts)
        var feedback = WatchAnswerFeedback()
        #expect(feedback.observe(.open, expectsReceipt: false) == nil)
        await fixture.store.answer("r", "deny")
        #expect(!fixture.store.expectsAnswerReceipt("r"))
        #expect(feedback.observe(fixture.store.state(of: "r"), expectsReceipt: false, decision: "deny") == .denied)
        #expect(feedback.observe(.sent, expectsReceipt: false) == nil)
    }

    @Test(arguments: ["answered", "expired", "prompt_gone", "unconfirmed", "failed"])
    func nonAppliedReceiptsDoNotGiveCompletionFeedback(result: String) async {
        let fixture = Fixture(receipts: true)
        var feedback = WatchAnswerFeedback()
        #expect(feedback.observe(.open, expectsReceipt: true) == nil)
        await fixture.store.answer("r", "approve")
        fixture.receipt(result)
        #expect(feedback.observe(fixture.store.state(of: "r"), expectsReceipt: true) == nil)
    }

    @Test(arguments: [WatchStore.RequestState.answered, .sent, .approved, .denied])
    func openingAnOldResultDoesNotBuzzOrDismiss(state: WatchStore.RequestState) {
        var feedback = WatchAnswerFeedback()
        #expect(feedback.observe(state, expectsReceipt: false) == nil)
        #expect(feedback.observe(state, expectsReceipt: false) == nil)
        #expect(feedback.observe(.answered, expectsReceipt: false) == nil)
    }

    @MainActor private final class Fixture {
        let directory = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        let now = Date()
        let store: WatchStore

        init(receipts: Bool?) {
            let directory = self.directory
            let now = self.now
            let pending = PendingStore(directory: directory, clock: { now })
            pending.upsert(PendingRequest(
                requestId: "r", hostId: "h", sessionId: "s", bib: "01", agent: "omp", host: "Studio",
                title: "Run tests?", subject: nil,
                options: [PushOption(n: 1, label: "Yes", role: "approve"), PushOption(n: 2, label: "No", role: "deny")],
                receivedAt: now, answered: nil
            ))
            let pairings = PairingStore(directory: directory)
            pairings.put(Pairing(
                hostId: "h", hostKey: "key", relayURL: URL(string: "https://relay.example")!, machineName: "Studio",
                appHostID: nil, pairedAt: now, tokenHash: "", liveActivityCommand: false, answerReceipts: receipts
            ))
            store = WatchStore(dependencies: .init(
                pending: pending, pairings: pairings, sender: FeedbackSender(), clock: { now }, reloadComplication: {}
            ))
        }

        func receipt(_ result: String) {
            var push = PushPayload(
                kind: .answer, sessionId: "s", bib: "01", agent: "omp", host: "Studio", title: "", body: "",
                requestId: "r", ts: Int64(now.timeIntervalSince1970 * 1000)
            )
            push.result = result
            store.ingest(push, hostId: "h")
        }
    }
}
