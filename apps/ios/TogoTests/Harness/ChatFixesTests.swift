import Foundation
import Testing
import TogoKit
@testable import Togo

/// The rules behind the chat fixes: option labels, the waiting state, the interrupted
/// dedupe, the mode chip's memory and background job labels.
@MainActor struct ChatFixesTests {
    private let t0 = Date(timeIntervalSince1970: 1_793_000_000)

    private func item(_ id: String, seq: Int, parent: String? = nil, _ body: HarnessItem.Body) -> HarnessItem {
        HarnessItem(id: id, seq: seq, turnId: "turn", parent: parent, at: WireDate(t0), body: body)
    }

    private func prompt(_ id: String, seq: Int) -> HarnessItem {
        item(id, seq: seq, .user(.init(text: "go", queued: false, source: "phone")))
    }

    private func approval(_ id: String, seq: Int, resolved: String? = nil, parent: String? = nil) -> HarnessItem {
        item(id, seq: seq, parent: parent, .approval(.init(requestId: "r-\(id)", resolved: resolved)))
    }

    // MARK: Option labels

    @Test func anOptionSplitsAtItsFirstMiddleDot() {
        let split = AnswerOptionText.split("Yes, and switch to auto mode · auto mode handles these prompts for you")
        #expect(split.title == "Yes, and switch to auto mode")
        #expect(split.explanation == "auto mode handles these prompts for you")
    }

    @Test func anExplanationKeepsItsOwnDots() {
        let split = AnswerOptionText.split("Allow · for this session · on this machine")
        #expect(split.title == "Allow")
        #expect(split.explanation == "for this session · on this machine")
    }

    @Test func aPlainLabelIsAllTitle() {
        #expect(AnswerOptionText.split("No, and tell Codex what to do differently").explanation == nil)
        #expect(AnswerOptionText.split("  Yes  ").title == "Yes")
        #expect(AnswerOptionText.split("Yes · ").explanation == nil)
        #expect(AnswerOptionText.split(" · only why").title == "only why")
    }

    @Test func everyStressOptionKeepsItsWholeWords() {
        for option in StressFixtures.options {
            let joined = [option.title, option.explanation].compactMap(\.self).joined(separator: " · ")
            #expect(joined == option.label)
        }
    }

    // MARK: Waiting for you

    @Test func anOpenApprovalTurnsWorkingIntoWaiting() {
        let items = [prompt("p", seq: 1), approval("a", seq: 2)]
        let state = WorkingLogic.state(items: items, working: true, awaitingSince: nil, localSince: t0, now: t0 + 24)
        #expect(state?.waiting == true)
        #expect(state?.step == nil)
        // The line counts the wait itself: "Waiting for you, 24 seconds".
        #expect(WorkingCopy.spoken(state!, now: t0 + 24) == "\(WorkingCopy.waiting), \(ChatCopy.spokenClock(24))")
    }

    @Test func aSubagentsOpenQuestionCountsToo() {
        let question = item("q", seq: 3, parent: "agent-1", .question(.init(questions: [], answered: nil)))
        #expect(WorkingLogic.hasOpenNeeds([prompt("p", seq: 1), question]))
    }

    @Test func theMissionsNeedsCountAloneIsEnough() {
        let state = WorkingLogic.state(items: [prompt("p", seq: 1)], working: true, awaitingSince: nil,
                                       localSince: t0, now: t0, needsYou: 1)
        #expect(state?.waiting == true)
    }

    @Test func answeredOrOlderRequestsDoNotHoldTheRow() {
        #expect(!WorkingLogic.hasOpenNeeds([prompt("p", seq: 1), approval("a", seq: 2, resolved: "phone")]))
        // An approval from before the newest prompt the helper never closed.
        #expect(!WorkingLogic.hasOpenNeeds([approval("old", seq: 1), prompt("p", seq: 2)]))
        let state = WorkingLogic.state(items: [prompt("p", seq: 1)], working: true, awaitingSince: nil,
                                       localSince: t0, now: t0)
        #expect(state?.waiting == false)
    }

    // MARK: Interrupted, said once

    private func notice(_ id: String, seq: Int, code: String) -> ChatRow {
        .notice(item(id, seq: seq, .notice(.init(level: "info", code: code, text: "Interrupted"))))
    }

    private func work(_ id: String, failed: Bool, running: Bool = false) -> ChatRow {
        .work(id: id, summary: WorkSummary(running: running, startedAt: t0, endedAt: running ? nil : t0 + 8,
                                           steps: 2, filesChanged: 0, failed: failed), steps: [])
    }

    @Test func aStoppedTurnDropsItsInterruptedNotice() {
        let rows: [ChatRow] = [.prompt(prompt("p", seq: 1)), work("w", failed: true), notice("n", seq: 5, code: "interrupted")]
        let out = ChatRowDedupe.apply(rows)
        #expect(out.map(\.id) == ["p", "w"])
    }

    @Test func aNoticeWithoutAStoppedLineStays() {
        let rows: [ChatRow] = [
            .prompt(prompt("p0", seq: 1)), work("w0", failed: true),
            .prompt(prompt("p1", seq: 3)), work("w1", failed: false), notice("n", seq: 5, code: "interrupted"),
        ]
        #expect(ChatRowDedupe.apply(rows).map(\.id) == ["p0", "w0", "p1", "w1", "n"])
    }

    @Test func otherNoticesStayBesideAStoppedLine() {
        let rows: [ChatRow] = [.prompt(prompt("p", seq: 1)), work("w", failed: true), notice("m", seq: 4, code: "model")]
        #expect(ChatRowDedupe.apply(rows).map(\.id) == ["p", "w", "m"])
    }

