import Foundation
import Testing
import TogoKit
import TogoTerminal
@testable import Togo

/// The terminal peek: a "/" command in the chat composer is typed into the agent's own input
/// line as it is written, the strip shows the rows the TUI draws under it, a tap takes one,
/// Esc and Control-U let go of the line, and Send presses Enter on it. Sessions whose prompts
/// go through the harness API keep the plain field.
@MainActor struct TerminalPeekTests {
    /// A TUI with one input line and, while it holds "/compact ", a menu under it that filters
    /// by what follows and marks its selection in its own colour.
    @MainActor final class FakeTUI {
        var line = ""
        var menu = ["soft", "remote", "snapcompact"]
        var selected = 0
        /// The first Enter with the menu up takes the selection rather than sending, as omp
        /// 18.16 did once.
        var takesFirstEnter = false
        private(set) var keys: [PeekKey] = []
        private(set) var sent: [String] = []

        var shown: [String] {
            guard line.hasPrefix("/compact ") else { return [] }
            let typed = line.dropFirst("/compact ".count)
            return menu.filter { $0.hasPrefix(typed) }
        }

        func apply(_ batch: [PeekKey]) {
            for key in batch {
                keys.append(key)
                switch key {
                case .text(let text): line += text
                case .backspace(let count): line = String(line.dropLast(count))
                case .clearLine: line = ""
                case .down(let count): selected = min(selected + count, max(0, shown.count - 1))
                case .up(let count): selected = max(selected - count, 0)
                case .accept: take()
                case .enter:
                    if takesFirstEnter, !shown.isEmpty {
                        takesFirstEnter = false
                        take()
                    } else {
                        sent.append(line)
                        line = ""
                    }
                }
            }
        }

        private func take() {
            guard shown.indices.contains(selected) else { return }
            line = "/compact " + shown[selected] + " "
            selected = 0
        }

        /// The screen as a terminal reads it: the line's trailing spaces are not text, and a
        /// hint is drawn from the cursor on while only the command is typed.
        func screen() -> TerminalScreenRows {
            let hint = line == "/compact " ? "[mode] [focus]" : ""
            var input = "> " + line + hint
            while input.last == " " { input.removeLast() }
            var rows = [TerminalScreenRows.Row(text: "earlier output", look: 1), TerminalScreenRows.Row(text: input, look: 1)]
            for (index, item) in shown.enumerated() {
                rows.append(.init(text: "  " + item, look: index == selected ? 2 : 1))
            }
            rows.append(.init(text: ""))
            return TerminalScreenRows(rows: rows, cursorRow: 1, cursorColumn: 2 + line.count)
        }
    }

    @MainActor final class Session {
        var working = false
        var caps: Set<String> = [HarnessCapability.commands]
        var commands: [HarnessCompletion] = []
        private(set) var sends: [String] = []
        private(set) var runs: [String] = []

        func routes() -> CommandLineModel.Routes {
            CommandLineModel.Routes(
                has: { self.caps.contains($0) }, working: { self.working }, mode: { "default" }, modes: { [] },
                queued: { [] }, scope: { "peek-tests" },
                complete: { _, query in CommandLineLogic.filter(self.commands, query: query) },
                send: { text, _ in
                    self.sends.append(text)
                    return nil
                },
                interrupt: { HarnessActionResult(result: .applied) },
                run: { action, _, _ in
                    self.runs.append(action)
                    return HarnessActionResult(result: .applied)
                }
            )
        }
    }

    private let compact = HarnessCompletion(insert: "/compact ", title: "/compact", hint: "[mode] [focus]", kind: .run, action: "compact")
    private let model = HarnessCompletion(
        insert: "/model ", title: "/model", screen: "model", kind: .choice, action: "set_model",
        choices: [.init(id: "opus", label: "Opus", selected: true), .init(id: "sonnet", label: "Sonnet")]
    )

    private func make(_ session: Session, _ tui: FakeTUI?) -> CommandLineModel {
        let defaults = UserDefaults(suiteName: "peek-tests-\(UUID().uuidString)")!
        let composer = CommandLineModel(store: .preview(), memory: CommandMemory(defaults: defaults), routes: session.routes())
        composer.peekSettle = .zero
        if let tui {
            composer.peekTerminal = PeekTerminal(available: { true }, send: { tui.apply($0) }, screen: { tui.screen() })
        }
        return composer
    }

