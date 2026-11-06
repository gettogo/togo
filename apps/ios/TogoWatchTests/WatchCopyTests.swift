import Foundation
import Testing
import TogoPush
@testable import TogoWatch

struct WatchCopyTests {
    private func session(_ status: String, reason: String? = nil, code: Int? = nil) -> WatchSession {
        WatchSession(
            hostName: "Studio", bib: "01", agentName: "Claude Code", status: status, activity: nil,
            exitReason: reason, exitCode: code
        )
    }

    @Test func watchEndedWords() {
        #expect(WatchCopy.status(session("ended", reason: "exit", code: 0)) == "Ended")
        #expect(WatchCopy.status(session("ended", reason: "interrupted", code: 130)) == "Ended")
        #expect(WatchCopy.status(session("failed", reason: "exit", code: 1)) == "Stopped with an error")
        #expect(WatchCopy.status(session("failed", reason: "error")) == "Stopped with an error")
        #expect(WatchCopy.status(session("failed", reason: "usage_limit")) == "Hit a usage limit")
        #expect(WatchCopy.status(session("ended", reason: "restart")) == "Ended by restart")
        #expect(WatchCopy.status(session("failed")) == "Stopped with an error")
        #expect(WatchCopy.status(session("working")) == "Working")
        #expect(WatchCopy.status(session("exited")) == "Ended", "the phone's word")
    }

    /// An error stop with no exit code leaves the agent alive at its prompt: the Watch offers
    /// Reply, as for a usage limit. With a code the agent is gone and takes none.
    @Test func errorStopOffersReplyNotRunAgain() {
        func row(_ status: String, reason: String?, code: Int?) -> WatchSession {
            var s = session(status, reason: reason, code: code)
            s.machineId = "6F1C2A9E-0000-4000-8000-000000000001"
            s.sessionId = "s_abcdefghij"
            return s
        }
        #expect(row("failed", reason: "error", code: nil).acceptsReply)
        #expect(row("failed", reason: "usage_limit", code: nil).acceptsReply)
        #expect(!row("failed", reason: "error", code: 1).acceptsReply)
        #expect(!row("failed", reason: "exit", code: 1).acceptsReply)
        #expect(WatchCopy.status(row("failed", reason: "error", code: nil)) == "Stopped with an error")
    }

    /// A phone from before exit reasons sends sessions without them.
    @Test func olderPhoneSessionsDecode() throws {
        let json = #"{"hostName":"Studio","bib":"01","agentName":"Claude Code","status":"failed","activity":null}"#
        let decoded = try JSONDecoder().decode(WatchSession.self, from: Data(json.utf8))
        #expect(decoded.exitReason == nil)
        #expect(WatchCopy.status(decoded) == "Stopped with an error")
    }
}
