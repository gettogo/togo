import Foundation
import Testing
@testable import TogoKit

/// The turn's stopwatch: working time pauses while the turn waits on the user, the wait counts
/// on its own, and the clock is the same wherever it is read.
struct TurnClockTests {
    private typealias S = HarnessSamples
    private let t0 = HarnessSamples.t0

    private func approval(_ id: String, at seconds: TimeInterval, resolved: Bool, turn: String? = "u1") -> HarnessItem {
        HarnessItem(id: id, seq: 10, turnId: turn, at: S.at(seconds),
                    body: .approval(.init(requestId: "r-\(id)", resolved: resolved ? "approved" : nil)))
    }

    private func question(_ id: String, at seconds: TimeInterval, answered: Bool) -> HarnessItem {
        HarnessItem(id: id, seq: 11, turnId: "u1", at: S.at(seconds),
                    body: .question(.init(questions: [], answered: answered ? ["q": ["yes"]] : nil)))
    }

    @Test func noWaitsCountsTheWholeTurn() {
        let clock = TurnClock(start: t0)
        #expect(clock.worked(at: t0 + 12) == 12)
        #expect(clock.currentWait(at: t0 + 12) == nil)
        #expect(!clock.isWaiting)
        #expect(TurnClock(start: t0, end: t0 + 40).worked(at: t0 + 90) == 40)
    }

    @Test func oneWaitIsTakenOut() {
        let clock = TurnClock(start: t0, waits: [.init(start: t0 + 10, end: t0 + 40)])
        #expect(clock.worked(at: t0 + 10) == 10)
        #expect(clock.worked(at: t0 + 25) == 10)
        // After the answer it resumes from where it paused: no jump.
        #expect(clock.worked(at: t0 + 40) == 10)
        #expect(clock.worked(at: t0 + 41) == 11)
    }

    @Test func twoWaitsAndOverlapsCountOnce() {
        let clock = TurnClock(start: t0, end: t0 + 100, waits: [
            .init(start: t0 + 10, end: t0 + 20),
            .init(start: t0 + 50, end: t0 + 70),
            .init(start: t0 + 60, end: t0 + 80),
        ])
        #expect(clock.worked(at: t0 + 200) == 100 - 10 - 30)
    }

    @Test func anOpenWaitPausesWorkAndCountsItself() {
        let clock = TurnClock(start: t0, waits: [.init(start: t0 + 30, end: nil)])
        #expect(clock.isWaiting)
        #expect(clock.worked(at: t0 + 30) == 30)
        #expect(clock.worked(at: t0 + 42) == 30)
        #expect(clock.currentWait(at: t0 + 42) == 12)
        #expect(clock.workingOrigin(at: t0 + 42) == t0 + 12)
    }

    @Test func waitsFromTheLedger() {
        let items = [S.user("u1", seq: 1), S.tool("t1", seq: 2, turn: "u1"), approval("ap", at: 3, resolved: true)]
        let waits = TurnClock.waits(in: items, resolvedAt: ["ap": t0 + 20], turnEnd: nil)
        #expect(waits == [.init(start: t0 + 3, end: t0 + 20)])
    }

    /// History never saw the answer: the wait ends at the next step of the turn, else the
    /// turn's end.
    @Test func historyWithoutAResolutionTime() {
        let next = S.tool("t2", seq: 12, turn: "u1", start: 30, end: 31)
        let items = [approval("ap", at: 3, resolved: true), next]
        #expect(TurnClock.waits(in: items, resolvedAt: [:], turnEnd: t0 + 72) == [.init(start: t0 + 3, end: t0 + 30)])
        let last = [approval("ap", at: 3, resolved: true)]
        #expect(TurnClock.waits(in: last, resolvedAt: [:], turnEnd: t0 + 72) == [.init(start: t0 + 3, end: t0 + 72)])
        // An unanswered request in an ended turn ends with it; in a running one it stays open.
        let open = [question("q", at: 5, answered: false)]
        #expect(TurnClock.waits(in: open, resolvedAt: [:], turnEnd: t0 + 72) == [.init(start: t0 + 5, end: t0 + 72)])
        #expect(TurnClock.waits(in: open, resolvedAt: [:], turnEnd: nil) == [.init(start: t0 + 5, end: nil)])
    }

    /// An adapter with turn items: the fold line's "Worked" leaves the wait out.
    @Test func aTurnAdapterFoldsWithoutTheWait() {
        let rows = TurnFold.rows([
            S.user("u1", seq: 1), S.tool("t1", seq: 2, turn: "u1"), approval("ap", at: 3, resolved: true),
            S.tool("t2", seq: 4, turn: "u1", start: 33, end: 34), S.turn("u1", seq: 5, steps: 2, files: 0),
        ], resolvedAt: ["ap": t0 + 33])
        guard let work = rows.first(where: { if case .work = $0 { true } else { false } }),
              case .work(_, let summary, _) = work else {
            Issue.record("no work row")
            return
        }
        #expect(abs((summary.elapsed(now: t0 + 500) ?? 0) - 42) < 0.001)
    }

