import Foundation
import SwiftUI
import Testing
import TogoKit
@testable import Togo

/// Chat motion v2: the live line never animates its clock and keeps it last, the store's
/// arrival transaction never reaches it, the tapped answer becomes the answered line, and
/// every motion in the app says why it moves (the v2 baseline is empty).
@MainActor struct ChatMotionTests {
    private let t0 = Date(timeIntervalSince1970: 1_793_000_000)

    private func working(waits: [TurnClock.Wait] = [], waiting: Bool = false, step: String? = nil) -> WorkingState {
        WorkingState(clock: TurnClock(start: t0, waits: waits), step: step, waiting: waiting)
    }

    // MARK: Following the store

    /// While the line stays, its contents change with no animation at all: a tick, a step, or
    /// waiting ↔ working never rides the store's `resize` transaction.
    @Test func aLineThatStaysChangesWithoutAnimation() {
        let a = working(step: "Reading a")
        let b = working(waits: [.init(start: t0 + 5, end: nil)], waiting: true)
        #expect(LiveFollow.change(from: a, to: b, loaded: true) == .cut)
        #expect(LiveFollow.change(from: b, to: a, loaded: true) == .cut)
        #expect(LiveFollow.change(from: a, to: a, loaded: true) == .cut)
        #expect(!LiveFollow.Change.cut.animates)
    }

    /// A turn starting or ending moves the rows on `resize`; before the first load the line is
    /// simply there.
    @Test func aTurnStartingOrEndingMakesRoom() {
        let a = working()
        #expect(LiveFollow.change(from: nil, to: a, loaded: true) == .appear)
        #expect(LiveFollow.change(from: a, to: nil, loaded: true) == .end)
        #expect(LiveFollow.Change.appear.animates && LiveFollow.Change.end.animates)
        #expect(LiveFollow.change(from: nil, to: a, loaded: false) == .cut)
        #expect(LiveFollow.change(from: a, to: nil, loaded: false) == .cut)
    }

    // MARK: The live line's layout

    /// Word, separator, clock: the clock is always last, so its growth pushes nothing.
    @Test func theClockComesLast() {
        let line = LiveLineLayout.segments(working(), now: t0 + 69)
        #expect(line == [.word(WorkingCopy.working), .separator, .clock("1m 9s")])
        let waiting = working(waits: [.init(start: t0 + 10, end: nil)], waiting: true)
        #expect(LiveLineLayout.segments(waiting, now: t0 + 22) == [.word(WorkingCopy.waiting), .separator, .clock("12s")])
        // Waiting on a request the clock cannot place: the word alone, no clock.
        #expect(LiveLineLayout.segments(working(waiting: true), now: t0 + 5) == [.word(WorkingCopy.waiting)])
        #expect(WorkingCopy.line(working(), now: t0 + 9) == "Working · 9s")
    }

    /// Each tick of the line's one schedule changes the shown number by exactly one, while
    /// working, across the wait boundary, while waiting, and after the answer.
    @Test func shownSecondsStepByOneAcrossAWaitAndBack() {
        func ticks(_ state: WorkingState, from first: Int = 0) -> [Int] {
            let origin = LiveLineLayout.origin(state)
            return (first..<first + 4).map { Int(WorkingCopy.seconds(state, now: origin + Double($0) + 0.001) ?? -1) }
        }
        // Working from the start.
        let start = ticks(working())
        #expect(start == [0, 1, 2, 3])
        // The wait opens at 10.4 s: the number cuts to the wait and counts from 0.
        let waiting = working(waits: [.init(start: t0 + 10.4, end: nil)], waiting: true)
        #expect(ticks(waiting) == [0, 1, 2, 3])
        // Answered at 40.4 s: working resumes from its paused value (10) and steps by one.
        let resumed = working(waits: [.init(start: t0 + 10.4, end: t0 + 40.4)])
        #expect(Int(WorkingCopy.seconds(resumed, now: t0 + 40.4) ?? -1) == 10)
        #expect(abs(LiveLineLayout.origin(resumed).timeIntervalSince(t0 + 30)) < 0.001)
        #expect(ticks(resumed, from: 11) == [11, 12, 13, 14])
    }

