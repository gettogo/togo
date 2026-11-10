import Foundation
import Testing
import TogoKit
@testable import Togo

/// The palette and the inline `/` menu dispatch every command by kind, the same way.
@MainActor struct CommandKindTests {
    private let effort = HarnessCompletion(
        insert: "/effort ", title: "/effort", detail: "Set reasoning effort", hint: "[level]", screen: "effort",
        kind: .choice, action: "set_effort",
        choices: [.init(id: "low", label: "low"), .init(id: "high", label: "high", selected: true), .init(id: "max", label: "max")]
    )
    private let resume = HarnessCompletion(insert: "/resume ", title: "/resume", hint: "[session]", screen: "resume", kind: .picker)
    private let add = HarnessCompletion(insert: "/add ", title: "/add", hint: "<file>", kind: .args)
    private let compact = HarnessCompletion(insert: "/compact ", title: "/compact", detail: "Summarise the conversation",
                                            hint: "[instructions]", kind: .run, action: "compact")
    private let clear = HarnessCompletion(insert: "/clear ", title: "/clear", detail: "Start a new conversation", kind: .run, role: .destructive)
    private let tools = HarnessCompletion(insert: "/tools ", title: "/tools", kind: .terminal)
    private let stats = HarnessCompletion(insert: "/stats ", title: "/stats", kind: .run, opens: "status")
    private let settings = HarnessCompletion(insert: "/settings ", title: "/settings", kind: .run, opens: "settings")

    @Test func eachKindDispatches() {
        #expect(CommandKind.dispatch(effort) == .choose(effort.choices!, action: "set_effort"))
        #expect(CommandKind.dispatch(resume) == .push(screen: "resume"))
        #expect(CommandKind.dispatch(add) == .compose(line: "/add ", files: true))
        #expect(CommandKind.dispatch(tools) == .terminal(line: "/tools"))
        #expect(CommandKind.dispatch(stats) == .open(.mission("status")))
        #expect(CommandKind.dispatch(settings) == .open(.screen("settings")))
        guard case .run(let run) = CommandKind.dispatch(compact) else {
            Issue.record("compact runs")
            return
        }
        #expect(run.action == "compact")
        #expect(!run.destructive)
        guard case .run(let wipe) = CommandKind.dispatch(clear) else {
            Issue.record("clear runs")
            return
        }
        #expect(wipe.destructive)
    }

    /// A pick never sends a command that may take words: it goes to the field first. Choices,
    /// lists and native homes are as they were.
    @Test func aPickPutsTypedAndRunCommandsInTheField() {
        #expect(CommandKind.picking(compact) == .compose(line: "/compact ", files: false))
        #expect(CommandKind.picking(clear) == .compose(line: "/clear ", files: false))
        #expect(CommandKind.picking(tools) == .compose(line: "/tools ", files: false))
        #expect(CommandKind.picking(add) == .compose(line: "/add ", files: true))
        #expect(CommandKind.picking(effort) == CommandKind.dispatch(effort))
        #expect(CommandKind.picking(resume) == .push(screen: "resume"))
        #expect(CommandKind.picking(stats) == .open(.mission("status")))
        #expect(CommandKind.picking(settings) == .open(.screen("settings")))
        // Only words a command cannot go without hold Send back.
        #expect(CommandKind.needsArgument(add))
        #expect(!CommandKind.needsArgument(compact))
        #expect(!CommandKind.needsArgument(clear))
    }

    @Test func olderHelpersGetADerivedKind() {
        let screen = HarnessCompletion(insert: "/model ", title: "/model", hint: "[model]", screen: "model")
        #expect(CommandKind.kind(of: screen) == .picker)
        #expect(CommandKind.kind(of: HarnessCompletion(insert: "/run ", title: "/run", hint: "<command>")) == .args)
        #expect(CommandKind.kind(of: HarnessCompletion(insert: "/init", title: "/init")) == .run)
        #expect(CommandKind.kind(of: HarnessCompletion(insert: "/x", title: "/x", opens: "changes")) == .run)
        // A choice without its values falls back to its screen.
        let bare = HarnessCompletion(insert: "/effort ", title: "/effort", screen: "effort", kind: .choice)
        #expect(CommandKind.dispatch(bare) == .push(screen: "effort"))
    }

