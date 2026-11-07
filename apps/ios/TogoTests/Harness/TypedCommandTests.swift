import Foundation
import Testing
import TogoKit
@testable import Togo

/// A command typed in the composer and sent does what picking it in the palette does; "/xyz"
/// the agent does not list is held with a hint; a sent command reads as "Ran /compact".
@MainActor struct TypedCommandTests {
    private let model = HarnessCompletion(
        insert: "/model ", title: "/model", detail: "Switch model", kind: .choice, action: "set_model",
        choices: [.init(id: "opus", label: "Opus", selected: true), .init(id: "sonnet", label: "Sonnet")]
    )
    private let effort = HarnessCompletion(
        insert: "/effort ", title: "/effort", hint: "[level]", screen: "effort", kind: .choice, action: "set_effort",
        choices: [.init(id: "low", label: "low"), .init(id: "high", label: "high", selected: true)]
    )
    private let resume = HarnessCompletion(insert: "/resume ", title: "/resume", screen: "resume", kind: .picker)
    private let add = HarnessCompletion(insert: "/add ", title: "/add", hint: "<file>", kind: .args)
    private let compact = HarnessCompletion(insert: "/compact ", title: "/compact", hint: "[instructions]", kind: .run)
    private let clear = HarnessCompletion(insert: "/clear ", title: "/clear", detail: "Start a new conversation",
                                          kind: .run, role: .destructive)
    private let tools = HarnessCompletion(insert: "/tools ", title: "/tools", kind: .terminal)
    private let stats = HarnessCompletion(insert: "/stats ", title: "/stats", kind: .run, opens: "status")

    private var all: [HarnessCompletion] { [model, effort, resume, add, compact, clear, tools, stats] }

    @MainActor final class Session {
        var commands: [HarnessCompletion] = []
        var caps: Set<String> = ["commands"]
        private(set) var sends: [String] = []
        private(set) var runs: [(String, [String: JSONValue]?)] = []
        private(set) var completes = 0
        var terminal = 0
        var mission = 0

        func routes() -> CommandLineModel.Routes {
            var routes = CommandLineModel.Routes(
                has: { self.caps.contains($0) }, working: { false }, mode: { "default" }, modes: { [] },
                queued: { [] }, scope: { "typed-tests" },
                complete: { _, query in
                    self.completes += 1
                    return CommandLineLogic.filter(self.commands, query: query)
                },
                send: { text, _ in
                    self.sends.append(text)
                    return nil
                },
                interrupt: { HarnessActionResult(result: .applied) },
                run: { action, _, args in
                    self.runs.append((action, args))
                    return HarnessActionResult(result: .applied)
                }
            )
            routes.showTerminal = { self.terminal += 1 }
            routes.showMission = { self.mission += 1 }
            routes.agent = { "OMP" }
            return routes
        }
    }

    private func make(_ session: Session) -> CommandLineModel {
        session.commands = session.commands.isEmpty ? all : session.commands
        let defaults = UserDefaults(suiteName: "typed-tests-\(UUID().uuidString)")!
        return CommandLineModel(store: .preview(), memory: CommandMemory(defaults: defaults), routes: session.routes())
    }

    private func send(_ text: String, _ session: Session) async -> CommandLineModel {
        let composer = make(session)
        composer.draft = text
        await composer.submit()
        return composer
    }

    // MARK: Classifying

    @Test func aDraftIsTextACommandOrUnknown() {
        #expect(CommandLineLogic.typed("fix the test", commands: all) == .text)
        #expect(CommandLineLogic.typed("/model", commands: all) == .command(model, args: ""))
        #expect(CommandLineLogic.typed("/MODEL sonnet", commands: all) == .command(model, args: "sonnet"))
        #expect(CommandLineLogic.typed("/compact focus on tests\n", commands: all) == .command(compact, args: "focus on tests"))
        #expect(CommandLineLogic.typed("/xyz", commands: all) == .unknown("/xyz"))
        // Paths and lone slashes are words.
        #expect(CommandLineLogic.typed("/Users/me/a.go is broken", commands: all) == .text)
        #expect(CommandLineLogic.typed("/", commands: all) == .text)
        #expect(CommandLineLogic.typed("// note", commands: all) == .text)
    }

    // MARK: Routing on Send

    @Test func aChoiceOpensItsMenuAndSendsNothing() async {
        let session = Session()
        let composer = await send("/model", session)
        #expect(session.sends.isEmpty)
        #expect(composer.paletteOpen)
        #expect(composer.paletteSeed == "/model")
        #expect(composer.draft.isEmpty)
        // Nothing went, so the chat has no live line to show.
        #expect(composer.store.liveWork == nil)
    }

