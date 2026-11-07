import Foundation
import Testing
import TogoKit
@testable import Togo

/// The chat's one live indicator, and when the composer's keyboard helps.
@MainActor struct LiveIndicatorTests {
    private let t0 = Date(timeIntervalSince1970: 1_793_000_000)

    private func prompt(_ id: String, queued: Bool = false) -> ChatRow {
        .prompt(HarnessItem(id: id, seq: 1, turnId: "turn", at: WireDate(t0),
                            body: .user(.init(text: "go", queued: queued, source: "phone"))))
    }

    private func work(_ id: String, running: Bool) -> ChatRow {
        .work(id: id, summary: WorkSummary(running: running, startedAt: t0, endedAt: running ? nil : t0 + 4,
                                           steps: 1, filesChanged: 0, failed: false), steps: [])
    }

    private var live: WorkingState { WorkingState(since: t0, step: "Reading src/a.ts") }

    // MARK: Placement

    @Test func idleShowsNoIndicator() {
        #expect(WorkingLogic.placement(rows: [prompt("p"), work("w", running: true)], live: nil) == nil)
    }

    @Test func theRunningTurnLineCarriesTheIndicator() {
        let rows = [prompt("p0"), work("w0", running: false), prompt("p1"), work("w1", running: true)]
        #expect(WorkingLogic.placement(rows: rows, live: live) == .row("w1"))
    }

    @Test func aTurnWithoutAWorkLineYetGetsOneAtTheFoot() {
        let rows = [prompt("p0"), work("w0", running: true), prompt("p1")]
        #expect(WorkingLogic.placement(rows: rows, live: live) == .foot)
        #expect(WorkingLogic.placement(rows: [], live: live) == .foot)
    }

    @Test func aFinishedLineAfterThePromptDoesNotCarryIt() {
        let rows = [prompt("p"), work("w", running: false)]
        #expect(WorkingLogic.placement(rows: rows, live: live) == .foot)
    }

    @Test func queuedPromptsDoNotEndTheRunningTurn() {
        let rows = [prompt("p"), work("w", running: true), prompt("q", queued: true)]
        #expect(WorkingLogic.placement(rows: rows, live: live) == .row("w"))
    }

    @Test func theFootLineRunsFromTheSend() {
        let summary = WorkingLogic.footSummary(live)
        #expect(summary.running)
        #expect(summary.steps == 0)
        #expect(ChatCopy.workLine(summary, now: t0 + 12) == "Working 12s")
    }

    // MARK: Interrupt

    /// What the helper reports once the owner interrupts a turn that left a background shell
    /// running: the turn ended as interrupted, working false, one shell running. The turn shows
    /// as stopped, the live indicator goes, the composer sends again, and the shell stays
    /// counted in mission control without counting as work.
    @Test func anInterruptedTurnEndsTheLiveIndicatorButKeepsTheShell() {
        let user = HarnessItem(id: "u7", seq: 1, turnId: "u7", at: WireDate(t0),
                               body: .user(.init(text: "try another", queued: false, source: "phone")))
        let turn = HarnessItem(id: "u7.turn", seq: 9, turnId: "u7", at: WireDate(t0),
                               body: .turn(.init(state: .interrupted, startedAt: WireDate(t0),
                                                 endedAt: WireDate(t0 + 8), steps: 2, filesChanged: 0)))
        let summary = MissionSummary(background: 1, working: false)
        // Sent from the phone a moment before the helper reported the turn at all.
        #expect(WorkingLogic.awaitingEnded(awaitingSince: t0 - 0.5, wasWorking: false, working: summary.working,
                                           items: [user, turn], now: t0 + 9))
        #expect(WorkingLogic.state(items: [user, turn], working: summary.working, awaitingSince: nil,
                                   localSince: nil, now: t0 + 9) == nil)
        let rows = [prompt("u7"), work("u7.turn", running: false)]
        #expect(WorkingLogic.placement(rows: rows, live: nil) == nil)
        #expect(CommandLineLogic.sendMode(working: summary.working) == .now)
        #expect(MissionText.pill(summary) == "1 shell")
    }

    // MARK: Keyboard help

    @Test func promptsGetTheKeyboardsHelp() {
        #expect(CommandLineLogic.keyboardAssists(""))
        #expect(CommandLineLogic.keyboardAssists("please fix the sign in bug"))
        #expect(CommandLineLogic.keyboardAssists("look at @src/a.ts and fix it"))
        #expect(CommandLineLogic.keyboardAssists("mail me@example.com"))
    }

    @Test func commandsAndMentionsAreTypedExactly() {
        #expect(!CommandLineLogic.keyboardAssists("/"))
        #expect(!CommandLineLogic.keyboardAssists("/review"))
        #expect(!CommandLineLogic.keyboardAssists("/review the auth change"))
        #expect(!CommandLineLogic.keyboardAssists("@"))
        #expect(!CommandLineLogic.keyboardAssists("look at @src/au"))
    }

    // MARK: Beacon light

    @Test func theLightStaysInsideTheCard() {
        for step in 0..<400 {
            let point = BeaconDrift.point(time: Double(step) * 0.37, period: 11, phase: 0)
            #expect((0.05...0.95).contains(point.x))
            #expect((0.05...0.95).contains(point.y))
        }
    }
}
