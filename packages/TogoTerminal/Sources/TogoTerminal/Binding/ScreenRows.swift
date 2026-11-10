import Foundation
internal import SwiftTerm

/// The live screen's rows as text, with where the cursor is and how each row starts out
/// drawn: enough to show a crop of what a program draws under its input line (a completion
/// menu, say) somewhere else, and to tell which of those rows it draws differently.
public struct TerminalScreenRows: Sendable, Equatable {
    public struct Row: Sendable, Equatable {
        public var text: String
        /// The look of the row's first visible character (colours and style), as a number to
        /// compare with other rows of the same screen. Not stable across launches.
        public var look: Int

        public init(text: String, look: Int = 0) {
            self.text = text
            self.look = look
        }
    }

    /// Top to bottom, the live screen only (never scrollback).
    public var rows: [Row]
    /// The cursor's row, an index into `rows`.
    public var cursorRow: Int
    public var cursorColumn: Int

    public init(rows: [Row], cursorRow: Int, cursorColumn: Int = 0) {
        self.rows = rows
        self.cursorRow = cursorRow
        self.cursorColumn = cursorColumn
    }
}

extension SwiftTermEngine {
    /// The live screen now, whether or not the view is drawing (a covered terminal keeps
    /// parsing). Nil before a session is attached.
    public func screenRows() -> TerminalScreenRows? {
        guard let snapshot = terminalView.terminalContentSnapshot(region: .history(maximumScrollbackRows: -1)),
              let top = snapshot.rows.first?.absoluteRow else { return nil }
        let rows = snapshot.rows.map { row in
            TerminalScreenRows.Row(text: row.text, look: Self.look(of: row.cells))
        }
        return TerminalScreenRows(rows: rows, cursorRow: snapshot.cursorRow - top, cursorColumn: snapshot.cursorColumn)
    }

    /// The first visible character's attribute, hashed; 0 for a blank row.
    static func look(of cells: [TerminalCellSnapshot]) -> Int {
        guard let cell = cells.first(where: { !$0.text.allSatisfy(\.isWhitespace) && !$0.text.isEmpty }) else { return 0 }
        var hasher = Hasher()
        hasher.combine(cell.attribute)
        return hasher.finalize()
    }
}