    @Test func aChoiceWithAScreenOpensTheScreen() async {
        let session = Session()
        let composer = await send("/effort", session)
        #expect(session.sends.isEmpty)
        #expect(composer.presentedScreen == "effort")
        #expect(!composer.paletteOpen)
    }

    @Test func aChoiceWithItsValuePicksIt() async {
        let session = Session()
        let composer = await send("/model sonnet", session)
        #expect(session.sends.isEmpty)
        #expect(session.runs.first?.0 == "set_model")
        #expect(session.runs.first?.1?["id"] == .string("sonnet"))
        #expect(composer.draft.isEmpty)
    }

    @Test func aListPushesItsScreen() async {
        let session = Session()
        let composer = await send("/resume", session)
        #expect(session.sends.isEmpty)
        #expect(composer.presentedScreen == "resume")
    }

    @Test func argsWithTheirWordsGoAsACommand() async {
        let session = Session()
        _ = await send("/add src/a.go", session)
        #expect(session.sends == ["/add src/a.go"])
    }

    @Test func argsWithoutWordsWaitInTheField() async {
        let session = Session()
        let composer = await send("/add", session)
        #expect(session.sends.isEmpty)
        #expect(composer.draft == "/add @")
        #expect(!composer.canSend)
    }

    @Test func aRunRunsAndADestructiveOneAsks() async {
        let session = Session()
        _ = await send("/compact", session)
        #expect(session.sends == ["/compact"])
        let wipe = await send("/clear", session)
        #expect(session.sends == ["/compact"])
        #expect(wipe.gate.asking?.dialog.destructive == true)
        await wipe.gate.asking?.run()
        #expect(session.sends == ["/compact", "/clear"])
    }

    @Test func aRunWithWordsGoesTyped() async {
        let session = Session()
        _ = await send("/compact focus on tests", session)
        #expect(session.sends == ["/compact focus on tests"])
        #expect(session.terminal == 0)
    }

    @Test func aTerminalCommandSwitchesToTheTerminal() async {
        let session = Session()
        _ = await send("/tools", session)
        #expect(session.terminal == 1)
        #expect(session.sends == ["/tools"])
    }

    @Test func anOpensCommandGoesToMissionControl() async {
        let session = Session()
        _ = await send("/stats", session)
        #expect(session.mission == 1)
        #expect(session.sends.isEmpty)
    }

    @Test func anUnknownCommandIsHeldWithAHint() async {
        let session = Session()
        let composer = await send("/xyz", session)
        #expect(session.sends.isEmpty)
        #expect(composer.draft == "/xyz")
        #expect(composer.notice == ComposerCopy.notACommand("OMP"))
        #expect(composer.noticeAction == .sendAsText)
        await composer.submit(asText: true)
        #expect(session.sends == ["/xyz"])
    }

    @Test func wordsAndPathsGoAsTheyAre() async {
        let session = Session()
        _ = await send("/Users/me/a.go is broken", session)
        _ = await send("fix it", session)
        #expect(session.sends == ["/Users/me/a.go is broken", "fix it"])
    }

    @Test func withoutCommandsTheLineGoesAsTyped() async {
        let session = Session()
        session.caps = []
        _ = await send("/model", session)
        #expect(session.sends == ["/model"])
        #expect(session.completes == 0)
    }

    @Test func returnOnAChoiceInTheMenuOpensIt() async {
        let session = Session()
        let composer = make(session)
        composer.draft = "/model"
        await composer.draftChanged()
        await composer.returnPressed()
        #expect(session.sends.isEmpty)
        #expect(composer.paletteOpen)
    }

    // MARK: The chat

    @Test func aSentCommandReadsAsWhatRan() {
        #expect(CommandEcho.isCommand("/compact", known: all))
        #expect(CommandEcho.isCommand("/compact focus on tests", known: all))
        #expect(!CommandEcho.isCommand("/xyz", known: all))
        #expect(!CommandEcho.isCommand("fix /compact", known: all))
        #expect(!CommandEcho.isCommand("/compact\nand more", known: all))
        // Before the list is read, the shape decides.
        #expect(CommandEcho.isCommand("/compact", known: []))
        #expect(!CommandEcho.isCommand("/Users/me", known: []))
        #expect(ChatCopy.ran("/compact") == "Ran /compact")
    }
}