    // MARK: Sync

    @Test func theAgentsLineFollowsTheFieldKeyByKey() {
        let tui = FakeTUI()
        let composer = make(Session(), tui)
        composer.draft = "/"
        composer.draft = "/co"
        composer.draft = "/c"
        composer.draft = "/compact "
        #expect(tui.keys == [.text("/"), .text("co"), .backspace(1), .text("ompact ")])
        #expect(tui.line == "/compact ")
        #expect(composer.peekActive)
        composer.draft = ""
        #expect(tui.keys.last == .clearLine)
        #expect(tui.line.isEmpty)
    }

    @Test func keysBackUpToWhereTheLinesPartThenType() {
        #expect(PeekSync.keys(from: "/compact soft", to: "/compact remote") == [.backspace(4), .text("remote")])
        #expect(PeekSync.keys(from: "/compact", to: "/compact") == [])
        #expect(PeekSync.keys(from: "/compact so", to: "") == [.clearLine])
        #expect(PeekSync.keys(from: "", to: "") == [])
        // A line the agent changed is cleared and typed again.
        #expect(PeekSync.keys(from: nil, to: "/compact x") == [.clearLine, .text("/compact x")])
        #expect(PeekSync.composing("/compact "))
        #expect(!PeekSync.composing("fix /compact"))
        #expect(!PeekSync.composing("/compact\nmore"))
    }

    /// The fallback: an adapter that sends prompts through the harness API, a turn in
    /// progress, or no terminal leaves the agent's line alone.
    @Test func onlyWhereTheTUIOwnsTheInput() {
        let api = Session()
        api.caps.insert(HarnessCapability.sendAPI)
        let viaAPI = FakeTUI()
        make(api, viaAPI).draft = "/compact "
        #expect(viaAPI.keys.isEmpty)

        let busy = Session()
        busy.working = true
        let working = FakeTUI()
        make(busy, working).draft = "/compact "
        #expect(working.keys.isEmpty)

        let noTerminal = make(Session(), nil)
        noTerminal.draft = "/compact "
        #expect(!noTerminal.peekActive)
    }

    @Test func escLetsGoOfTheLineAndKeepsTheField() {
        let tui = FakeTUI()
        let composer = make(Session(), tui)
        composer.draft = "/compact so"
        composer.cancelPeek()
        #expect(tui.keys.last == .clearLine)
        #expect(tui.line.isEmpty)
        #expect(composer.draft == "/compact so")
        #expect(!composer.peekActive)
        let count = tui.keys.count
        composer.draft = "/compact sof"
        #expect(tui.keys.count == count)
        // Once the field leaves "/", the next command is mirrored again.
        composer.draft = ""
        composer.draft = "/"
        #expect(tui.keys.last == .text("/"))
    }

    // MARK: The strip

    @Test func theStripShowsTheMenuUnderTheLineAndTakesARow() async {
        let tui = FakeTUI()
        let composer = make(Session(), tui)
        composer.draft = "/compact "
        composer.refreshPeek()
        #expect(composer.peekRows == [2, 3, 4])
        await composer.takePeekRow(4)
        #expect(tui.keys.suffix(2) == [.down(2), .accept])
        #expect(tui.line == "/compact snapcompact ")
        // The field reads the line back, so both say the same.
        #expect(composer.draft == "/compact snapcompact ")
        #expect(composer.peek.mirrored == "/compact snapcompact ")
        composer.draft = "/compact snapcompact tests"
        #expect(tui.keys.last == .text("tests"))
        #expect(tui.line == "/compact snapcompact tests")
    }

    @Test func aMenuAboveTheLineIsWhatChangedThere() {
        let before = TerminalScreenRows(rows: [.init(text: "old output"), .init(text: "more output"), .init(text: "> ")], cursorRow: 2)
        let after = TerminalScreenRows(
            rows: [.init(text: "old output"), .init(text: " soft  local", look: 2), .init(text: "> /compact")], cursorRow: 2
        )
        #expect(PeekSync.strip(after, baseline: before) == [1])
        #expect(PeekSync.strip(after, baseline: nil) == [])
        // A box's border under the line is not a row.
        let boxed = TerminalScreenRows(rows: [.init(text: "│ > /co │"), .init(text: "╰──────╯"), .init(text: "  /compact")], cursorRow: 0)
        #expect(PeekSync.strip(boxed, baseline: nil) == [2])
    }

