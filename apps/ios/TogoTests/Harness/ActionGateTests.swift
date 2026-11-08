import Foundation
import Testing
import TogoKit
@testable import Togo

/// Confirm only when it costs: relaunches run with Undo while idle and ask while working; other
/// changes ask only with a confirm line or a destructive role; "With instructions…" is offered
/// only for an action a command runs with an optional argument.
@MainActor struct ActionGateTests {
    private let relaunch = HarnessSelectConfirm(text: "Restart Auggie with this model?", effect: .relaunch)

    @Test func anIdleRelaunchRunsWithUndo() {
        #expect(ActionGate.decide(confirm: relaunch.text, effect: .relaunch, working: false, previous: "Opus")
            == .runWithUndo(previous: "Opus"))
        // Nothing to go back to: it just runs.
        #expect(ActionGate.decide(confirm: relaunch.text, effect: .relaunch, working: false) == .run)
    }

    @Test func aRelaunchWhileWorkingAsksWithAfterThisReply() {
        let decision = ActionGate.decide(confirm: relaunch.text, effect: .relaunch, working: true, agent: "Auggie", previous: "Opus")
        guard case .confirm(let dialog) = decision else {
            Issue.record("expected a dialog, got \(decision)")
            return
        }
        #expect(dialog.title == PrimitiveCopy.restartNow("Auggie"))
        #expect(dialog.confirmLabel == PrimitiveCopy.restart)
        #expect(dialog.offersAfterReply)
        #expect(dialog.instructions == nil)
        #expect(!dialog.destructive)
    }

    @Test func aPlainConfirmLineAsksInAnyState() {
        for working in [false, true] {
            let decision = ActionGate.decide(confirm: "Compact the conversation now?", effect: nil, working: working, label: "Compact")
            guard case .confirm(let dialog) = decision else {
                Issue.record("expected a dialog")
                return
            }
            #expect(dialog.title == "Compact the conversation now?")
            #expect(dialog.confirmLabel == "Compact")
            #expect(!dialog.offersAfterReply)
        }
    }

    @Test func noConfirmLineRuns() {
        #expect(ActionGate.decide(confirm: nil, effect: nil, working: true) == .run)
        #expect(ActionGate.decide(confirm: "  ", effect: nil, working: false) == .run)
    }

    @Test func destructiveAsksEvenWithoutALine() {
        let decision = ActionGate.decide(confirm: nil, effect: nil, working: false, label: "Clear", destructive: true)
        guard case .confirm(let dialog) = decision else {
            Issue.record("expected a dialog")
            return
        }
        #expect(dialog.title == PrimitiveCopy.asking("Clear"))
        #expect(dialog.destructive)
    }

    @Test func instructionsAreOfferedForAnOptionalArgument() {
        let decision = ActionGate.decide(confirm: "Compact now?", effect: nil, working: false, label: "Compact", instructions: "/compact ")
        guard case .confirm(let dialog) = decision else {
            Issue.record("expected a dialog")
            return
        }
        #expect(dialog.instructions == "/compact ")
        #expect(dialog.confirmLabel == PrimitiveCopy.runNow("Compact"))
    }

    @Test func theInstructionsLineComesFromACommandWithAnOptionalHint() {
        let commands = [
            HarnessCompletion(insert: "/compact ", title: "/compact", hint: "[instructions]", kind: .run, action: "compact"),
            HarnessCompletion(insert: "/clear ", title: "/clear", kind: .run, role: .destructive, action: "clear"),
            HarnessCompletion(insert: "/add ", title: "/add", hint: "<file>", kind: .args, action: "add")
        ]
        #expect(ActionGate.instructionsLine(for: "compact", in: commands) == "/compact ")
        #expect(ActionGate.instructionsLine(for: "clear", in: commands) == nil)
        #expect(ActionGate.instructionsLine(for: "add", in: commands) == nil)
        #expect(ActionGate.instructionsLine(for: "missing", in: commands) == nil)
    }

    // MARK: Through PrimitiveActions

    private func context(
        working: Bool, results: [String: HarnessActionResult.Outcome] = [:],
        runs: @escaping @MainActor (String, [String: JSONValue]?) -> Void = { _, _ in },
        notes: @escaping @MainActor (String, SessionUndo?) -> Void = { _, _ in },
        held: @escaping @MainActor (String, String) -> Void = { _, _ in }
    ) -> PrimitiveContext {
        var context = PrimitiveContext(
            run: { action, _, args in
                runs(action, args)
                return HarnessActionResult(result: results[action] ?? .applied)
            },
            openScreen: { _ in }, open: { _ in }, showTerminal: {}
        )
        context.working = { working }
        context.agent = "Auggie"
        context.note = { text, _, undo in notes(text, undo) }
        context.runAfterTurn = { action, _, _, note in held(action, note) }
        return context
    }

    private func modelChange(_ value: String) -> GatedChange {
        GatedChange(
            action: "set_model", target: nil, args: ["id": .string(value.lowercased())], confirm: relaunch,
            setting: "Model", value: value, previous: ("Opus", ["id": .string("opus")])
        )
    }

    @Test func anIdleRelaunchNotesTheRestartWithUndoBack() async {
        var runs: [(String, [String: JSONValue]?)] = []
        var notes: [(String, SessionUndo?)] = []
        let context = context(working: false, results: ["set_model": .relaunched],
                              runs: { runs.append(($0, $1)) }, notes: { notes.append(($0, $1)) })
        let actions = PrimitiveActions()
        var result: HarnessActionResult?
        await actions.change(modelChange("Sonnet"), context: context) { result = $0 }
        #expect(actions.asking == nil)
        #expect(result?.result == .relaunched)
        #expect(notes.first?.0 == PrimitiveCopy.restartedWith("Sonnet"))
        let undo = notes.first?.1
        #expect(undo?.title == PrimitiveCopy.backTo("Opus"))
        await undo?.run()
        #expect(runs.map(\.0) == ["set_model", "set_model"])
        #expect(runs.last?.1 == ["id": .string("opus")])
        // Undoing offers no further Undo.
        #expect(notes.last?.0 == PrimitiveCopy.restartedWith("Opus"))
        #expect(notes.last?.1 == nil)
    }

    @Test func aRelaunchWhileWorkingWaitsForTheDialog() async {
        var runs: [String] = []
        var held: [(String, String)] = []
        let context = context(working: true, results: ["set_model": .relaunched],
                              runs: { action, _ in runs.append(action) }, held: { held.append(($0, $1)) })
        let actions = PrimitiveActions()
        var results: [HarnessActionResult?] = []
        await actions.change(modelChange("Sonnet"), context: context) { results.append($0) }
        #expect(runs.isEmpty)
        guard let request = actions.asking else {
            Issue.record("expected a dialog")
            return
        }
        #expect(request.dialog.offersAfterReply)
        // After this reply: held for the turn's end, and the picker may close.
        request.afterReply?()
        #expect(held.map(\.0) == ["set_model"])
        #expect(held.first?.1 == PrimitiveCopy.restartedWith("Sonnet"))
        #expect(results.last??.result == .queued)
        #expect(runs.isEmpty)
        // Restart now runs it.
        await request.run()
        #expect(runs == ["set_model"])
    }

    @Test func anAppliedChangeNotesTheNewValue() async {
        var notes: [String] = []
        let context = context(working: false, results: ["set_effort": .typed], notes: { text, _ in notes.append(text) })
        let actions = PrimitiveActions()
        let change = GatedChange(action: "set_effort", args: ["value": .string("high")], confirm: nil, setting: "Effort", value: "high")
        await actions.change(change, context: context)
        #expect(notes == [PrimitiveCopy.setWaiting("Effort", "high", agent: "Auggie")])
    }

    @Test func triggerOffersInstructionsOnlyWhereTheComposerIs() async {
        var context = context(working: false)
        context.instructionsLine = { $0 == "compact" ? "/compact " : nil }
        let ref = HarnessActionRef(id: "compact", label: "Compact", confirm: "Compact the conversation now?")
        let actions = PrimitiveActions()
        await actions.trigger(ref, context: context)
        #expect(actions.asking?.dialog.instructions == nil)
        actions.asking = nil
        var composed: [String] = []
        context.compose = { composed.append($0) }
        await actions.trigger(ref, context: context)
        #expect(actions.asking?.dialog.instructions == "/compact ")
        actions.asking?.instructions?("/compact ")
        #expect(composed == ["/compact "])
    }

    @Test func aControlsOutcomeShowsOnItsOwnLabel() async {
        let context = context(working: false, results: ["restart": .relaunched])
        let actions = PrimitiveActions()
        await actions.trigger(HarnessActionRef(id: "restart", label: "Restart"), context: context)
        #expect(actions.outcome("restart")?.kind == .restarted)
        #expect(actions.feedback == nil)
        #expect(PrimitiveFeedback.shortTitle(actions.outcome("restart")!) == PrimitiveCopy.restartedShort)
    }
}
