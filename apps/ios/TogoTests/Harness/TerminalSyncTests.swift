import Foundation
import Testing
import TogoKit
import TogoTerminal
@testable import Togo

/// Show in terminal (jump math, needle matching, the band's lifecycle), command blocks from
/// shell items, subagent threads and background jobs.
@MainActor
struct TerminalSyncTests {
    static let t0 = WireDate(Date(timeIntervalSince1970: 1_000))

    static func item(_ id: String, seq: Int, parent: String? = nil, turn: String? = nil, _ body: HarnessItem.Body) -> HarnessItem {
        HarnessItem(id: id, seq: seq, turnId: turn, parent: parent, at: t0, body: body)
    }

    static func timeline(_ items: [HarnessItem], mission: HarnessMission? = nil) -> HarnessTimeline {
        var timeline = HarnessTimeline()
        timeline.load(HarnessOpenResult(
            epoch: "e1", cursor: items.map(\.seq).max() ?? 0, items: items, more: false,
            mission: mission ?? HarnessMission(rev: 1, summary: MissionSummary(), sections: [])
        ))
        return timeline
    }

    // MARK: Jump math

    @Test func jumpScrollsSoTheLineSitsAThirdDown() {
        // 500 lines of history over a 30-row screen, cursor on row 20: line 520 is the cursor line.
        let frame = ScrollbackFrame(base: 500, rows: 30, cursorRow: 20, top: 500)
        #expect(JumpLanding.resolve(linesFromBottom: 100, frame: frame) == .scroll(top: 410, row: 10))
        // On the live screen: no scroll past the bottom, the row is where the line is.
        #expect(JumpLanding.resolve(linesFromBottom: 5, frame: frame) == .scroll(top: 500, row: 15))
        #expect(JumpLanding.resolve(linesFromBottom: 0, frame: frame) == .scroll(top: 500, row: 20))
    }

    @Test func jumpClampsAtTheOldestLine() {
        let frame = ScrollbackFrame(base: 40, rows: 30, cursorRow: 29, top: 40)
        // Line 3 of history: the view cannot start above line 0.
        #expect(JumpLanding.resolve(linesFromBottom: 66, frame: frame) == .scroll(top: 0, row: 3))
        // Exactly the oldest line.
        #expect(JumpLanding.resolve(linesFromBottom: 69, frame: frame) == .scroll(top: 0, row: 0))
    }

    @Test func jumpFallsBackToTheCaptureSheetWhenThePhoneHasNoSuchLine() {
        // Mosh: no history on the phone at all.
        let mosh = ScrollbackFrame(base: 0, rows: 30, cursorRow: 12, top: 0)
        #expect(JumpLanding.resolve(linesFromBottom: 13, frame: mosh) == .capture)
        #expect(JumpLanding.resolve(linesFromBottom: 12, frame: mosh) == .scroll(top: 0, row: 0))
        // A short buffer after a fresh attach.
        let fresh = ScrollbackFrame(base: 10, rows: 30, cursorRow: 29, top: 10)
        #expect(JumpLanding.resolve(linesFromBottom: 200, frame: fresh) == .capture)
        // No grid yet, or nonsense from the wire.
        #expect(JumpLanding.resolve(linesFromBottom: 1, frame: nil) == .capture)
        #expect(JumpLanding.resolve(linesFromBottom: -1, frame: fresh) == .capture)
        #expect(JumpLanding.resolve(linesFromBottom: 1, frame: ScrollbackFrame(base: 0, rows: 0, cursorRow: 0, top: 0)) == .capture)
    }

    @Test func jumpClampsACursorRowOffTheScreen() {
        let frame = ScrollbackFrame(base: 100, rows: 24, cursorRow: 99, top: 100)
        #expect(JumpLanding.resolve(linesFromBottom: 0, frame: frame) == .scroll(top: 100, row: 23))
    }

    @Test func needleSettlesTheRowNearestTheExpectedOne() {
        let rows = ["$ ls", "a.txt b.txt", "> fix the failing   date test", "working", "> fix the failing date test"]
        // Exact.
        #expect(JumpLanding.refine(row: 2, needle: "> fix the failing date test", rows: rows) == 2)
        // Off by two: the nearest match wins, spacing squeezed.
        #expect(JumpLanding.refine(row: 4, needle: "> fix the failing date test", rows: rows) == 4)
        #expect(JumpLanding.refine(row: 3, needle: ">  fix the failing date test", rows: rows) == 2)
        // A needle longer than the row matches by its start.
        #expect(JumpLanding.refine(row: 0, needle: "a.txt  b.txt" + String(repeating: " more words", count: 10), rows: rows) == 1)
        // Not near: keep the line the helper gave, clamped to the screen.
        #expect(JumpLanding.refine(row: 9, needle: "nowhere", rows: rows) == 4)
        #expect(JumpLanding.refine(row: 1, needle: nil, rows: rows) == 1)
        #expect(JumpLanding.refine(row: 1, needle: " ", rows: rows) == 1)
        #expect(JumpLanding.refine(row: 3, needle: "x", rows: []) == 0)
    }