    @Test func theSelectionIsTheRowDrawnUnlikeTheOthers() {
        let screen = TerminalScreenRows(
            rows: [.init(text: "> /x"), .init(text: "a", look: 1), .init(text: "b", look: 7), .init(text: "c", look: 1)], cursorRow: 0
        )
        #expect(PeekSync.selected(screen, among: [1, 2, 3]) == 2)
        #expect(PeekSync.take(1, selected: 2, among: [1, 2, 3]) == [.up(1), .accept])
        #expect(PeekSync.take(2, selected: 2, among: [1, 2, 3]) == [.accept])
        // Two rows that both stand out, or none: the menu's first row is taken as selected.
        #expect(PeekSync.selected(screen, among: [1, 2]) == nil)
        #expect(PeekSync.take(2, selected: nil, among: [1, 2]) == [.down(1), .accept])
        #expect(PeekSync.take(9, selected: nil, among: [1, 2]).isEmpty)
    }

    @Test func theLineReadsUpToTheCursor() {
        let hinted = TerminalScreenRows(rows: [.init(text: "╰─ /compact [mode] [focus]")], cursorRow: 0, cursorColumn: 12)
        #expect(PeekSync.inputLine(hinted, command: "/compact") == "/compact ")
        let spaced = TerminalScreenRows(rows: [.init(text: "> /compact soft")], cursorRow: 0, cursorColumn: 16)
        #expect(PeekSync.inputLine(spaced, command: "/compact") == "/compact soft ")
        let gone = TerminalScreenRows(rows: [.init(text: "> ")], cursorRow: 0, cursorColumn: 2)
        #expect(PeekSync.inputLine(gone, command: "/compact") == nil)
        #expect(PeekSync.cropped(["  soft", "  remote", ""]) == ["soft", "remote", ""])
    }

    // MARK: Send

    @Test func sendPressesEnterOnTheAgentsLine() async {
        let session = Session()
        session.commands = [compact]
        let tui = FakeTUI()
        let composer = make(session, tui)
        composer.draft = "/compact snapcompact"
        await composer.submit()
        #expect(tui.sent == ["/compact snapcompact"])
        #expect(session.sends.isEmpty)
        #expect(session.runs.isEmpty)
        #expect(composer.draft.isEmpty)
        #expect(tui.keys.filter { $0 == .enter }.count == 1)
    }

    @Test func aMenuThatTookTheFirstEnterGetsASecond() async {
        let session = Session()
        session.commands = [compact]
        let tui = FakeTUI()
        tui.takesFirstEnter = true
        let composer = make(session, tui)
        composer.draft = "/compact snap"
        await composer.submit()
        #expect(tui.sent == ["/compact snapcompact "])
        #expect(tui.line.isEmpty)
    }

    /// A command with a native home lets go of the agent's line and opens it, as typed.
    @Test func aNativeCommandOpensItsHomeInstead() async {
        let session = Session()
        session.commands = [model]
        let tui = FakeTUI()
        let composer = make(session, tui)
        composer.draft = "/model"
        await composer.submit()
        #expect(tui.sent.isEmpty)
        #expect(tui.line.isEmpty)
        #expect(composer.presentedScreen == "model")
    }

    @Test func whatSendDoesWithEachCommand() {
        let clear = HarnessCompletion(insert: "/clear", title: "/clear", kind: .run, role: .destructive)
        let tools = HarnessCompletion(insert: "/tools", title: "/tools", kind: .terminal)
        let add = HarnessCompletion(insert: "/add ", title: "/add", hint: "<file>", kind: .args)
        let all = [compact, model, clear, tools, add]
        #expect(PeekSync.submit("/compact soft", commands: all) == .enter(showTerminal: false))
        #expect(PeekSync.submit("/compact", commands: all) == .native)
        #expect(PeekSync.submit("/model", commands: all) == .native)
        #expect(PeekSync.submit("/model sonnet", commands: all) == .native)
        #expect(PeekSync.submit("/clear", commands: all) == .native)
        #expect(PeekSync.submit("/tools", commands: all) == .enter(showTerminal: true))
        #expect(PeekSync.submit("/add a.go", commands: all) == .enter(showTerminal: false))
        #expect(PeekSync.submit("/skill-of-mine x", commands: all) == .enter(showTerminal: false))
    }
}
