import Foundation
import TogoKit

extension AppStore {
    /// The workspace is what the window shows: pushed over the roster in a compact window, or
    /// the terminal column beside it (which shows the empty detail while no tab is open).
    var workspaceOnScreen: Bool {
        guard workspaceOpen else { return false }
        return !workspaceInline || !workspace.tabs.isEmpty
    }

    /// Shows a session in the focused pane, from its line when the roster has one.
    func showSession(_ id: TabID) {
        let rows = controlCenter.machines.flatMap(\.sessions) + controlCenter.archived
        if let row = rows.first(where: { $0.hostID == id.hostID && $0.id == id.sessionID }) {
            showFromSidebar(row)
        } else {
            if workspaceInline { path = [] }
            open(hostID: id.hostID, sessionID: id.sessionID)
        }
    }

    /// A line picked in the roster beside the terminal fills the focused pane. With one pane on
    /// screen that is the primary, so the column shows one large terminal unless the user
    /// opened a second.
    func showFromSidebar(_ row: SessionRow, transport: TransportPreference? = nil) {
        // Whatever the detail had pushed would otherwise stay on top of the session.
        if workspaceInline { path = [] }
        let id = TabID(hostID: row.hostID, sessionID: row.id)
        guard panesVisible, workspace.secondary != nil, workspace.focused == .secondary else {
            open(hostID: row.hostID, sessionID: row.id, transport: transport)
            return
        }
        if !workspace.tabs.contains(where: { $0.id == id }) || transport != nil {
            let primary = workspace.selected
            let secondary = workspace.secondary
            open(hostID: row.hostID, sessionID: row.id, transport: transport)
            workspace.selected = primary
            workspace.secondary = secondary
        }
        place(id, in: .secondary)
        workspaceOpen = true
    }

    /// Settings is a sheet with its own stack in every layout (its pages push inside it), so it
    /// never covers the session in the detail column.
    func showSettings() {
        // A form in progress keeps the screen; Settings waits for it to close.
        guard sheet?.isForm != true else { return }
        showSheet(.settings)
    }

    /// A swipe in from the leading edge of a terminal: beside the roster it shows the roster,
    /// on a phone it goes back to the roster.
    func edgeSwipe() {
        if workspaceInline {
            sidebar.hidden = false
        } else {
            workspaceOpen = false
        }
    }
}
