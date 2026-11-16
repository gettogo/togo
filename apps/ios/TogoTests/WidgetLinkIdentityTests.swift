import Foundation
import Testing
import TogoPush
@testable import Togo

@MainActor struct WidgetLinkIdentityTests {
    @Test func waitingLinkUsesThePairingIdentityAndSavedName() throws {
        let id = UUID()
        let now = Date()
        let request = PendingRequest(requestId: "r_widget", hostId: "h_test", sessionId: "s_exact", bib: "04",
            agent: "omp", host: "different-helper-hostname", title: "", subject: nil, options: [], receivedAt: now, answered: nil)
        let summary = WaitingSummary(requests: [request], now: now, machineID: { _ in id }, machineName: { _ in "Saved name" })
        let url = try #require(summary.oldest?.link)
        let query = try #require(URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems)
        #expect(query.first { $0.name == "hostID" }?.value == id.uuidString)
        #expect(query.first { $0.name == "sessionID" }?.value == request.sessionId)
        #expect(query.first { $0.name == "host" }?.value == "Saved name")
    }
}
