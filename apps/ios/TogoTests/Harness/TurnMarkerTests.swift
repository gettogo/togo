import Foundation
import Testing
import TogoKit
@testable import Togo

/// Turns the agent starts on its own (after a background task or subagent finished) and the
/// pauses between messages.
@MainActor struct TurnMarkerTests {
    private let t0 = Date(timeIntervalSince1970: 1_793_000_000)

    private func at(_ s: TimeInterval) -> WireDate { WireDate(t0.addingTimeInterval(s)) }

    private func user(_ id: String, _ s: TimeInterval) -> HarnessItem {
        HarnessItem(id: id, seq: Int(s), turnId: id, at: at(s), body: .user(.init(text: "go", queued: false, source: "phone")))
    }

    private func reply(_ id: String, turn: String, _ s: TimeInterval) -> HarnessItem {
        HarnessItem(id: id, seq: Int(s), turnId: turn, at: at(s), body: .assistant(.init(text: "ok", partial: false)))
    }

    private func marker(_ id: String, _ s: TimeInterval, text: String = "Subagent finished") -> HarnessItem {
        HarnessItem(id: id, seq: Int(s), turnId: id, at: at(s),
                    body: .notice(.init(level: "info", code: HarnessItem.Notice.continued, text: text)))
    }

    private func tool(_ id: String, turn: String, _ s: TimeInterval) -> HarnessItem {
        HarnessItem(id: id, seq: Int(s), turnId: turn, at: at(s),
                    body: .tool(.init(callId: id, name: "Read", title: "Read a.go", category: .read, state: .ok,
                                      startedAt: at(s), endedAt: at(s + 1))))
    }

    private func turn(_ id: String, start: TimeInterval, end: TimeInterval, steps: Int) -> HarnessItem {
        HarnessItem(id: id + ".turn", seq: Int(end), turnId: id, at: at(start),
                    body: .turn(.init(state: .ok, startedAt: at(start), endedAt: at(end), steps: steps, filesChanged: 0)))
    }

    /// The owner's case: "I'll tell you when each one finishes", then two minutes later the
    /// subagent's follow-up with no prompt between. It opens with its marker and folds its
    /// own work; nothing of it joins the first turn.
    private var conversation: [HarnessItem] {
        [
            user("u1", 0), reply("m1", turn: "u1", 7), turn("u1", start: 0, end: 8, steps: 0),
            marker("n1", 140), tool("t1", turn: "n1", 141), reply("m2", turn: "n1", 144),
            turn("n1", start: 140, end: 145, steps: 1),
        ]
    }

    @Test func agentTurnOpensWithItsMarkerAndOwnFold() {
        let rows = TurnFold.rows(conversation)
        #expect(rows.map(\.id) == ["u1", "m1", "n1", "work.n1", "m2"])
        #expect(rows[2].isContinued)
        guard case .work(_, let summary, let steps) = rows[3] else { Issue.record("no fold for the agent's turn"); return }
        #expect(steps.map(\.id) == ["t1"])
        #expect(summary.steps == 1)
        #expect(summary.startedAt == t0.addingTimeInterval(140))
    }

    @Test func markerIsDrawnBeforeTheTurnsWorkEvenWhenItArrivesLater() {
        let rows = TurnFold.rows([tool("t1", turn: "n1", 141), marker("n1", 142), reply("m2", turn: "n1", 144)])
        #expect(rows.map(\.id) == ["n1", "work.n1", "m2"])
    }

    @Test func otherNoticesStayAfterTheWork() {
        let compacted = HarnessItem(id: "c", seq: 3, turnId: "u1", at: at(2),
                                    body: .notice(.init(level: "info", code: "compacted", text: "Compacted")))
        let rows = TurnFold.rows([user("u1", 0), tool("t1", turn: "u1", 1), compacted])
        #expect(rows.map(\.id) == ["u1", "work.u1", "c"])
        #expect(!rows[2].isContinued)
    }

    @Test func pausesOverAMinuteAreMarked() {
        let rows = TurnFold.rows(conversation)
        let gaps = ChatGaps.gaps(rows)
        // The marker comes 133 s after the first reply at 7 s; nothing else waited.
        #expect(Set(gaps.keys) == ["n1"])
        #expect(gaps["n1"] == 133)
        let next = TurnFold.rows(conversation + [user("u2", 200), reply("m3", turn: "u2", 230)])
        #expect(Set(ChatGaps.gaps(next).keys) == ["n1"])
        let later = TurnFold.rows(conversation + [user("u2", 400)])
        #expect(ChatGaps.gaps(later)["u2"] == 255)
    }

    @Test func replyBeforeTheWorkEndsDoesNotFakeAPause() {
        // Rows are drawn prompt, work, replies: a reply may predate the work's end.
        let rows = TurnFold.rows([
            user("u1", 0), tool("t1", turn: "u1", 1), reply("m1", turn: "u1", 5), turn("u1", start: 0, end: 300, steps: 1),
            user("u2", 330),
        ])
        #expect(ChatGaps.gaps(rows).isEmpty)
    }

    @Test func markerCopy() {
        #expect(TurnMarkerCopy.marker("Subagent finished", gap: nil) == "Subagent finished")
        #expect(TurnMarkerCopy.marker("Subagent finished", gap: 30) == "Subagent finished")
        #expect(TurnMarkerCopy.marker("Background task finished", gap: 132) == "Background task finished · 2m later")
        #expect(TurnMarkerCopy.later(3_840) == "1h 4m later")
        #expect(TurnMarkerCopy.later(7_200) == "2h later")
        #expect(TurnMarkerCopy.later(3 * 86_400) == "3d later")
        for text in [TurnMarkerCopy.marker("Subagent finished", gap: 600), TurnMarkerCopy.later(90)] {
            #expect(!text.contains("\u{2014}"))
        }
    }

    @Test func stampIsTimeTodayAndDatedOtherwise() {
        var calendar = Calendar(identifier: .gregorian)
        calendar.timeZone = .current
        let now = t0
        let today = TurnMarkerCopy.stamp(now.addingTimeInterval(-120), now: now, calendar: calendar)
        #expect(today == now.addingTimeInterval(-120).formatted(date: .omitted, time: .shortened))
        let old = TurnMarkerCopy.stamp(now.addingTimeInterval(-5 * 86_400), now: now, calendar: calendar)
        #expect(old.hasSuffix(now.addingTimeInterval(-5 * 86_400).formatted(date: .omitted, time: .shortened)))
        #expect(old != today)
    }
}
