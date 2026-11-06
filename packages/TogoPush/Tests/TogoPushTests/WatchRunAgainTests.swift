import Foundation
import Testing
@testable import TogoPush

/// Run again tapped on a notification shown on the Watch reaches the phone as a message the
/// phone reads strictly before it starts anything.
struct WatchRunAgainTests {
    private let host = "h_mmg42klgyqzwneiskrelxms3j7"

    @Test func roundTripsByHostOrMachine() throws {
        let byHost = WatchRunAgainMessage(id: "n-1", machineId: host, sessionId: "s_abcdefghij")
        #expect(WatchRunAgainMessage(message: byHost.message) == byHost)
        #expect(byHost.message["type"] as? String == "runAgain")
        let machine = UUID().uuidString
        let byMachine = WatchRunAgainMessage(id: "n-2", machineId: machine, sessionId: "s_abcdefghij")
        #expect(WatchRunAgainMessage(message: byMachine.message) == byMachine)
    }

    @Test func refusesAnythingElse() {
        let good: [String: Any] = ["type": "runAgain", "id": "n-1", "machineId": host, "sessionId": "s_abcdefghij"]
        func with(_ key: String, _ value: Any?) -> [String: Any] {
            var m = good
            m[key] = value
            return m
        }
        for bad in [
            with("type", "reply"),
            with("id", nil),
            with("id", "../x"),
            with("machineId", "h_studio"),
            with("machineId", nil),
            with("sessionId", "s_1"),
            with("sessionId", "u_abcdefghij"),
            with("text", "rm -rf"),
            with("requestId", "r_aaaaaaaaaa"),
            with("hostId", host),
        ] {
            #expect(WatchRunAgainMessage(message: bad) == nil, "\(bad)")
        }
        #expect(WatchRunAgainMessage(message: good) != nil)
    }

    /// A reply message is never read as a run again, nor the other way round.
    @Test func neverConfusedWithReply() {
        let run = WatchRunAgainMessage(id: "n-1", machineId: host, sessionId: "s_abcdefghij")
        #expect(WatchReplyMessage(message: run.message) == nil)
        let reply = WatchReplyMessage(id: "n-1", target: .prompt(machineId: host, sessionId: "s_abcdefghij"), text: "")
        #expect(WatchRunAgainMessage(message: reply.message) == nil)
    }
}
