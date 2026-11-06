import Foundation

/// The sidebar's keyboard state: a cursor the arrows move through the rows, and whether the
/// open search came from ⌘K, so closing it hands focus back to the terminal.
struct SidebarKeyboard: Equatable {
    var cursor: TabID?
    private var searchFromShortcut = false

    /// Moves along the rows and stops at either end. From nothing, down starts at the top and
    /// up at the bottom.
    mutating func move(_ step: Int, through ids: [TabID]) {
        guard !ids.isEmpty else { return }
        let start = cursor.flatMap(ids.firstIndex(of:)) ?? (step > 0 ? -1 : ids.count)
        cursor = ids[min(max(start + step, 0), ids.count - 1)]
    }

    func row(in rows: [SessionRow]) -> SessionRow? {
        rows.first { TabID(hostID: $0.hostID, sessionID: $0.id) == cursor }
    }

    /// ⌘⌫: the row to confirm ending, only where its menu offers Kill.
    func rowToEnd(in rows: [SessionRow], machines: [MachineSection]) -> SessionRow? {
        guard let row = row(in: rows) else { return nil }
        let machine = machines.first { $0.id == row.hostID }
        return SessionActions.actions(for: row, on: machine).allows(.kill) ? row : nil
    }

    mutating func searchRequested() {
        searchFromShortcut = true
    }

    /// Called when the search closes, by Escape or Cancel. True when ⌘K opened it, so focus
    /// goes back where it came from.
    mutating func searchClosed() -> Bool {
        defer { searchFromShortcut = false }
        return searchFromShortcut
    }
}
