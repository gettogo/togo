import Foundation
import Testing
@testable import TogoKit

struct HarnessTurnFoldTests {
    typealias S = HarnessSamples

    @Test func turnFoldsIntoPromptWorkAndReply() {
        let rows = TurnFold.rows([
            S.user("u1", seq: 1),
            S.tool("t1", seq: 2, turn: "u1", files: ["a.go"]),
            S.reply("m1", seq: 3, turn: "u1", text: "Looking."),
            S.tool("t2", seq: 4, turn: "u1", start: 5, end: 9, files: ["a.go", "b.go"]),
            S.reply("m2", seq: 5, turn: "u1"),
        ])
        #expect(rows.map(\.id) == ["u1", "work.u1", "m1", "m2"])
        guard case .work(_, let summary, let steps) = rows[1] else { Issue.record("no work row"); return }
        #expect(steps.map(\.id) == ["t1", "t2"])
        #expect(summary.steps == 2)
        #expect(summary.filesChanged == 2)
        #expect(summary.startedAt == S.at(1).date)
        #expect(summary.endedAt == S.at(9).date)
        #expect(!summary.running)
    }

    /// A prompt taken back before it was typed leaves the chat; the wire flag is optional.
    @Test func withdrawnPromptsAreNotShown() throws {
        var gone = S.user("u2", seq: 3, text: "drop", queued: false)
        gone.body = .user(.init(text: "drop", queued: false, source: "phone", withdrawn: true))
        let rows = TurnFold.rows([S.user("u1", seq: 1), S.reply("m1", seq: 2, turn: "u1"), gone])
        #expect(rows.map(\.id) == ["u1", "m1"])
        let wire = #"{"text":"x","queued":false,"source":"phone"}"#
        let old = try JSONDecoder().decode(HarnessItem.User.self, from: Data(wire.utf8))
        #expect(!old.withdrawn)
        let back = try JSONDecoder().decode(HarnessItem.User.self, from: JSONEncoder().encode(HarnessItem.User(text: "drop", queued: false, source: "phone", withdrawn: true)))
        #expect(back.withdrawn)
    }

    @Test func turnItemNumbersArePreferred() {
        let rows = TurnFold.rows([
            S.user("u1", seq: 1),
            S.tool("t1", seq: 2, turn: "u1"),
            S.turn("u1", seq: 3, steps: 6, files: 2),
        ])
        guard case .work(_, let summary, let steps) = rows[1] else { Issue.record("no work row"); return }
        #expect(summary.steps == 6)
        #expect(summary.filesChanged == 2)
        #expect(steps.count == 1)
        #expect(summary.elapsed(now: .now) == 72)
    }

    /// The live line at the chat's foot stands for a running turn until its first step.
    @Test func runningTurnHasNoWorkLineBeforeAnyStep() {
        let rows = TurnFold.rows([S.user("u1", seq: 1), S.turn("u1", seq: 2, state: .running, steps: 0, files: 0)])
        #expect(rows.map(\.id) == ["u1"])
        let stepped = TurnFold.rows([
            S.user("u1", seq: 1), S.turn("u1", seq: 2, state: .running, steps: 1, files: 0), S.tool("t1", seq: 3, turn: "u1"),
        ])
        #expect(stepped.map(\.id) == ["u1", "work.u1"])
        guard stepped.count == 2, case .work(_, let summary, _) = stepped[1] else { Issue.record("no work row"); return }
        #expect(summary.running)
    }

    @Test func finishedTurnWithoutStepsHasNoWorkRow() {
        let rows = TurnFold.rows([
            S.user("u1", seq: 1), S.reply("m", seq: 2, turn: "u1"), S.turn("u1", seq: 3, steps: 0, files: 0),
        ])
        #expect(rows.map(\.id) == ["u1", "m"])
    }

    @Test func needsAreNeverFolded() {
        let rows = TurnFold.rows([
            S.user("u1", seq: 1),
            S.tool("t1", seq: 2, turn: "u1"),
            S.approval("ap", seq: 3, turn: "u1"),
            S.tool("t2", seq: 4, turn: "u1"),
            S.reply("m", seq: 5, turn: "u1"),
        ])
        #expect(rows.map(\.id) == ["u1", "work.u1", "ap", "m"])
        guard case .needs = rows[2] else { Issue.record("approval folded"); return }
    }

    @Test func subagentItemsAreExcluded() {
        let rows = TurnFold.rows([
            S.user("u1", seq: 1),
            S.agent("a1", seq: 2, turn: "u1"),
            S.tool("inner", seq: 3, turn: "u1", parent: "a1"),
            S.reply("inner-reply", seq: 4, turn: "u1").withParent("a1"),
            S.reply("m", seq: 5, turn: "u1"),
        ])
        #expect(rows.map(\.id) == ["u1", "work.u1", "m"])
        guard case .work(_, _, let steps) = rows[1] else { Issue.record("no work row"); return }
        #expect(steps.map(\.id) == ["a1"])
    }

    @Test func itemsWithoutATurnKeepTheirOwnRows() {
        let notice = HarnessItem(id: "n", seq: 3, at: S.at(0), body: .notice(.init(level: "info", code: "compacted", text: "Compacted")))
        let rows = TurnFold.rows([
            S.tool("a", seq: 1, turn: nil), S.tool("b", seq: 2, turn: nil), notice, S.tool("c", seq: 4, turn: nil),
        ])
        #expect(rows.map(\.id) == ["work.a", "n", "work.c"])
        guard case .work(_, let summary, _) = rows[0] else { Issue.record("no work row"); return }
        #expect(summary.steps == 2)
    }

    @Test func turnsKeepTheirFirstSeenOrder() {
        let rows = TurnFold.rows([
            S.user("u1", seq: 1), S.reply("m1", seq: 2, turn: "u1"),
            S.user("u2", seq: 3), S.reply("m2", seq: 4, turn: "u2"),
        ])
        #expect(rows.map(\.id) == ["u1", "m1", "u2", "m2"])
    }

    @Test func unknownKindsAreSkipped() {
        let odd = HarnessItem(id: "x", seq: 2, turnId: "u1", at: S.at(0), body: .unknown(kind: "hologram"))
        #expect(TurnFold.rows([S.user("u1", seq: 1), odd]).map(\.id) == ["u1"])
    }

    @Test func runningToolKeepsTheWorkLineRunning() {
        let rows = TurnFold.rows([S.user("u1", seq: 1), S.tool("t", seq: 2, turn: "u1", state: .running, end: nil)])
        guard case .work(_, let summary, _) = rows[1] else { Issue.record("no work row"); return }
        #expect(summary.running)
        #expect(summary.endedAt == nil)
    }
}

extension HarnessItem {
    func withParent(_ parent: String) -> HarnessItem {
        var copy = self
        copy.parent = parent
        return copy
    }
}
