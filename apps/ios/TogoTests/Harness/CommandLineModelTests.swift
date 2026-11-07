import Foundation
import Testing
import TogoKit
@testable import Togo

/// The smart command line: which menu the draft calls for, how completions land, and how
/// prompts, modes, interrupts and the queue reach the session.
@MainActor struct CommandLineModelTests {
    /// Stands in for the session: records every call and answers from the samples.
    @MainActor final class Session {
        var working = false
        var mode = "default"
        var caps: Set<String> = ["commands", "files", "mode", "interrupt", "send.queue"]
        var commands = HarnessSamples.commands
        var completeError: (any Error)?
        var sendErrors: [any Error] = []
        var runResult = HarnessActionResult(result: .applied)
        private(set) var completes: [(HarnessCompleteParams.Kind, String)] = []
        private(set) var sends: [(String, HarnessSendParams.Mode)] = []
        private(set) var runs: [(String, String?, [String: JSONValue]?)] = []
        var interruptResult = HarnessActionResult(result: .applied)
        private(set) var interrupts = 0

        func routes() -> CommandLineModel.Routes {
            CommandLineModel.Routes(
                has: { self.caps.contains($0) },
                working: { self.working },
                mode: { self.mode },
                modes: { HarnessSamples.descriptor.modes },
                queued: { [] },
                scope: { "test" },
                complete: { kind, query in
                    self.completes.append((kind, query))
                    if let error = self.completeError { throw error }
                    return CommandLineLogic.filter(kind == .command ? self.commands : HarnessSamples.files, query: query)
                },
                send: { text, mode in
                    self.sends.append((text, mode))
                    if !self.sendErrors.isEmpty { throw self.sendErrors.removeFirst() }
                    return HarnessItem(id: "q\(self.sends.count)", seq: 1, at: HarnessSamples.at,
                                       body: .user(.init(text: text, queued: mode == .queue, source: "phone")))
                },
                interrupt: {
                    self.interrupts += 1
                    return self.interruptResult
                },
                run: { action, target, args in
                    self.runs.append((action, target, args))
                    return self.runResult
                }
            )
        }
    }

    struct Offline: Error {}

    private func make(_ session: Session = Session()) -> CommandLineModel {
        let defaults = UserDefaults(suiteName: "composer-tests-\(UUID().uuidString)")!
        return CommandLineModel(store: .preview(), memory: CommandMemory(defaults: defaults), routes: session.routes())
    }

    // MARK: Menu detection

    @Test func slashAtTheStartAsksForCommands() {
        #expect(CommandLineLogic.trigger(in: "/") == .command(""))
        #expect(CommandLineLogic.trigger(in: "/mo") == .command("mo"))
        #expect(CommandLineLogic.trigger(in: "/model sonnet") == nil)
        #expect(CommandLineLogic.trigger(in: "run /model") == nil)
    }

    @Test func atWordAsksForFiles() {
        #expect(CommandLineLogic.trigger(in: "@") == .file("", at: 0))
        #expect(CommandLineLogic.trigger(in: "fix @pa") == .file("pa", at: 4))
        #expect(CommandLineLogic.trigger(in: "/compact @src/da") == .file("src/da", at: 9))
        #expect(CommandLineLogic.trigger(in: "mail me@example.com") == nil)
        #expect(CommandLineLogic.trigger(in: "fix @parse.go and") == nil)
    }

    @Test func filterRanksPrefixFirstAndKeepsOrder() {
        let items = CommandLineLogic.filter(HarnessSamples.commands, query: "re")
        #expect(Array(items.map(\.title).prefix(2)) == ["/resume", "/review"])
        #expect(!items.contains { $0.title == "/model" })
        #expect(CommandLineLogic.filter(HarnessSamples.commands, query: "").count == HarnessSamples.commands.count)
        #expect(CommandLineLogic.filter(HarnessSamples.commands, query: "zzz").isEmpty)
    }

    @Test func draftChangedShowsCommandsAfterAsking() async {
        let session = Session()
        let model = make(session)
        model.draft = "/mo"
        await model.draftChanged()
        #expect(session.completes.map(\.1) == ["mo"])
        #expect(session.completes.first?.0 == .command)
        #expect(model.menu == .commands([HarnessSamples.commands[2]]))
    }

    @Test func aCancelledKeystrokeNeverAsks() async {
        let session = Session()
        let model = make(session)
        model.draft = "/m"
        let task = Task { await model.draftChanged() }
        task.cancel()
        await task.value
        #expect(session.completes.isEmpty)
    }

