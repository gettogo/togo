import Foundation
import Testing
import TogoPush
@testable import Togo

struct LiveContextSourceTests {
    let host = "h_mmg42klgyqzwneiskrelxms3j7"

    var request: PendingRequest {
        PendingRequest(
            requestId: "r_aaaaaaaaaa", hostId: host, sessionId: "s_abcdefghij", bib: "03", agent: "shell",
            host: nil, title: "Run this check?", subject: nil,
            options: [PushOption(n: 1, label: "Approve", role: "approve"), PushOption(n: 2, label: "Deny", role: "deny")],
            receivedAt: Date(), answered: nil
        )
    }

    var state: TogoActivityAttributes.ContentState {
        .init(
            bib: "03", agent: "shell", status: "needs_you", waiting: 1, since: Int(Date().timeIntervalSince1970),
            requestId: request.requestId, command: nil
        )
    }

    @Test func matchingSavedRequestKeepsItsQuestionAndActions() throws {
        let context = try #require(LiveContextSource.fallback(state, hostId: host, pending: request))
        #expect(context.ask == request.title)
        #expect(context.answer == .approveDeny)
    }

    @Test func anotherMachinesRequestCannotSupplyContext() {
        #expect(LiveContextSource.fallback(state, hostId: "h_qqg42klgyqzwneiskrelxms3j7", pending: request) == nil)
    }

    @Test func anotherRequestCannotSupplyContext() {
        var other = request
        other.requestId = "r_bbbbbbbbbb"
        #expect(LiveContextSource.fallback(state, hostId: host, pending: other) == nil)
        #expect(LiveContextSource.fallback(state, hostId: host, pending: nil) == nil)
    }
}
