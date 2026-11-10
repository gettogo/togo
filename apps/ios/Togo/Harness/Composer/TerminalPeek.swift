import Foundation
import Observation
import TogoKit
import TogoTerminal

// The terminal peek: every session runs the agent's real TUI under the chat. While the chat
// composer holds a "/" command, what is typed there is typed into the TUI's own input line
// too, and a strip above the composer shows the rows the TUI draws around that line: its
// own completion menu (omp's soft, remote and snapcompact for /compact, say). A tap on a row
// moves the TUI's selection there and takes it; Send presses Enter on the TUI's line. Only
// where the TUI owns input: an adapter that sends prompts through the harness API
// (`send.api`) gets the plain field (`CommandKind.picking`).

/// A key the composer sends into the agent's own input line.
enum PeekKey: Equatable, Sendable {
    case text(String)
    case backspace(Int)
    /// Empties the input line (Ctrl-U).
    case clearLine
    case down(Int)
    case up(Int)
    /// Takes the menu's selected row (Tab): it completes the line and does not send it.
    case accept
    case enter

    var strokes: [KeyStroke] {
        switch self {
        case .text(let text): text.map { KeyStroke(.char($0)) }
        case .backspace(let count): Array(repeating: KeyStroke(.backspace), count: max(0, count))
        case .clearLine: [KeyStroke(.char("u"), .ctrl)]
        case .down(let count): Array(repeating: KeyStroke(.down), count: max(0, count))
        case .up(let count): Array(repeating: KeyStroke(.up), count: max(0, count))
        case .accept: [KeyStroke(.tab)]
        case .enter: [KeyStroke(.enter)]
        }
    }
}

/// The agent's terminal as the peek uses it. Looked up at each use: the tab's terminal can be
/// attached anew.
struct PeekTerminal {
    var available: @MainActor () -> Bool
    var send: @MainActor ([PeekKey]) -> Void
    var screen: @MainActor () -> TerminalScreenRows?

    /// Through the tab's live terminal.
    static func live(_ handle: @escaping @MainActor () -> TerminalHandle?) -> PeekTerminal {
        PeekTerminal(
            available: { handle()?.screenRows != nil },
            send: { keys in handle()?.sendKeys(keys.flatMap(\.strokes)) },
            screen: { handle()?.screenRows?() }
        )
    }
}

/// What the peek holds between keystrokes.
@MainActor @Observable final class TerminalPeek {
    /// What this composer typed into the agent's input line. Nil when the agent changed the
    /// line itself (a row taken from its menu) and the field could not read it back.
    var mirrored: String? = ""
    /// Esc put the peek away for this draft; it is back once the draft no longer starts "/".
    var dismissed = false
    /// The screen as last read, while the peek runs.
    var screen: TerminalScreenRows?
    /// The screen before the first key went, to tell a menu drawn above the line from the
    /// conversation that was already there.
    var baseline: TerminalScreenRows?
}

/// The peek's rules, apart from the model and the terminal.
enum PeekSync {
    /// The draft is a one-line "/" command: the peek mirrors it.
    static func composing(_ draft: String) -> Bool {
        draft.hasPrefix("/") && !draft.contains(where: \.isNewline)
    }

    /// The keys that turn the agent's line from `old` into `new`: backspaces to where they
    /// part, then the rest typed. An empty `new` clears the line in one key; an unknown `old`
    /// is cleared and typed again.
    static func keys(from old: String?, to new: String) -> [PeekKey] {
        if old == new { return [] }
        if new.isEmpty { return [.clearLine] }
        guard let old else { return [.clearLine, .text(new)] }
        let common = zip(old, new).prefix { pair in pair.0 == pair.1 }.count
        var keys: [PeekKey] = []
        if old.count > common { keys.append(.backspace(old.count - common)) }
        if new.count > common { keys.append(.text(String(new.dropFirst(common)))) }
        return keys
    }