    /// An adapter without turn items: each stretch of steps keeps its own span, no waits.
    @Test func aTurnlessAdapterKeepsItsSpans() {
        let rows = TurnFold.rows([
            S.user("u1", seq: 1, turn: nil), S.tool("t1", seq: 2, turn: nil, start: 1, end: 5),
        ])
        guard rows.count == 2, case .work(_, let summary, _) = rows[1] else {
            Issue.record("unexpected rows \(rows.map(\.id))")
            return
        }
        #expect(summary.waits.isEmpty)
        #expect(abs((summary.elapsed(now: t0 + 100) ?? 0) - 4) < 0.001)
    }

    @Test func theLedgerRecordsOnlyAnswersItSaw() {
        var ledger = ResolutionLedger()
        // Already answered when first seen: no entry, history falls back.
        ledger.observe([approval("old", at: 1, resolved: true)], now: t0 + 50)
        #expect(ledger.resolvedAt.isEmpty)
        ledger.observe([approval("ap", at: 3, resolved: false)], now: t0 + 4)
        #expect(ledger.resolvedAt.isEmpty)
        let changed = ledger.observe([approval("ap", at: 3, resolved: true)], now: t0 + 20)
        #expect(changed)
        #expect(ledger.resolvedAt == ["ap": t0 + 20])
        // A later sighting keeps the first time.
        ledger.observe([approval("ap", at: 3, resolved: true)], now: t0 + 90)
        #expect(ledger.resolvedAt == ["ap": t0 + 20])
    }

    // MARK: - The origin of the number on screen

    /// Working: the origin is the turn's start, so `now - origin` is the working time and a
    /// one-second schedule from it ticks exactly when the shown second changes.
    @Test func displayOriginWhileWorking() {
        let clock = TurnClock(start: t0)
        #expect(clock.displayOrigin(waiting: false, at: t0 + 7) == t0)
        #expect(clock.displayOrigin(waiting: false, at: t0 + 300) == t0)
    }

    /// Waiting: the origin is the open wait's start, so the number shown is the wait.
    @Test func displayOriginWhileWaiting() {
        let clock = TurnClock(start: t0, waits: [.init(start: t0 + 30, end: nil)])
        #expect(clock.displayOrigin(waiting: true, at: t0 + 42) == t0 + 30)
        #expect(clock.displayOrigin(waiting: true, at: t0 + 90) == t0 + 30)
        #expect((t0 + 42).timeIntervalSince(clock.displayOrigin(waiting: true, at: t0 + 42)) == clock.currentWait(at: t0 + 42))
        // A wait that began before the turn counts from the turn's start.
        let early = TurnClock(start: t0, waits: [.init(start: t0 - 5, end: nil)])
        #expect(early.displayOrigin(waiting: true, at: t0 + 3) == t0)
    }

    /// Resumed after an answer: the origin moves on by the wait and then stays fixed, so the
    /// working number continues from its paused value.
    @Test func displayOriginAfterTheWaitResumes() {
        let clock = TurnClock(start: t0, waits: [.init(start: t0 + 10, end: t0 + 40)])
        let origin = clock.displayOrigin(waiting: false, at: t0 + 41)
        #expect(origin == t0 + 30)
        #expect(clock.displayOrigin(waiting: false, at: t0 + 100) == origin)
        for second in [41.0, 55, 100] {
            #expect((t0 + second).timeIntervalSince(origin) == clock.worked(at: t0 + second))
        }
        // Asked for the wait's origin with no wait open, it answers the working one.
        #expect(clock.displayOrigin(waiting: true, at: t0 + 41) == origin)
    }

    /// Each one-second tick from the shown origin changes the shown number by exactly one,
    /// across a wait boundary and back.
    @Test func shownSecondsStepByOneFromTheOrigin() {
        let waiting = TurnClock(start: t0, waits: [.init(start: t0 + 10.4, end: nil)])
        let waitOrigin = waiting.displayOrigin(waiting: true, at: t0 + 11)
        for tick in 0..<5 {
            let at = waitOrigin + Double(tick)
            #expect(Int(waiting.currentWait(at: at) ?? -1) == tick)
        }
        let resumed = TurnClock(start: t0, waits: [.init(start: t0 + 10.4, end: t0 + 20.9)])
        let origin = resumed.displayOrigin(waiting: false, at: t0 + 21)
        let paused = Int(resumed.worked(at: t0 + 20.9))
        for tick in 0..<5 {
            let at = origin + Double(paused + tick)
            #expect(Int(resumed.worked(at: at).rounded()) == paused + tick)
        }
    }
}
