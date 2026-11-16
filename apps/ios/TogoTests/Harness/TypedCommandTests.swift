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
        var nativeCommands: [String] = []
        var nativeFailure = false
        var beforeComplete: (@MainActor () async -> Void)?
        var terminal = 0
        var mission = 0

        func routes() -> CommandLineModel.Routes {
            var routes = CommandLineModel.Routes(
                has: { self.caps.contains($0) }, working: { false }, mode: { "default" }, modes: { [] },
                queued: { [] }, scope: { "typed-tests" },
                complete: { _, query in
                    self.completes += 1
                    let commands = CommandLineLogic.filter(self.commands, query: query)
                    await self.beforeComplete?()
                    return commands
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
            routes.nativeCommand = { text, _ in
                self.nativeCommands.append(text)
                if self.nativeFailure { throw ChatSurfaceInteraction.Failure(message: "Command not confirmed") }
            }
            routes.showTerminal = { self.terminal += 1 }
            routes.showMission = { self.mission += 1 }
            routes.agent = { "OMP" }
            return routes
        }
    }

    @Test func aliasesResolveThroughTheSameNativeScreenAndSearchList() async {
        let session = Session()
        var command = resume
        command.native = NativeCommandMetadata(opens: "picker", args: "optional", aliases: ["continue", "/sessions"])
        session.commands = [command]
        let composer = make(session)
        composer.draft = "/continue"
        await composer.submit()
        #expect(composer.presentedScreen == "resume")
        #expect(session.sends.isEmpty)
        #expect(CommandLineLogic.filter([command], query: "cont") == [command])
        #expect(CommandLineLogic.matchingAlias(command, query: "cont") == "/continue")
        #expect(CommandLineLogic.matchingAlias(command, query: "resume") == nil)
        #expect(CommandLineLogic.matchingAlias(command, query: "") == nil)
        #expect(CommandLineLogic.filter([command], query: "sessions") == [command])
        #expect(CommandLineLogic.typed("/sessions more words", commands: [command]) == .command(command, args: "more words"))
        #expect(CommandLineLogic.typed("/continue/path", commands: [command]) == .text)
    }

    @Test func liveScreenIntentUsesOriginalWorkflowInsteadOfTheLegacyScreen() async {
        let session = Session()
        session.caps.insert(HarnessCapability.surface)
        session.commands = [.init(insert: "/model", title: "/model", screen: "model", kind: .picker,
            native: .init(opens: "screen", args: "optional", screenId: "model"))]
        let composer = make(session)
        composer.draft = "/model"
        await composer.submit()
        #expect(session.nativeCommands == ["/model"])
        #expect(composer.presentedScreen == nil)
        #expect(session.sends.isEmpty)
        _ = await composer.perform(session.commands[0])
        #expect(session.nativeCommands == ["/model", "/model"])
    }

    @Test(arguments: [false, true])
    func terminalReaderKeepsTheAdapterModelScreen(hasExplicitKind: Bool) async {
        let session = Session()
        session.caps.insert(HarnessCapability.terminalNative)
        let command = HarnessCompletion(insert: "/model", title: "/model", screen: "model",
            kind: hasExplicitKind ? .picker : nil,
            native: .init(opens: "screen", args: "none", screenId: "model"))
        session.commands = [command]
        let composer = make(session)
        composer.draft = "/model"
        await composer.submit()
        #expect(composer.presentedScreen == "model")
        #expect(session.nativeCommands.isEmpty && session.sends.isEmpty && session.runs.isEmpty)
        #expect(await composer.perform(command) == .push("model"))
        #expect(session.nativeCommands.isEmpty && session.sends.isEmpty)
    }

    @Test func anUnknownCatalogCommandWaitsForSendAfterAPaletteTap() async {
        let session = Session()
        session.caps.insert(HarnessCapability.terminalNative)
        session.commands = [.init(insert: "/permission", title: "/permission", kind: .terminal,
            native: .init(opens: "unknown"))]
        let composer = make(session)
        _ = await composer.perform(session.commands[0])
        #expect(session.nativeCommands.isEmpty && session.sends.isEmpty)
        #expect(composer.draft.trimmingCharacters(in: .whitespaces) == "/permission")
        await composer.submit()
        #expect(session.nativeCommands == ["/permission"])
        #expect(session.terminal == 0 && session.sends.isEmpty)
    }

    @Test func anUnknownExitCommandIsNeverExecutedByAPaletteTap() async {
        let session = Session()
        session.caps.insert(HarnessCapability.terminalNative)
        session.commands = [.init(insert: "/exit", title: "/exit", kind: .run, native: .init(opens: "unknown"))]
        let composer = make(session)
        _ = await composer.perform(session.commands[0])
        #expect(session.nativeCommands.isEmpty && session.sends.isEmpty && session.runs.isEmpty)
        #expect(composer.draft.trimmingCharacters(in: .whitespaces) == "/exit")
    }

    @Test func aRicherSourceKeepsDestructiveConfirmationWhenTerminalReadingIsAvailable() async {
        let session = Session()
        session.caps.formUnion([HarnessCapability.surface, HarnessCapability.terminalNative])
        session.commands = [.init(insert: "/delete", title: "/delete", kind: .run, role: .destructive,
            native: .init(opens: "action", args: "none", destructive: true))]
        let composer = make(session)
        _ = await composer.perform(session.commands[0], typed: true)
        #expect(composer.gate.asking != nil)
        #expect(session.nativeCommands.isEmpty && session.sends.isEmpty)
    }

    @Test func lateCatalogReadCannotReplaceAChangedCatalog() async throws {
        let session = Session()
        session.commands = [tools]
        let composer = make(session)
        let gate = AsyncStream<Void>.makeStream()
        var reading = false
        session.beforeComplete = {
            reading = true
            var iterator = gate.stream.makeAsyncIterator()
            _ = await iterator.next()
        }
        let old = Task { await composer.loadPalette() }
        let deadline = ContinuousClock.now + .seconds(2)
        while !reading, ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(5)) }
        try #require(reading)
        composer.store.commandRevision = UUID()
        session.commands = [resume]
        session.beforeComplete = nil
        await composer.loadPalette()
        gate.continuation.yield(())
        await old.value
        gate.continuation.finish()
        #expect(composer.palette == [resume])
        #expect(composer.store.commandList == [resume])
    }

    private func make(_ session: Session) -> CommandLineModel {
        session.commands = session.commands.isEmpty ? all : session.commands
        let defaults = UserDefaults(suiteName: "typed-tests-\(UUID().uuidString)")!
        return CommandLineModel(store: .preview(), memory: CommandMemory(defaults: defaults), routes: session.routes())
    }

    @Test func nativeOptionalInputOverridesLegacyRequiredHintAndLeavesComposerUsable() async {
        let session = Session()
        session.caps.insert(HarnessCapability.terminalNative)
        let command = HarnessCompletion(insert: "/goal ", title: "/goal", hint: "<objective>", kind: .args,
            native: .init(opens: "input", args: "optional"))
        session.commands = [command]
        let composer = make(session)
        #expect(!CommandKind.needsArgument(command))
        composer.draft = "/goal"
        await composer.submit()
        #expect(session.nativeCommands == ["/goal"])
        #expect(composer.draft.isEmpty)
        composer.draft = "Continue here"
        #expect(composer.canSend)
        await composer.submit()
        #expect(session.sends == ["Continue here"])
        _ = await composer.perform(command)
        #expect(session.nativeCommands == ["/goal", "/goal"])
        composer.draft = "Another message"
        #expect(composer.canSend)
        var required = command
        required.native?.args = "required"
        #expect(CommandKind.needsArgument(required))
    }

    @Test func inlineMenuOpensOptionalNativeInputInsteadOfLeavingAnArgumentPlaceholder() async throws {
        let session = Session()
        session.caps.insert(HarnessCapability.terminalNative)
        let command = HarnessCompletion(insert: "/goal ", title: "/goal", hint: "<objective>", kind: .args,
            native: .init(opens: "input", args: "optional"))
        session.commands = [command]
        let composer = make(session)
        composer.draft = "/go"
        await composer.draftChanged()
        composer.choose(command)
        let deadline = ContinuousClock.now + .seconds(2)
        while session.nativeCommands.isEmpty, ContinuousClock.now < deadline { await Task.yield() }
        #expect(session.nativeCommands == ["/goal"])
        #expect(composer.draft.isEmpty)
        composer.draft = "Next message"
        #expect(composer.canSend)
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

    @Test func destructiveNativeActionWithArgumentsStillAsks() async {
        let session = Session()
        let command = HarnessCompletion(insert: "/memory ", title: "/memory", kind: .args, role: .destructive,
            native: .init(opens: "text", opensWithArgs: "action", args: "optional", destructive: true))
        session.commands = [command]
        let composer = make(session)
        composer.draft = "/memory clear"
        await composer.submit()
        #expect(session.sends.isEmpty)
        #expect(composer.gate.asking?.dialog.destructive == true)
        await composer.gate.asking?.run()
        #expect(session.sends == ["/memory clear"])
    }

    @Test func extraArgumentsThatBecomeAPromptDoNotAskToDelete() async {
        let session = Session()
        session.commands = [HarnessCompletion(insert: "/delete ", title: "/delete", kind: .run, role: .destructive,
            native: .init(opens: "action", opensWithArgs: "turn", args: "none", destructive: true))]
        let composer = make(session)
        composer.draft = "/delete explain this command"
        await composer.submit()
        #expect(composer.gate.asking == nil)
        #expect(session.sends == ["/delete explain this command"])
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

    /// The list Togo has may not hold every command (personal commands, skills, a list cut
    /// short): the harness judges a "/word" it does not list, so it is sent, never refused.
    @Test func anUnlistedCommandIsSentForTheHarnessToJudge() async {
        let session = Session()
        let composer = await send("/xyz", session)
        #expect(session.sends == ["/xyz"])
        #expect(composer.draft.isEmpty)
        #expect(composer.notice == nil)
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

    @Test func terminalCommandsUseTheOriginalNativeRouteWhenAvailable() async {
        let session = Session()
        session.caps.insert(HarnessCapability.surface)
        let composer = await send("/tools", session)
        #expect(session.nativeCommands == ["/tools"])
        #expect(session.sends.isEmpty)
        #expect(session.terminal == 0)
        #expect(composer.draft.isEmpty)
    }

    @Test func unlistedExtensionCommandsUseTheSameNativeRouteWithLiteralArguments() async {
        let session = Session()
        session.caps.insert(HarnessCapability.surface)
        _ = await send("/togo-form <div> 100%! 🧑🏽‍💻", session)
        #expect(session.nativeCommands == ["/togo-form <div> 100%! 🧑🏽‍💻"])
        #expect(session.sends.isEmpty)
        #expect(session.terminal == 0)
    }

    @Test func nativeCommandFailureKeepsTextAndDoesNotRetryThroughTheTerminal() async {
        let session = Session()
        session.caps.insert(HarnessCapability.surface)
        session.nativeFailure = true
        let composer = await send("/tools", session)
        #expect(session.nativeCommands == ["/tools"])
        #expect(session.sends.isEmpty)
        #expect(session.terminal == 0)
        #expect(composer.draft == "/tools")
        #expect(composer.notice != nil)
    }

    @Test func nativeAvailabilityDoesNotReplaceAPINativeChoicesOrOrdinaryPrompts() async {
        let session = Session()
        session.caps.insert(HarnessCapability.surface)
        _ = await send("/model sonnet", session)
        _ = await send("fix the test", session)
        _ = await send("/Users/me/a.go is broken", session)
        #expect(session.nativeCommands.isEmpty)
        #expect(session.runs.first?.0 == "set_model")
        #expect(session.sends == ["fix the test", "/Users/me/a.go is broken"])
    }

    @Test func nativeTerminalCommandWorksFromReturnAndPaletteDispatch() async {
        let session = Session()
        session.caps.insert(HarnessCapability.surface)
        let composer = make(session)
        composer.draft = "/tools"
        await composer.draftChanged()
        await composer.returnPressed()
        await composer.perform(tools, typed: true)
        #expect(session.nativeCommands == ["/tools", "/tools"])
        #expect(session.sends.isEmpty)
        #expect(session.terminal == 0)
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
