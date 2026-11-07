import Foundation
import Testing
@testable import TogoKit

struct HarnessTimelineTests {
    typealias S = HarnessSamples

    @Test func applyBeforeLoadIsEarly() {
        var timeline = HarnessTimeline()
        #expect(timeline.apply(S.items([S.user("u1", seq: 1)])) == .early)
        #expect(timeline.items.isEmpty)
    }

    @Test func loadSortsBySeqAndKeepsTheCursor() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 5, items: [S.reply("m", seq: 5, turn: "u1"), S.user("u1", seq: 1)], more: true))
        #expect(timeline.items.map(\.id) == ["u1", "m"])
        #expect(timeline.cursor == 5)
        #expect(timeline.hasOlder)
        #expect(timeline.oldestSeq == 1)
    }

    @Test func appliesInSeqOrderAndMovesTheCursor() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 1, items: [S.user("u1", seq: 1)]))
        let note = S.items([S.reply("m", seq: 3, turn: "u1"), S.tool("t", seq: 2, turn: "u1")])
        #expect(timeline.apply(note) == .applied)
        #expect(timeline.items.map(\.id) == ["u1", "t", "m"])
        #expect(timeline.cursor == 3)
    }

    @Test func updateReplacesInPlace() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 3, items: [
            S.user("u1", seq: 1), S.tool("t", seq: 2, turn: "u1", state: .running, end: nil), S.reply("m", seq: 3, turn: "u1"),
        ]))
        #expect(timeline.apply(S.items([S.tool("t", seq: 4, turn: "u1", state: .ok)])) == .applied)
        #expect(timeline.items.map(\.id) == ["u1", "t", "m"])
        #expect(timeline.items[1].seq == 4)
        guard case .tool(let tool) = timeline.items[1].body else { Issue.record("not a tool"); return }
        #expect(tool.state == .ok)
    }

    @Test func seqAtOrBelowTheCursorIsIgnored() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 5, items: [S.reply("m", seq: 5, turn: nil, text: "new")]))
        #expect(timeline.apply(S.items([S.reply("m", seq: 4, turn: nil, text: "old")])) == .ignored)
        guard case .assistant(let text) = timeline.items[0].body else { Issue.record("not a reply"); return }
        #expect(text.text == "new")
        #expect(timeline.cursor == 5)
    }

    @Test func gapAfterTheCursorReopens() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 2, items: [S.user("u1", seq: 2)]))
        #expect(timeline.apply(S.items([S.reply("m", seq: 4, turn: "u1")])) == .reopen)
        #expect(timeline.items.count == 1)
    }

    @Test func anotherEpochOrResetReopens() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 1, items: [S.user("u1", seq: 1)]))
        #expect(timeline.apply(S.items([S.reply("m", seq: 2, turn: "u1")], epoch: "e2")) == .reopen)
        #expect(timeline.apply(S.reset(epoch: "e1")) == .reopen)
    }

    @Test func loadOfAnotherEpochReplacesAndSameEpochMerges() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 2, items: [S.user("u1", seq: 1), S.reply("m", seq: 2, turn: "u1")]))
        timeline.load(S.open(cursor: 3, items: [S.reply("m2", seq: 3, turn: "u1")]))
        #expect(timeline.items.map(\.id) == ["u1", "m", "m2"])
        timeline.load(S.open(epoch: "e2", cursor: 1, items: [S.user("x", seq: 1)]))
        #expect(timeline.items.map(\.id) == ["x"])
        #expect(timeline.epoch == "e2")
        #expect(timeline.cursor == 1)
    }

    @Test func prependAddsOlderItemsFirstWithoutDuplicates() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 10, items: [S.user("u2", seq: 9), S.reply("m2", seq: 10, turn: "u2")], more: true))
        timeline.prepend(HarnessPage(items: [S.reply("m1", seq: 4, turn: "u1"), S.user("u1", seq: 3), S.user("u2", seq: 9)], more: false))
        #expect(timeline.items.map(\.id) == ["u1", "m1", "u2", "m2"])
        #expect(!timeline.hasOlder)
        #expect(timeline.item("m1")?.seq == 4)
    }

    @Test func holdsAtMostTheLimit() {
        var timeline = HarnessTimeline()
        let many = (1...(HarnessTimeline.limit + 5)).map { S.reply("m\($0)", seq: $0, turn: nil) }
        timeline.load(S.open(cursor: many.count, items: many))
        #expect(timeline.items.count == HarnessTimeline.limit)
        #expect(timeline.items.first?.id == "m6")
        #expect(timeline.hasOlder)
    }

    @Test func conversationSplitsSubagentItems() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 3, items: [
            S.user("u1", seq: 1), S.agent("a1", seq: 2, turn: "u1"), S.tool("t", seq: 3, turn: nil, parent: "a1"),
        ]))
        #expect(timeline.conversation().map(\.id) == ["u1", "a1"])
        #expect(timeline.conversation(parent: "a1").map(\.id) == ["t"])
    }

    @Test func sentItemShowsWithoutMovingTheCursor() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 1, items: [S.user("u1", seq: 1)]))
        timeline.insertSent(S.user("u2", seq: 2, queued: true))
        #expect(timeline.items.map(\.id) == ["u1", "u2"])
        #expect(timeline.cursor == 1)
        #expect(timeline.apply(S.items([S.user("u2", seq: 2)])) == .applied)
        #expect(timeline.items.count == 2)
    }

    @Test func sentItemStaysAfterOutputThatCameBeforeIt() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 1, items: [S.user("u1", seq: 1)]))
        timeline.insertSent(S.user("u2", seq: 4, queued: true))
        #expect(timeline.apply(S.items([S.tool("t", seq: 2, turn: "u1"), S.reply("m", seq: 3, turn: "u1")])) == .applied)
        #expect(timeline.items.map(\.id) == ["u1", "t", "m", "u2"])
        #expect(timeline.apply(S.items([S.user("u2", seq: 4), S.reply("r", seq: 5, turn: "u2")])) == .applied)
        #expect(timeline.items.map(\.id) == ["u1", "t", "m", "u2", "r"])
    }
}

