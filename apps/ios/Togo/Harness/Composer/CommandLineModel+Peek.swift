import Foundation
import TogoKit

/// The terminal peek through the model (`TerminalPeek`): the draft mirrored into the agent's
/// input line, the strip's rows, a row taken, Esc, and Send pressing Enter on that line.
extension CommandLineModel {
    /// The session's TUI owns its input and can be reached: commands, prompts typed rather
    /// than sent through the harness API, an idle agent, no pictures waiting.
    var peekable: Bool {
        guard let peekTerminal, peekTerminal.available(), !peek.dismissed, attachments.isEmpty, !working else { return false }
        return routes.has(HarnessCapability.commands) && !routes.has(HarnessCapability.sendAPI)
    }

    /// The draft is a "/" command the agent's line mirrors.
    var peekActive: Bool { peekable && PeekSync.composing(draft) }

    /// The screen rows the strip shows; empty while the `/` menu is up or the TUI draws
    /// nothing around its line.
    var peekRows: [Int] {
        guard peekActive, menu == .none, let screen = peek.screen else { return [] }
        return PeekSync.strip(screen, baseline: peek.baseline)
    }

    /// Brings the agent's line to what the field holds while the peek runs, and empties it
    /// once it stops. Nothing is typed into a line this composer did not type into.
    func syncPeek() {
        guard let terminal = peekTerminal else { return }
        if !PeekSync.composing(draft) { peek.dismissed = false }
        let target = peekActive ? draft : ""
        let keys = PeekSync.keys(from: peek.mirrored, to: target)
        guard !keys.isEmpty else { return }
        if peek.mirrored == "" { peek.baseline = terminal.screen() }
        terminal.send(keys)
        peek.mirrored = target
        if target.isEmpty {
            peek.screen = nil
            peek.baseline = nil
        }
    }

    /// Reads the screen again while the peek runs.
    func refreshPeek() {
        let screen = peekActive ? peekTerminal?.screen() : nil
        if screen != peek.screen { peek.screen = screen }
    }

    /// Esc or the strip's close: the agent's line is emptied and the strip goes; the field
    /// keeps its words, which Send sends the usual way.
    func cancelPeek() {
        guard peekActive else { return }
        peek.dismissed = true
        syncPeek()
    }

    /// A row of the strip: the TUI's selection moves there and takes it, and the field reads
    /// the line back so the two stay the same.
    func takePeekRow(_ row: Int) async {
        guard peekActive, let terminal = peekTerminal, let screen = peek.screen else { return }
        let rows = PeekSync.strip(screen, baseline: peek.baseline)
        let keys = PeekSync.take(row, selected: PeekSync.selected(screen, among: rows), among: rows)
        guard !keys.isEmpty else { return }
        let word = String(draft.prefix { !$0.isWhitespace })
        terminal.send(keys)
        // The agent changed its line; until it is read back, the line is what goes on Send.
        peek.mirrored = nil
        requestFocus()
        try? await Task.sleep(for: peekSettle)
        guard peekActive, peek.mirrored == nil, let after = terminal.screen() else { return }
        peek.screen = after
        guard let line = PeekSync.inputLine(after, command: word), PeekSync.composing(line) else { return }
        peek.mirrored = line
        draft = line
    }

    /// Send while the peek runs. A command with a native home, or one that asks first, has
    /// the agent's line emptied and goes the way a typed command goes (false). Anything else
    /// is the agent's line already: Enter sends it (true).
    func submitThroughPeek() async -> Bool {
        guard peekActive, let terminal = peekTerminal, let text = CommandLineLogic.outgoing(draft) else { return false }
        let commands = routes.has(HarnessCapability.commands) ? await routingCommands() : []
        guard peekActive, CommandLineLogic.outgoing(draft) == text else { return false }
        switch PeekSync.submit(text, commands: commands) {
        case .native:
            peek.dismissed = true
            syncPeek()
            return false
        case .enter(let showTerminal):
            memory.record(text, scope: scope)
            // Enter empties the agent's line; the field empties with it.
            peek.mirrored = ""
            peek.screen = nil
            peek.baseline = nil
            draft = ""
            closeMenu()
            pendingArgs = nil
            clearNotice()
            store.undo = nil
            terminal.send([.enter])
            if showTerminal { routes.showTerminal() }
            await confirmPeekSent(text, terminal: terminal)
            return true
        }
    }

    /// A TUI whose menu was open takes the first Enter as a pick: the line, completed, is
    /// still there. One more Enter sends it.
    private func confirmPeekSent(_ text: String, terminal: PeekTerminal) async {
        guard let word = CommandLineLogic.commandWord(text) else { return }
        try? await Task.sleep(for: peekSettle)
        guard draft.isEmpty, peek.mirrored == "", let screen = terminal.screen(),
              screen.rows.indices.contains(screen.cursorRow),
              PeekSync.inputLine(screen, command: word) != nil else { return }
        terminal.send([.enter])
    }
}