    @Test func captureSheetOpensAtTheLastNeedle() {
        let lines = ["go test", "FAIL date", "go test", "ok"]
        #expect(JumpLanding.captureLine(needle: "go test", lines: lines) == 2)
        #expect(JumpLanding.captureLine(needle: "absent", lines: lines) == 3)
        #expect(JumpLanding.captureLine(needle: nil, lines: lines) == 3)
        #expect(JumpLanding.captureLine(needle: "go", lines: []) == nil)
    }

    // MARK: Highlight

    @Test func bandHoldsThenGoesAndANewerJumpTakesItOver() {
        var band = JumpHighlight()
        #expect(!band.isShown)
        let first = band.show()
        #expect(band.isShown)
        let second = band.show()
        // The first jump's hold ends while the second owns the band: nothing happens.
        let firstEnded = band.end(first)
        #expect(!firstEnded)
        #expect(band.isShown)
        let secondEnded = band.end(second)
        #expect(secondEnded)
        #expect(!band.isShown)
        // Ending twice does nothing.
        let again = band.end(second)
        #expect(!again)
        let third = band.show()
        band.clear()
        let afterClear = band.end(third)
        #expect(!afterClear)
    }

    @Test func bandFadesOnlyWithMotion() {
        #expect(JumpHighlight.fades(reduceMotion: false))
        #expect(!JumpHighlight.fades(reduceMotion: true))
        #expect(JumpHighlight.hold == .seconds(2))
    }

    // MARK: Command blocks

    static func shell(_ command: String, state: HarnessState, exit: Int?, output: String? = nil, seconds: Double? = nil) -> HarnessItem {
        let end = seconds.map { WireDate(t0.date.addingTimeInterval($0)) }
        return item("cmd1", seq: 1, .shell(.init(
            command: command, cwd: "/src", state: state, exitCode: exit, startedAt: t0, endedAt: end, output: output
        )))
    }

    @Test func blockStatusFromState() throws {
        let running = try #require(CommandBlock(Self.shell("make", state: .running, exit: nil)))
        #expect(running.status == .running)
        #expect(!running.canRerun)
        #expect(running.badge.isEmpty)
        let ok = try #require(CommandBlock(Self.shell("make", state: .ok, exit: 0, seconds: 4)))
        #expect(ok.status == .succeeded)
        #expect(ok.badge == "4s")
        let failed = try #require(CommandBlock(Self.shell("make", state: .failed, exit: 2, seconds: 0.2)))
        #expect(failed.status == .failed(2))
        #expect(failed.badge == "exit 2")
        #expect(failed.canRerun)
        let unknown = try #require(CommandBlock(Self.shell("make", state: .ok, exit: nil)))
        #expect(unknown.status == .finished)
        #expect(CommandBlock(Self.item("n", seq: 1, .notice(.init(level: "info", code: "x", text: "y")))) == nil)
    }

    @Test func blockOutputFoldsWhenLong() throws {
        let short = try #require(CommandBlock(Self.shell("ls", state: .ok, exit: 0, output: "a\nb")))
        #expect(short.lineCount == 2)
        #expect(!short.startsCollapsed)
        #expect(short.copyText == "a\nb")
        let long = (1...13).map(String.init).joined(separator: "\n")
        let tall = try #require(CommandBlock(Self.shell("seq 13", state: .ok, exit: 0, output: long)))
        #expect(tall.lineCount == 13)
        #expect(tall.startsCollapsed)
        let quiet = try #require(CommandBlock(Self.shell("  true  ", state: .ok, exit: 0)))
        #expect(!quiet.hasOutput)
        #expect(quiet.copyText == "true")
        #expect(quiet.lineCount == 0)
        let blank = try #require(CommandBlock(Self.shell(" ", state: .ok, exit: 0)))
        #expect(blank.title == TerminalSyncCopy.command)
        #expect(!blank.canRerun)
    }

    // MARK: Subagents

    static func agent(_ id: String, seq: Int, parent: String? = nil, name: String = "Explorer") -> HarnessItem {
        item(id, seq: seq, parent: parent, turn: parent == nil ? "t1" : nil, .agent(.init(
            agentId: id, name: name, role: "explore", state: .running, startedAt: t0
        )))
    }

    static var nestedTimeline: HarnessTimeline {
        timeline([
            item("u1", seq: 1, turn: "t1", .user(.init(text: "look around", queued: false, source: "phone"))),
            agent("a1", seq: 2),
            item("a1.p", seq: 3, parent: "a1", .user(.init(text: "find the tests", queued: false, source: "unknown"))),
            item("a1.r", seq: 4, parent: "a1", .assistant(.init(text: "Found 3", partial: false))),
            item("a1.ok", seq: 5, parent: "a1", .approval(.init(requestId: "r0", resolved: "phone"))),
            agent("a2", seq: 6, parent: "a1", name: "Reader"),
            item("a2.q", seq: 7, parent: "a2", .approval(.init(requestId: "r1"))),
            item("r1", seq: 8, turn: "t1", .assistant(.init(text: "Done", partial: false))),
        ])
    }

