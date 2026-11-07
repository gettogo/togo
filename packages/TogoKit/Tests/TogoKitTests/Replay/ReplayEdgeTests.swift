import Foundation
import Testing
@testable import TogoKit

/// The hand-written fixtures (protocol/v1/replays/synthetic.*): delivery the recordings rarely
/// catch. The invariants run on them too (ReplayTests); these pin what each one is about.
@Suite("Replay edges")
struct ReplayEdgeTests {
    @Test func reorderedFramesReopenAndKeepTurnOrder() throws {
        let r = try Replay.run("synthetic.reorder")
        #expect(r.ever(\.needsReopen), "the swapped frame should look like a gap")
        let end = r.final
        #expect(!end.needsReopen)
        #expect(end.items.map(\.id) == ["u1", "c1", "a1", "t1.turn"])
        #expect(end.rows.map(\.id) == ["u1", "work.t1", "a1"])
        #expect(end.workingSignals.isEmpty)
    }

    @Test func duplicatesShowOneCardAndOnePendingRequest() throws {
        let r = try Replay.run("synthetic.duplicate")
        guard let answer = r.request("approval.answer") else { throw ReplayError.empty("no answer") }
        let waiting = r.after(answer - 1)
        #expect(waiting.pending.map(\.requestId) == ["r_dupabcdefg"])
        #expect(waiting.openNeeds.map(\.id) == ["ap1"])
        #expect(waiting.mission?.summary.needsYou == 1)
        let end = r.final
        #expect(end.pending.isEmpty && end.openNeeds.isEmpty)
        #expect(end.rows.map(\.id) == ["u1", "work.t1", "ap1", "a1"])
        #expect(end.workingSignals.isEmpty)
    }

    @Test func reconnectMidTurnCatchesUpWithoutPhantoms() throws {
        let r = try Replay.run("synthetic.reconnect-midturn")
        let away = r.after(r.request("events.subscribe", nth: 1)! - 1)
        #expect(away.pending.count == 1, "before the link drops the request waits")
        let end = r.final
        #expect(end.pending.isEmpty, "answered at the desk while away")
        #expect(end.openNeeds.isEmpty)
        #expect(end.workingSignals.isEmpty)
        #expect(Set(end.items.map(\.id)).count == end.items.count)
        #expect(end.rows.map(\.id) == ["u1", "work.t1", "ap1", "a1"])
    }

    @Test func resetReplacesTheOldEpoch() throws {
        let r = try Replay.run("synthetic.reset-epoch")
        let end = r.final
        #expect(end.feed.timeline.epoch == "e2")
        #expect(end.items.map(\.id) == ["u1", "c1", "n1", "t1.turn"])
        #expect(end.workingSignals.isEmpty)
    }

    @Test func eventGapResyncsAndDropsTheAnsweredRequest() throws {
        let r = try Replay.run("synthetic.event-gap")
        #expect(r.ever(\.needsResync))
        #expect(r.final.pending.isEmpty)
        #expect(r.final.openNeeds.isEmpty)
        #expect(!r.final.needsResync)
    }

    /// A late open for a request already closed brings it back in the monitor
    /// (`HostSnapshot.apply` upserts it), so the prompt bar shows a phantom.
    @Test func lateOpenAfterCloseStaysClosed() throws {
        let r = try Replay.run("synthetic.reopen-after-close")
        withKnownIssue("HostSnapshot.apply re-adds a request on an approval.open that comes after its approval.closed, so a late hook resurrects an answered request as a phantom in the prompt bar and the pill.") {
            #expect(r.final.pending.isEmpty, "the answered request is pending again")
        }
        #expect(r.final.openNeeds.isEmpty, "the chat card stays resolved")
    }
}
