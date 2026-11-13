import Foundation
import Testing
import TogoPush

struct SavedLiveContextTests {
    let now = Date(timeIntervalSince1970: 1_794_582_000)

    func request(roles: [String] = ["approve", "deny"]) -> PendingRequest {
        PendingRequest(
            requestId: "r_aaaaaaaaaa", hostId: "h_mmg42klgyqzwneiskrelxms3j7", sessionId: "s_abcdefghij",
            bib: "03", agent: "shell", host: nil, title: "Run this check?", subject: nil,
            options: roles.enumerated().map { PushOption(n: $0.offset + 1, label: "Choice \($0.offset)", role: $0.element) },
            receivedAt: now, answered: nil
        )
    }

    @Test func actionsFollowRolesRatherThanOptionLabels() {
        #expect(LiveContext(pending: request(), now: now).answer == .approveDeny)
        #expect(LiveContext(pending: request(roles: ["approve_always", "deny"]), now: now).answer == .deny)
    }

    @Test(arguments: [["approve"], ["approve_always"], ["other"], []])
    func unsupportedChoicesOfferNoApprovalActions(_ roles: [String]) {
        #expect(LiveContext(pending: request(roles: roles), now: now).answer == nil)
    }

    @Test(arguments: [PendingRequest.Answered.sending, .approved, .denied, .elsewhere])
    func answeredOrSendingRequestsOfferNothing(_ answered: PendingRequest.Answered) {
        var pending = request()
        pending.answered = answered
        pending.claimedAt = now
        #expect(LiveContext(pending: pending, now: now).answer == nil)
    }

    @Test func abandonedSendCanBeRetried() {
        var pending = request()
        pending.answered = .sending
        pending.claimedAt = now.addingTimeInterval(-PendingRequest.claimTimeout)
        #expect(LiveContext(pending: pending, now: now).answer == .approveDeny)
    }

    @Test func expiredRequestAndFutureReplayOfferNothing() {
        var pending = request()
        pending.receivedAt = now.addingTimeInterval(-RequestLifetime.seconds)
        let expired = LiveContext(pending: pending, now: now)
        #expect(expired.ask == pending.title)
        #expect(expired.answer == nil)
        pending.receivedAt = now.addingTimeInterval(NotificationServiceCore.maxSkew + 1)
        #expect(LiveContext(pending: pending, now: now).answer == nil)
    }
}