    @Test func subagentThreadShowsOnlyItsOwnItems() {
        let thread = SubagentThread(id: "a1", timeline: Self.nestedTimeline)
        #expect(thread.title == "Explorer")
        #expect(thread.parent == nil)
        #expect(thread.state == .running)
        let ids = thread.rows.flatMap { row -> [String] in
            if case .work(_, _, let steps) = row { return steps.map(\.id) }
            return [row.id]
        }
        #expect(ids == ["a1.p", "a1.r", "a1.ok", "a2"])
        // The nested agent's own items stay in its own thread.
        let nested = SubagentThread(id: "a2", timeline: Self.nestedTimeline)
        #expect(nested.parent == "a1")
        #expect(nested.rows.map(\.id) == ["a2.q"])
        // An unknown agent has no rows and a plain title.
        let missing = SubagentThread(id: "zz", timeline: Self.nestedTimeline)
        #expect(missing.rows.isEmpty)
        #expect(missing.title == TerminalSyncCopy.subagent)
    }

    @Test func subagentLineageWalksUpAndStopsAtLoops() {
        #expect(SubagentThread.lineage(of: "a2", in: Self.nestedTimeline) == ["a1", "a2"])
        let looped = Self.timeline([Self.agent("x", seq: 1, parent: "y"), Self.agent("y", seq: 2, parent: "x")])
        #expect(SubagentThread.lineage(of: "x", in: looped) == ["y", "x"])
    }

    @Test func waitingSubagentNeedsBubbleUpToTheMainChat() {
        let timeline = Self.nestedTimeline
        #expect(SubagentThread.waitingNeeds(in: timeline).map(\.id) == ["a2.q"])
        let rows = SubagentThread.mainRows(timeline)
        let ids = rows.map(\.id)
        #expect(ids.contains("a2.q"))
        // Answered ones stay down in their thread.
        #expect(!ids.contains("a1.ok"))
        // The waiting approval sits where it arrived: before the reply that came after it.
        let needs = ids.firstIndex(of: "a2.q")
        let reply = ids.firstIndex(of: "r1")
        #expect(needs != nil && reply != nil && needs! < reply!)
        // Without subagent needs the main rows are the plain fold.
        let plain = Self.timeline([Self.item("u", seq: 1, .user(.init(text: "hi", queued: false, source: "phone")))])
        #expect(SubagentThread.mainRows(plain) == TurnFold.rows(plain.conversation()))
    }

    // MARK: Background

    @Test func backgroundJobJoinsItsRowAndItem() throws {
        let row = HarnessRow(
            id: "bg1", title: "Dev server", state: .running, startedAt: Self.t0,
            tail: ["one", "two", "three", "four", "five"], open: HarnessOpen(kind: .terminal, ref: "s_abcdefghij:1.0")
        )
        let mission = HarnessMission(rev: 1, summary: MissionSummary(background: 1), sections: [
            HarnessSection(kind: .background, rows: [row]),
        ])
        let item = Self.item("item-bg", seq: 1, .background(.init(
            jobId: "bg1", kind: "server", title: "Dev server", command: "npm run dev", state: .running, startedAt: Self.t0
        )))
        var state = MissionState()
        state.load(mission)
        let timeline = Self.timeline([item], mission: mission)
        let byItem = try #require(BackgroundJob.find("item-bg", mission: state, timeline: timeline))
        #expect(byItem.id == "bg1")
        #expect(byItem.command == "npm run dev")
        #expect(byItem.tail == ["two", "three", "four", "five"])
        #expect(byItem.terminal == TerminalTarget("s_abcdefghij:1.0"))
        let byRow = try #require(BackgroundJob.find("bg1", mission: state, timeline: timeline))
        #expect(byRow == byItem)
        // An item with no row still shows, with nowhere to open.
        let lone = try #require(BackgroundJob.find("item-bg", mission: MissionState(), timeline: timeline))
        #expect(lone.terminal == nil && lone.outputScreen == nil)
        #expect(BackgroundJob.find("nope", mission: state, timeline: timeline) == nil)
    }

    @Test func terminalTargetsAreSessionIdsWithAnOptionalPane() {
        #expect(TerminalTarget("s_abcdefghij")?.sessionID == "s_abcdefghij")
        #expect(TerminalTarget("s_abcdefghij:2")?.pane == "2")
        #expect(TerminalTarget("s_abcdefghij:2.1")?.pane == "2.1")
        #expect(TerminalTarget("u_6d79")?.sessionID == "u_6d79")
        for bad in ["", "s_ABCDEFGHIJ", "s_abc", "u_6d7", "u_ZZ", "s_abcdefghij:", "s_abcdefghij:;rm", "s_abcdefghij:.1", "x_abcdefghij"] {
            #expect(TerminalTarget(bad) == nil, "\(bad)")
        }
    }
}