    // MARK: A model change, said once

    private func modelNotice(_ id: String, seq: Int, _ text: String) -> ChatRow {
        .notice(item(id, seq: seq, .notice(.init(level: "info", code: "model", text: text))))
    }

    private func phoneNote(_ text: String, after: String?, value: String, waiting: Bool = false) -> SessionNote {
        SessionNote(id: "note", text: text, code: "model", after: after, at: t0,
                    echo: SessionNote.Echo(value: value, waiting: waiting))
    }

    // Seen on the phone with omp: "Model Claude Sonnet 5" above the prompt, the reply, then the
    // harness's "Model Claude Sonnet 5" again. The phone's line stays where it was made.
    @Test func theHarnessNoticeOfAChangeThisPhoneMadeIsLeftOut() {
        let rows: [ChatRow] = [
            .prompt(prompt("p0", seq: 1)), .prompt(prompt("p1", seq: 3)),
            modelNotice("m", seq: 5, "Model Claude Sonnet 5"),
        ]
        let note = phoneNote(PrimitiveCopy.setTo("Model", "Claude Sonnet 5"), after: "p0", value: "Claude Sonnet 5")
        #expect(SessionNote.merge([note], into: rows).map(\.id) == ["p0", "note", "p1"])
    }

    @Test func aRestartLineAlsoStandsForTheHarnessNotice() {
        let rows: [ChatRow] = [.prompt(prompt("p", seq: 1)), modelNotice("m", seq: 2, "Model: Sonnet")]
        let note = phoneNote(PrimitiveCopy.restartedWith("Sonnet"), after: "p", value: "Sonnet")
        #expect(SessionNote.merge([note], into: rows).map(\.id) == ["p", "note"])
    }

    @Test func aWaitingLineGivesWayToTheHarnessNotice() {
        let rows: [ChatRow] = [.prompt(prompt("p", seq: 1)), modelNotice("m", seq: 2, "Model Sonnet")]
        let note = phoneNote(PrimitiveCopy.setWaiting("Model", "Sonnet", agent: "omp"), after: "p", value: "Sonnet", waiting: true)
        #expect(SessionNote.merge([note], into: rows).map(\.id) == ["p", "m"])
    }

    @Test func anEarlierOrDifferentModelNoticeStays() {
        let rows: [ChatRow] = [
            modelNotice("old", seq: 1, "Model Sonnet"), .prompt(prompt("p", seq: 2)),
            modelNotice("other", seq: 3, "Model Opus"),
        ]
        let note = phoneNote(PrimitiveCopy.setTo("Model", "Sonnet"), after: "p", value: "Sonnet")
        #expect(SessionNote.merge([note], into: rows).map(\.id) == ["old", "p", "note", "other"])
    }

    // MARK: Mode memory

    @Test func theStatusLineWinsThenTheLastKnownMode() {
        #expect(ModeMemory.current(status: "plan", remembered: "default", described: "ask") == "plan")
        #expect(ModeMemory.current(status: nil, remembered: "acceptEdits", described: "ask") == "acceptEdits")
        #expect(ModeMemory.current(status: " ", remembered: nil, described: "ask") == "ask")
        #expect(ModeMemory.current(status: nil, remembered: nil, described: "").isEmpty)
    }

    @Test func aModeMatchesItsChoiceByIdOrLabel() {
        let modes = [HarnessChoice(id: "default", label: "Ask"), HarnessChoice(id: "acceptEdits", label: "Accept edits")]
        #expect(ModeMemory.label(for: "acceptedits", in: modes) == "Accept edits")
        #expect(ModeMemory.label(for: "Ask", in: modes) == "Ask")
        #expect(ModeMemory.label(for: "yolo", in: modes) == "yolo")
        #expect(ModeMemory.label(for: "", in: modes) == nil)
    }

    // MARK: Background labels

    @Test func aDescriptionNamesTheJob() {
        let row = HarnessRow(id: "b", title: "Start the dev server", subtitle: "npm run dev", state: .running)
        #expect(BackgroundLabel.title(row) == "Start the dev server")
    }

    @Test func aCommandTitleIsShortened() {
        let row = StressFixtures.background()
        #expect(BackgroundLabel.title(row) == "Loop: echo, sleep")
        #expect(BackgroundLabel.shorten("cd apps/web && FOO=1 npm run build -- --watch") == "npm run build")
        #expect(BackgroundLabel.shorten("python3 -m http.server 8000") == "python3 -m http.server 8000")
        #expect(BackgroundLabel.shorten("/usr/bin/env NODE_ENV=dev node server.js | tee log") == "node server.js")
        #expect(BackgroundLabel.shorten("sleep 30") == "sleep 30")
        #expect(BackgroundLabel.title(HarnessRow(id: "c", title: StressFixtures.buildCommand, state: .running))
            .count <= BackgroundLabel.maxTitle + 1)
    }

    @Test func theSubtitleIsTheLastLineAndTheTime() {
        let row = StressFixtures.background(startedAgo: 20)
        let subtitle = BackgroundLabel.subtitle(row, now: row.startedAt!.date + 20)
        #expect(subtitle == "\(StressFixtures.lastOutput) · 0:20")
        let quiet = HarnessRow(id: "q", title: "x", state: .running, startedAt: WireDate(t0))
        #expect(BackgroundLabel.subtitle(quiet, now: t0 + 3725) == "\(MissionCopy.noOutputYet) · 1:02:05")
        let done = HarnessRow(id: "d", title: "x", state: .ok, tail: ["ok", ""])
        #expect(BackgroundLabel.subtitle(done, now: t0) == "ok")
    }
}
