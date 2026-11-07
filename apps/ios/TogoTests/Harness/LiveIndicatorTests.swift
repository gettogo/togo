import Foundation
import Testing
import TogoKit
@testable import Togo

/// The chat's one live indicator, and when the composer's keyboard helps.
@MainActor struct LiveIndicatorTests {
    private let t0 = Date(timeIntervalSince1970: 1_793_000_000)

    private func item(_ id: String, _ body: HarnessItem.Body, at seconds: TimeInterval, seq: Int) -> HarnessItem {
        HarnessItem(id: id, seq: seq, turnId: "u1", at: WireDate(t0 + seconds), body: body)
    }

    private func turn(ended: TimeInterval?) -> HarnessItem {
        item("u1.turn", .turn(.init(state: ended == nil ? .running : .ok, startedAt: WireDate(t0),
                                    endedAt: ended.map { WireDate(t0 + $0) }, steps: 2, filesChanged: 0)), at: 0, seq: 2)
    }

    private func approval(resolved: Bool) -> HarnessItem {
        item("ap", .approval(.init(requestId: "r-ap", resolved: resolved ? "approved" : nil)), at: 10, seq: 4)
    }

    private var user: HarnessItem {
        item("u1", .user(.init(text: "go", queued: false, source: "phone")), at: 0, seq: 1)
    }

    private var tool: HarnessItem {
        item("t1", .tool(.init(callId: "t1", name: "Read", title: "Read a", category: .read, state: .ok,
                               startedAt: WireDate(t0 + 1), endedAt: WireDate(t0 + 2))), at: 1, seq: 3)
    }

    // MARK: The stopwatch

    /// Working pauses while the turn waits, "Waiting for you · 15s" counts the wait, and after
    /// the answer Working resumes from where it paused: no jump.
    @Test func workingPausesWhileWaitingAndResumes() throws {
        let waiting = try #require(WorkingLogic.state(
            items: [user, turn(ended: nil), tool, approval(resolved: false)], working: true,
            awaitingSince: nil, localSince: t0, now: t0 + 25, needsYou: 1
        ))
        #expect(waiting.waiting)
        #expect(WorkingCopy.line(waiting, now: t0 + 25) == "Waiting for you · 15s")
        #expect(abs(waiting.clock.worked(at: t0 + 25) - 10) < 0.001)

        let resolved: [String: Date] = ["ap": t0 + 40]
        let working = try #require(WorkingLogic.state(
            items: [user, turn(ended: nil), tool, approval(resolved: true)], working: true,
            awaitingSince: nil, localSince: t0, now: t0 + 40, resolvedAt: resolved
        ))
        #expect(!working.waiting)
        #expect(WorkingCopy.line(working, now: t0 + 40) == "Working · 10s")
        #expect(WorkingCopy.line(working, now: t0 + 45) == "Working · 15s")
    }

    /// The foot's live line and the fold line read the same clock, before and after an answer,
    /// and the fold line's "Worked" is the live line's last value.
    @Test func theLiveLineAndTheFoldLineAgree() throws {
        let resolved: [String: Date] = ["ap": t0 + 40]
        let live = try #require(WorkingLogic.state(
            items: [user, turn(ended: nil), tool, approval(resolved: true)], working: true,
            awaitingSince: nil, localSince: t0, now: t0 + 60, resolvedAt: resolved
        ))
        let running = TurnFold.rows([user, turn(ended: nil), tool, approval(resolved: true)], resolvedAt: resolved)
        let finished = TurnFold.rows([user, turn(ended: 60), tool, approval(resolved: true)], resolvedAt: resolved)
        for rows in [running, finished] {
            guard let work = rows.first(where: { if case .work = $0 { true } else { false } }),
                  case .work(_, let summary, _) = work else {
                Issue.record("no work row")
                return
            }
            #expect(abs((summary.elapsed(now: t0 + 60) ?? 0) - live.clock.worked(at: t0 + 60)) < 0.001)
        }
        #expect(abs(live.clock.worked(at: t0 + 60) - 30) < 0.001)
    }

    @Test func aSentPromptCountsFromTheSend() throws {
        let state = try #require(WorkingLogic.state(items: [], working: false, awaitingSince: t0, localSince: nil, now: t0 + 1))
        #expect(WorkingCopy.line(state, now: t0 + 12) == "Working · 12s")
        #expect(WorkingCopy.spoken(state, now: t0 + 12).hasPrefix("Working, "))
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