    @Test func aLongerQueryFiltersTheHeldListAtOnce() async {
        let session = Session()
        let model = make(session)
        model.draft = "/"
        await model.draftChanged()
        #expect(model.menu.items.count == HarnessSamples.commands.count)
        session.completeError = Offline()
        model.draft = "/rev"
        await model.draftChanged()
        #expect(model.menu.items.map(\.title) == ["/review"])
    }

    @Test func menusFollowCapabilities() async {
        let session = Session()
        session.caps = []
        let model = make(session)
        model.draft = "/"
        await model.draftChanged()
        #expect(model.menu == .none)
        model.draft = "see @pa"
        await model.draftChanged()
        #expect(model.menu == .none)
        #expect(session.completes.isEmpty)
    }

    @Test func fileMenuListsFiles() async {
        let model = make()
        model.draft = "look at @pars"
        await model.draftChanged()
        guard case let .files(items) = model.menu else {
            Issue.record("expected the file menu")
            return
        }
        #expect(items.map(\.title) == ["parse.go", "parse_test.go"])
    }

    // MARK: Completion

    @Test func choosingACommandReplacesTheDraft() {
        let model = make()
        model.draft = "/comp"
        model.choose(HarnessSamples.commands[0])
        #expect(model.draft == "/compact ")
        #expect(model.menu == .none)
    }

    @Test func choosingAFileReplacesTheWord() {
        let model = make()
        model.draft = "explain @pa"
        model.choose(HarnessSamples.files[0])
        #expect(model.draft == "explain @internal/clock/parse.go ")
    }

    @Test func aCompletionWithAScreenOpensIt() {
        let model = make()
        model.draft = "/mod"
        model.choose(HarnessSamples.commands[2])
        #expect(model.presentedScreen == "model")
        #expect(model.draft.isEmpty)
    }

    @Test func returnTakesTheHighlightedCompletionOrSendsWhenTypedInFull() async {
        let session = Session()
        let model = make(session)
        model.draft = "/mod"
        await model.draftChanged()
        await model.returnPressed()
        #expect(model.presentedScreen == "model")
        #expect(session.sends.isEmpty)

        model.draft = "/clear"
        await model.draftChanged()
        await model.returnPressed()
        #expect(session.sends.map(\.0) == ["/clear"])
    }

    @Test func highlightWraps() async {
        let model = make()
        model.draft = "/"
        await model.draftChanged()
        model.moveHighlight(-1)
        #expect(model.highlight == HarnessSamples.commands.count - 1)
        model.moveHighlight(1)
        #expect(model.highlight == 0)
    }

    // MARK: Sending

    @Test func idleSendsNowAndWorkingQueues() async {
        let session = Session()
        let model = make(session)
        model.draft = "fix the test\n"
        await model.submit()
        session.working = true
        model.draft = "also the changelog"
        await model.submit()
        #expect(session.sends.map(\.0) == ["fix the test", "also the changelog"])
        #expect(session.sends.map(\.1) == [.now, .queue])
        #expect(model.draft.isEmpty)
        #expect(model.queued.map(\.id) == ["q2"])
    }

    @Test func busyRefusalQueuesInstead() async {
        let session = Session()
        session.sendErrors = [HelperError.conflict("busy")]
        let model = make(session)
        model.draft = "go on"
        await model.submit()
        #expect(session.sends.map(\.1) == [.now, .queue])
        #expect(model.notice == nil)
    }

    @Test func aFailedSendPutsTheWordsBack() async {
        let session = Session()
        session.sendErrors = [Offline()]
        let model = make(session)
        model.draft = "deploy it"
        await model.submit()
        #expect(model.draft == "deploy it")
        #expect(model.notice == ComposerCopy.notSent)
    }

    /// A palette whose list failed to load says so and loads again on Try again, instead of
    /// spinning for ever.
    @Test func aFailedPaletteCanTryAgain() async {
        let session = Session()
        session.completeError = Offline()
        let model = make(session)
        await model.loadPalette()
        #expect(model.paletteFailed)
        #expect(model.palette.isEmpty)
        session.completeError = nil
        await model.loadPalette()
        #expect(!model.paletteFailed)
        #expect(!model.palette.isEmpty)
    }

    @Test func blankDraftSendsNothing() async {
        let session = Session()
        let model = make(session)
        model.draft = "  \n"
        await model.submit()
        #expect(session.sends.isEmpty)
    }