struct HarnessFeedStateTests {
    typealias S = HarnessSamples

    @Test func earlyNotesReplayAfterLoad() {
        var feed = HarnessFeedState()
        #expect(feed.apply(S.items([S.reply("m", seq: 2, turn: "u1")])) == .early)
        #expect(feed.apply(S.delta(2, [.init(op: .summary, summary: MissionSummary(working: true))])) == .early)
        #expect(feed.load(S.open(cursor: 1, items: [S.user("u1", seq: 1)], rev: 1)) == .applied)
        #expect(feed.timeline.items.map(\.id) == ["u1", "m"])
        #expect(feed.mission.summary.working)
    }

    @Test func earlyNotesAlreadyInTheResultAreDropped() {
        var feed = HarnessFeedState()
        _ = feed.apply(S.items([S.user("u1", seq: 1, text: "stale")]))
        feed.load(S.open(cursor: 2, items: [S.user("u1", seq: 2, text: "fresh")]))
        guard case .user(let user) = feed.timeline.items[0].body else { Issue.record("not a prompt"); return }
        #expect(user.text == "fresh")
    }

    @Test func resetReopensFromTheStart() {
        var feed = HarnessFeedState()
        feed.load(S.open(cursor: 7, items: [S.user("u1", seq: 7)]))
        #expect(feed.openAfter == 7)
        #expect(feed.apply(S.reset(epoch: "e2")) == .reopen)
        #expect(feed.openAfter == 0)
        feed.load(S.open(epoch: "e2", cursor: 1, items: [S.user("x", seq: 1)]))
        #expect(feed.timeline.items.map(\.id) == ["x"])
        #expect(feed.openAfter == 1)
    }

    @Test func gapReopensWithTheCursor() {
        var feed = HarnessFeedState()
        feed.load(S.open(cursor: 3, items: [S.user("u1", seq: 3)]))
        #expect(feed.apply(S.items([S.reply("m", seq: 6, turn: "u1")])) == .reopen)
        #expect(feed.openAfter == 3)
    }

    @Test func missionNotesGoToMission() {
        var feed = HarnessFeedState()
        feed.load(S.open(cursor: 1, items: [], rev: 4))
        #expect(feed.apply(S.delta(5, [.init(op: .upsert, section: .background, row: S.row("bash_7"))])) == .applied)
        #expect(feed.mission.section(.background)?.rows.map(\.id) == ["bash_7"])
        #expect(feed.apply(S.delta(7, [])) == .reopen)
    }
}
