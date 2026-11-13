import Foundation
import Testing
import TogoPush

struct LiveAnswerFeedbackTests {
    let now = Date(timeIntervalSince1970: 1_794_582_000)

    func request(_ answered: PendingRequest.Answered?) -> PendingRequest {
        .init(
            requestId: "r_aaaaaaaaaa", hostId: "h_mmg42klgyqzwneiskrelxms3j7", sessionId: "s_abcdefghij",
            bib: "03", agent: "shell", host: nil, title: "Run this check?", subject: nil,
            options: [], receivedAt: now, answered: answered, claimedAt: answered == .sending ? now : nil
        )
    }

    @Test func savedAnswerOutranksAnOldActivityState() {
        #expect(LiveAnswerFeedback.resolve(request(.approved), local: nil, now: now) == .sent)
        #expect(LiveAnswerFeedback.resolve(request(.denied), local: .sending, now: now) == .sent)
        #expect(LiveAnswerFeedback.resolve(request(.elsewhere), local: .failed, now: now) == .answeredElsewhere)
    }

    @Test func abandonedClaimDoesNotLeaveSendingForever() {
        var pending = request(.sending)
        #expect(LiveAnswerFeedback.resolve(pending, local: nil, now: now) == .sending)
        pending.claimedAt = now.addingTimeInterval(-PendingRequest.claimTimeout)
        #expect(LiveAnswerFeedback.resolve(pending, local: .sending, now: now) == nil)
    }

    @Test func retryableFailureDoesNotReinstateAnOldSuccess() {
        #expect(LiveAnswerFeedback.resolve(request(nil), local: .failed, now: now) == .failed)
        #expect(LiveAnswerFeedback.resolve(request(nil), local: .sent, now: now) == nil)
        #expect(!LiveAnswerFeedback.failed.blocksAnswer)
        #expect(LiveAnswerFeedback.sent.blocksAnswer)
    }

    @Test func savedDeliveryBlocksEvenSealedApprovalActions() {
        for feedback in [LiveAnswerFeedback.sending, .sent, .answeredElsewhere, .failed] {
            let card = LiveCard(
                agent: "shell", status: "needs_you", waiting: 1, requestId: "r_aaaaaaaaaa", command: nil,
                context: LiveContext(ask: "Run this check?", answer: .approveDeny), machine: nil, stale: false,
                feedback: feedback
            )
            #expect(card.ask == "Run this check?")
            #expect(card.answer == (feedback == .failed ? .approveDeny : nil))
        }
        let working = LiveCard(
            agent: "shell", status: "working", waiting: 0, requestId: nil, command: nil,
            context: nil, machine: nil, stale: false, feedback: .sent
        )
        #expect(working.feedback == nil)
    }
}