    /// The rows the strip shows, top to bottom: the readable rows under the input line, as a
    /// menu is drawn there; else the rows above it that changed since the first key, for a
    /// menu that opens upward. Never the input line itself; at most `limit`.
    static func strip(_ screen: TerminalScreenRows, baseline: TerminalScreenRows?, limit: Int = 8) -> [Int] {
        let rows = screen.rows
        let cursor = screen.cursorRow
        guard rows.indices.contains(cursor) else { return [] }
        let below = rows.indices.dropFirst(cursor + 1).filter { readable(rows[$0].text) }
        if !below.isEmpty { return Array(below.prefix(limit)) }
        guard let baseline, baseline.rows.count == rows.count else { return [] }
        let above = (0..<cursor).filter { readable(rows[$0].text) && rows[$0].text != baseline.rows[$0].text }
        return Array(above.suffix(limit))
    }

    /// A row with words in it, not only a box's border or blanks.
    static func readable(_ text: String) -> Bool {
        text.contains { $0.isLetter || $0.isNumber }
    }

    /// The row the TUI draws unlike the others among `rows`: its selection. Nil when no one
    /// row stands out.
    static func selected(_ screen: TerminalScreenRows, among rows: [Int]) -> Int? {
        guard rows.count > 1 else { return rows.first }
        var counts: [Int: Int] = [:]
        for row in rows { counts[screen.rows[row].look, default: 0] += 1 }
        let unique = rows.filter { counts[screen.rows[$0].look] == 1 }
        return unique.count == 1 ? unique[0] : nil
    }

    /// The arrow keys from the selected row to `target`, counted in strip rows (a border
    /// between items is not one), then the key that takes it.
    static func take(_ target: Int, selected: Int?, among rows: [Int]) -> [PeekKey] {
        guard let to = rows.firstIndex(of: target) else { return [] }
        let from = selected.flatMap { rows.firstIndex(of: $0) } ?? 0
        let step = to - from
        let moves: [PeekKey] = step > 0 ? [.down(step)] : step < 0 ? [.up(-step)] : []
        return moves + [.accept]
    }

    /// The agent's input line as the screen shows it: from the command word to the cursor,
    /// which sits at the end of what was typed (hints drawn after it are not the line).
    static func inputLine(_ screen: TerminalScreenRows, command: String) -> String? {
        guard !command.isEmpty, screen.rows.indices.contains(screen.cursorRow) else { return nil }
        let row = Array(screen.rows[screen.cursorRow].text)
        let word = Array(command)
        guard let start = row.indices.first(where: { row[$0...].starts(with: word) }) else { return nil }
        let end = screen.cursorColumn
        guard end >= start + word.count else { return nil }
        var line = String(row[start..<min(end, row.count)])
        // The row's text stops at its last written cell; a space typed at the end is past it.
        if end > row.count { line += String(repeating: " ", count: end - row.count) }
        return line
    }

    /// What Send does with a command the agent's line holds.
    enum Submit: Equatable {
        /// The command has a native home, or asks first: the line is cleared and the command
        /// goes the way a typed one goes (`routeTyped`).
        case native
        /// Enter on the agent's line; a terminal command shows the terminal as it goes.
        case enter(showTerminal: Bool)
    }

    static func submit(_ text: String, commands: [HarnessCompletion]) -> Submit {
        guard case let .command(command, args) = CommandLineLogic.typed(text, commands: commands) else {
            return .enter(showTerminal: false)
        }
        switch CommandKind.dispatch(command) {
        case .choose, .push, .open:
            return .native
        case .run(let run):
            return args.isEmpty && (run.action != nil || run.destructive) ? .native : .enter(showTerminal: false)
        case .compose:
            return .enter(showTerminal: false)
        case .terminal:
            return .enter(showTerminal: true)
        }
    }

    /// The strip's rows cropped as one block: the indent they all share goes.
    static func cropped(_ texts: [String]) -> [String] {
        func leading(_ text: String) -> Int { text.prefix { $0 == " " }.count }
        let indent = texts.filter { !$0.allSatisfy(\.isWhitespace) }.map(leading).min() ?? 0
        return texts.map { text in String(text.dropFirst(min(indent, leading(text)))) }
    }
}