    /// A subagent's running line keeps its counts first and its clock last.
    @Test func aRunningWorkLineLeadsWithItsWords() {
        let summary = WorkSummary(running: true, startedAt: t0, endedAt: nil, steps: 6, filesChanged: 2, failed: false)
        let lead = ChatCopy.workRunningLead(summary)
        #expect(lead.hasPrefix("Working"))
        #expect(lead.contains("6"))
        #expect(!lead.contains("s ·"))
        let bare = WorkSummary(running: true, startedAt: t0, endedAt: nil, steps: 0, filesChanged: 0, failed: false)
        #expect(ChatCopy.workRunningLead(bare) == "Working")
    }

    // MARK: Answer continuity

    /// The answer the person tapped is recorded per decision, and only that control is the
    /// source of the answered line.
    @Test func theTappedAnswerIsRecordedPerDecision() {
        var tap = AnswerTap<Decision>()
        #expect(!tap.isSource(.approve) && !tap.isSource(.deny))
        tap.record(.approve)
        #expect(tap.isSource(.approve))
        #expect(!tap.isSource(.deny))
        #expect(!tap.isSource(.option(2)))
        // After a failure the person taps another answer: that one is the source now.
        tap.record(.option(2))
        #expect(tap.isSource(.option(2)))
        #expect(!tap.isSource(.approve))
        tap.clear()
        #expect(tap.answer == nil)
    }

    /// Own answers travel and never bounce; answered elsewhere (or nothing tapped) collapses in
    /// place with the one bounce.
    @Test func ownAnswersTravelAndOthersCollapse() {
        var tap = AnswerTap<Decision>()
        #expect(tap.settle(answeredHere: true) == .collapse)
        tap.record(.deny)
        #expect(tap.settle(answeredHere: true) == .travel)
        #expect(tap.settle(answeredHere: false) == .collapse)
        #expect(!AnswerSettle.travel.bounces)
        #expect(AnswerSettle.collapse.bounces)
        #expect(AnswerMatch.id("ap") == "ap.answer")
    }

    /// Approved or denied here counts as the phone's own answer; elsewhere or expired does not.
    @Test func whatCountsAsAnsweredHere() {
        let open = HarnessItem.Approval(requestId: "r", resolved: nil)
        #expect(!ApprovalRow.answeredHere(approval: open, card: nil))
        #expect(ApprovalRow.answeredHere(approval: .init(requestId: "r", resolved: "phone"), card: nil))
        #expect(!ApprovalRow.answeredHere(approval: .init(requestId: "r", resolved: "desk"), card: nil))
        #expect(QuestionRow.answeredHere(.answered(nil)))
        #expect(QuestionRow.answeredHere(.sending))
        #expect(!QuestionRow.answeredHere(.open))
        #expect(!QuestionRow.answeredHere(.failed("x")))
    }

    // MARK: The lint at zero

    private static let repo = URL(filePath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent()
        .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()

    /// The v2 baseline is empty and every motion in the app, the Watch and the widgets passes
    /// every v2 rule, the same as `scripts/check-motion.sh`.
    @Test func everyMotionSaysWhyItMoves() throws {
        let baseline = try String(contentsOf: Self.repo.appending(path: "scripts/motion-v2-baseline.txt"), encoding: .utf8)
        let entries = baseline.split(separator: "\n").filter { line in
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            return !trimmed.isEmpty && !trimmed.hasPrefix("#")
        }
        #expect(entries.isEmpty, "\(entries)")
        var found: [String] = []
        for folder in ["Togo", "TogoWatch", "TogoWidgets", "TogoWatchWidgets"] {
            let root = Self.repo.appending(path: "apps/ios/\(folder)")
            guard let files = FileManager.default.enumerator(at: root, includingPropertiesForKeys: nil) else { continue }
            for case let url as URL in files where url.pathExtension == "swift" {
                let path = folder + "/" + String(url.path.dropFirst(root.path.count + 1))
                if path.hasPrefix("Togo/Design/Motion") { continue }
                let source = try String(contentsOf: url, encoding: .utf8)
                for offence in MotionLint.v2Offences(in: source, path: path) {
                    found.append("\(path):\(offence.line): \(offence.rule)")
                }
            }
        }
        #expect(found.isEmpty, "\(found.joined(separator: "\n"))")
    }
}