    @Test func argsWaitForTheirArgument() {
        #expect(!CommandKind.argumentGiven(draft: "/add", command: "/add "))
        #expect(!CommandKind.argumentGiven(draft: "/add ", command: "/add "))
        #expect(!CommandKind.argumentGiven(draft: "/add @", command: "/add "))
        #expect(CommandKind.argumentGiven(draft: "/add src/a.go", command: "/add "))
        // Something else in the field is not held back.
        #expect(CommandKind.argumentGiven(draft: "fix the test", command: "/add "))
        #expect(CommandKind.argumentGiven(draft: "/addendum", command: "/add "))
    }

    @Test func glyphsSayWhatATapDoes() {
        #expect(CommandKind.glyph(effort) == "chevron.up.chevron.down")
        #expect(CommandKind.glyph(resume) == "chevron.forward")
        #expect(CommandKind.glyph(add) == "text.cursor")
        #expect(CommandKind.glyph(tools) == "terminal")
        #expect(CommandKind.glyph(compact) == nil)
    }

    @Test func theCurrentChoiceFollowsWhatTheHarnessReports() {
        let choices = effort.choices!
        #expect(CommandKind.current(choices)?.id == "high")
        #expect(CommandKind.current(choices, reported: "max")?.id == "max")
        #expect(CommandKind.current(choices, reported: "LOW")?.id == "low")
        #expect(CommandKind.current(choices, reported: "unknown")?.id == "high")
    }

    // MARK: Through the model

    @MainActor final class Session {
        var working = false
        var commands: [HarnessCompletion] = []
        var runResult = HarnessActionResult(result: .applied)
        private(set) var sends: [String] = []
        private(set) var runs: [(String, [String: JSONValue]?)] = []
        var terminal = 0
        var mission = 0

        func routes() -> CommandLineModel.Routes {
            var routes = CommandLineModel.Routes(
                has: { _ in true }, working: { self.working }, mode: { "default" },
                modes: { [.init(id: "default", label: "Manual"), .init(id: "plan", label: "Plan")] },
                queued: { [] }, scope: { "kind-tests" },
                complete: { _, query in CommandLineLogic.filter(self.commands, query: query) },
                send: { text, _ in
                    self.sends.append(text)
                    return nil
                },
                interrupt: { HarnessActionResult(result: .applied) },
                run: { action, _, args in
                    self.runs.append((action, args))
                    return self.runResult
                }
            )
            routes.showTerminal = { self.terminal += 1 }
            routes.showMission = { self.mission += 1 }
            routes.agent = { "Claude" }
            return routes
        }
    }

    private func make(_ session: Session) -> CommandLineModel {
        let defaults = UserDefaults(suiteName: "kind-tests-\(UUID().uuidString)")!
        return CommandLineModel(store: .preview(), memory: CommandMemory(defaults: defaults), routes: session.routes())
    }

    @Test func aTerminalCommandGoesToTheFieldThenSwitchesToTheTerminalAsItSends() async {
        let session = Session()
        session.commands = [tools]
        let model = make(session)
        model.paletteOpen = true
        let outcome = await model.perform(tools)
        #expect(outcome == .close)
        #expect(!model.paletteOpen)
        #expect(model.draft == "/tools ")
        #expect(session.sends.isEmpty)
        await model.submit()
        #expect(session.terminal == 1)
        #expect(session.sends == ["/tools"])
    }

    @Test func opensGoesToItsNativeHome() async {
        let session = Session()
        let model = make(session)
        #expect(await model.perform(stats) == .close)
        #expect(session.mission == 1)
        #expect(await model.perform(settings) == .push("settings"))
        #expect(session.sends.isEmpty)
    }

