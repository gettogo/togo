import Foundation
import TogoKit

/// A row of the sidebar that the detail can show.
enum SidebarItem: Hashable, Sendable {
    case board
    case session(TabID)
}

/// What the empty detail offers.
enum EmptyDetailAction: Equatable, Sendable {
    case newSession, addMachine
}

extension AppStore {
    /// The workspace is what the window shows: the cover in a compact window, or the detail
    /// beside the sidebar (which shows the empty detail while no tab is open). The workspace
    /// presents reviews and sign-in questions only while it is mounted; otherwise the root does.
    var workspaceOnScreen: Bool {
        guard workspaceOpen, !workspaceBehindBoard else { return false }
        return !workspaceInline || !workspace.tabs.isEmpty
    }

    /// The sidebar row that matches the detail: the board, the focused pane's session, or none.
    var sidebarSelection: SidebarItem? {
        get {
            if boardShown { return .board }
            guard workspaceOpen, let id = focusedTab?.id else { return nil }
            return .session(id)
        }
        set {
            switch newValue {
            case .board:
                boardShown = true
                path = []
            case .session(let id):
                showSession(id)
            case nil:
                break
            }
        }
    }

    /// Shows a session in the focused pane, from its row when the list has one.
    func showSession(_ id: TabID) {
        let rows = controlCenter.machines.flatMap(\.sessions) + controlCenter.archived
        if let row = rows.first(where: { $0.hostID == id.hostID && $0.id == id.sessionID }) {
            showFromSidebar(row)
        } else {
            boardShown = false
            if workspaceInline { path = [] }
            open(hostID: id.hostID, sessionID: id.sessionID)
        }
    }

    /// Settings opens as a sheet beside the sidebar, so it never covers the session in the
    /// detail; in a compact window it pushes onto the list.
    func showSettings() {
        if workspaceInline {
            sheet = .settings
        } else {
            path.append(.settings)
        }
    }

    /// A swipe in from the leading edge of a terminal: beside the sidebar it shows the sidebar,
    /// on a phone it goes back to the sessions list.
    func edgeSwipe() {
        if workspaceInline {
            sidebar.hidden = false
        } else {
            workspaceOpen = false
        }
    }

    var emptyDetailAction: EmptyDetailAction {
        services.hosts.isEmpty ? .addMachine : .newSession
    }

    /// The new-session sheet grows out of the plus button only while one is on screen.
    var newSessionZooms: Bool {
        !(workspaceInline && sidebar.isHidden)
    }
}