    @Test func historyRecallsSentLines() async {
        let model = make()
        model.draft = "first"
        await model.submit()
        model.draft = "second"
        await model.submit()
        model.recall(older: true)
        #expect(model.draft == "second")
        model.recall(older: true)
        #expect(model.draft == "first")
        model.recall(older: false)
        #expect(model.draft == "second")
        model.recall(older: false)
        #expect(model.draft.isEmpty)
    }

    @Test func favouritesLeadTheMenu() async {
        let model = make()
        model.toggleFavourite(HarnessSamples.commands[6])
        #expect(model.isFavourite(HarnessSamples.commands[6]))
        model.draft = "/"
        await model.draftChanged()
        #expect(model.menu.items.first?.title == "/init")
    }

    // MARK: Mode, interrupt, queue

    @Test func choosingAModeRunsSetMode() async {
        let session = Session()
        let model = make(session)
        await model.setMode("plan")
        #expect(session.runs.count == 1)
        #expect(session.runs.first?.0 == "set_mode")
        #expect(session.runs.first?.2 == ["id": .string("plan")])
        #expect(model.mode == "plan")
        #expect(model.modeLabel == "Plan")
        // The session reports another mode: it wins.
        session.mode = "acceptEdits"
        #expect(model.mode == "acceptEdits")
    }

    @Test func unknownOrCurrentModeRunsNothing() async {
        let session = Session()
        let model = make(session)
        await model.setMode("default")
        await model.setMode("yolo")
        #expect(session.runs.isEmpty)
    }

    @Test func refusedModeLeavesTheChip() async {
        let session = Session()
        session.runResult = HarnessActionResult(result: .failed, message: "mode is read only")
        let model = make(session)
        await model.setMode("plan")
        #expect(model.mode == "default")
        #expect(model.notice == "mode is read only")
    }

    @Test func interruptShowsOnlyWhileWorking() async {
        let session = Session()
        let model = make(session)
        #expect(!model.canInterrupt)
        session.working = true
        #expect(model.canInterrupt)
        await model.interrupt()
        #expect(session.interrupts == 1)
        #expect(model.notice == nil)
    }

    /// A stop the helper could not finish says why, so Stop never looks dead.
    @Test func interruptThatDidNotStopSaysSo() async {
        let session = Session()
        session.working = true
        session.interruptResult = HarnessActionResult(result: .failed, message: "Still working after 2 presses; stopped")
        let model = make(session)
        await model.interrupt()
        #expect(model.notice == "Still working after 2 presses; stopped")
        session.interruptResult = HarnessActionResult(result: .fallback)
        await model.interrupt()
        #expect(model.notice == ComposerCopy.notInterrupted)
    }

    /// The Stop chip's action reaches the helper: the composer's store routes call the session
    /// store, which calls harness.interrupt for this session.
    @Test func stopReachesHarnessInterrupt() async {
        let services = PreviewHarnessServices()
        let store = HarnessSessionStore.preview(services)
        let defaults = UserDefaults(suiteName: "composer-tests-\(UUID().uuidString)")!
        let model = CommandLineModel(store: store, memory: CommandMemory(defaults: defaults), routes: .store(store))
        model.routes.has = { _ in true }
        await model.interrupt()
        #expect(services.interrupts == 1)
        #expect(model.notice == nil)
    }

    @Test func cancellingAQueuedPromptStopsIt() async throws {
        let session = Session()
        session.working = true
        let model = make(session)
        model.draft = "later"
        await model.submit()
        let item = try #require(model.queued.first)
        await model.cancelQueued(item)
        #expect(session.runs.first?.0 == "stop")
        #expect(session.runs.first?.1 == item.id)
        #expect(model.queued.isEmpty)
    }

    @Test func outgoingKeepsLeadingSpaceAndDropsTrailingBreaks() {
        #expect(CommandLineLogic.outgoing("  ls\n\n") == "  ls")
        #expect(CommandLineLogic.outgoing("\n \n") == nil)
    }

    @Test func composerCountsQueuedPromptsWithoutListingThem() async {
        let session = Session()
        session.working = true
        let model = make(session)
        model.draft = "first"
        await model.submit()
        model.draft = "second"
        await model.submit()
        #expect(model.queuedCount == 2)
        #expect(ComposerCopy.queuedCount(2) == "2 queued")
    }
}