    @Test func argsGoToTheFieldAndWait() async {
        let session = Session()
        let model = make(session)
        #expect(await model.perform(add) == .close)
        #expect(model.draft == "/add @")
        #expect(!model.canSend)
        await model.submit()
        #expect(session.sends.isEmpty)
    }

    @Test func aDestructiveCommandAsksFirst() async {
        let session = Session()
        session.commands = [clear]
        let model = make(session)
        #expect(await model.perform(clear) == .close)
        #expect(model.draft == "/clear ")
        #expect(model.gate.asking == nil)
        await model.submit()
        #expect(session.sends.isEmpty)
        let request = model.gate.asking
        #expect(request?.dialog.title == PrimitiveCopy.asking("Start a new conversation"))
        #expect(request?.dialog.destructive == true)
        await request?.run()
        #expect(session.sends == ["/clear"])
    }

    /// The owner's report: /compact takes words (omp's soft, remote, snapcompact and a focus),
    /// so a pick puts it in the field rather than asking in a dialog. Sent with words it goes
    /// as typed; sent bare it runs its action, without a dialog either.
    @Test func compactGoesToTheFieldWithoutADialog() async {
        let session = Session()
        session.commands = [compact]
        let model = make(session)
        #expect(await model.perform(compact) == .close)
        #expect(model.gate.asking == nil)
        #expect(model.draft == "/compact ")
        #expect(model.canSend)
        model.draft = "/compact snapcompact"
        await model.submit()
        #expect(session.sends == ["/compact snapcompact"])
        #expect(session.runs.isEmpty)

        model.draft = "/compact"
        await model.submit()
        #expect(model.gate.asking == nil)
        #expect(session.runs.map(\.0) == ["compact"])
    }

    @Test func aPlainRunWaitsInTheFieldForSend() async {
        let session = Session()
        let review = HarnessCompletion(insert: "/review", title: "/review", kind: .run)
        session.commands = [review]
        let model = make(session)
        #expect(await model.perform(review) == .close)
        #expect(model.draft == "/review ")
        #expect(session.sends.isEmpty)
        await model.submit()
        #expect(session.sends == ["/review"])
        #expect(model.gate.asking == nil)
    }

    @Test func aChoiceAppliesAndClosesThePalette() async {
        let session = Session()
        session.runResult = HarnessActionResult(result: .typed)
        let model = make(session)
        model.paletteOpen = true
        await model.pick(effort.choices![2], of: effort, setting: "Effort")
        #expect(session.runs.first?.0 == "set_effort")
        #expect(session.runs.first?.1 == ["id": .string("max"), "value": .string("max")])
        #expect(!model.paletteOpen)
        #expect(model.store.notes.last?.text == PrimitiveCopy.setWaiting("Effort", "max", agent: "Claude"))
    }

    @Test func pickingTheCurrentValueRunsNothing() async {
        let session = Session()
        let model = make(session)
        await model.pick(effort.choices![1], of: effort, setting: "Effort")
        #expect(session.runs.isEmpty)
    }

    @Test func aFailedChoiceSaysWhyAndKeepsThePalette() async {
        let session = Session()
        session.runResult = HarnessActionResult(result: .failed, message: "Effort is read only")
        let model = make(session)
        model.paletteOpen = true
        await model.pick(effort.choices![0], of: effort, setting: "Effort")
        #expect(model.paletteOpen)
        #expect(model.notice == "Effort is read only")
    }

    @Test func theInlineMenuAndThePaletteAgree() async {
        let session = Session()
        session.commands = [clear, add, tools]
        let model = make(session)
        model.draft = "/clear"
        await model.draftChanged()
        await model.returnPressed()
        // Typed in full: still asks first, as in the palette.
        #expect(session.sends.isEmpty)
        #expect(model.gate.asking?.dialog.destructive == true)
        model.gate.asking = nil

        model.draft = "/tools"
        await model.draftChanged()
        await model.returnPressed()
        #expect(session.sends == ["/tools"])
        #expect(session.terminal == 1)
    }
}
