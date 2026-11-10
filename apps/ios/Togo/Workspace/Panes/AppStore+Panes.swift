import Foundation

/// Where a fold left the panes: the secondary's tab was shown alone and the primary's waits
/// beside it, to be put back when the window opens again.
struct PanePromotion: Equatable, Sendable {
    var shown: TabID
    var beside: TabID
}

extension AppStore {
    /// Called by the workspace when its arrangement changes. Folding to one pane shows the
    /// focused one, so a focused secondary trades places with the primary; opening again puts
    /// both back where they were, unless the user has moved on to other tabs meanwhile.
    func arrangePanes(visible: Bool) {
        if visible {
            setPanesVisible(true)
            if let promotion = panePromotion, workspace.selected == promotion.shown,
               workspace.secondary == promotion.beside {
                swapPanes()
            }
            panePromotion = nil
        } else {
            if panesVisible, workspace.focused == .secondary,
               let shown = workspace.secondary, let beside = workspace.selected {
                swapPanes()
                panePromotion = PanePromotion(shown: shown, beside: beside)
            }
            setPanesVisible(false)
        }
    }

    /// The tabs `pane` pages through. With two panes on screen the other pane's tab is left out,
    /// so paging never pulls a tab across.
    func pages(for pane: Pane) -> [WorkspaceTab] {
        guard panesVisible, let secondary = workspace.secondary else { return workspace.tabs }
        let other = pane == .primary ? secondary : workspace.selected
        return workspace.tabs.filter { $0.id != other }
    }

    /// The pane showing `id`: the secondary only while it is on screen.
    func pane(showing id: TabID) -> Pane {
        panesVisible && id == workspace.secondary ? .secondary : .primary
    }

    /// A swipe on the terminal of `id` moves its own pane along its pages and stops at either
    /// end, the way that pane's pager does.
    func step(_ offset: Int, from id: TabID) {
        let pane = pane(showing: id)
        let pages = pages(for: pane)
        guard let current = pages.firstIndex(where: { $0.id == id }) else { return }
        let target = pages[min(max(current + offset, 0), pages.count - 1)].id
        guard target != id else { return }
        workspace.ended = nil
        place(target, in: pane)
    }

    /// A tap on a tab chip. With two panes on screen a visible tab's chip focuses its pane and
    /// any other fills the focused pane.
    func choose(_ id: TabID) {
        workspace.ended = nil
        guard panesVisible, let secondary = workspace.secondary else {
            select(id)
            return
        }
        if id == secondary {
            focus(.secondary)
        } else if id == workspace.selected {
            focus(.primary)
        } else {
            place(id, in: workspace.focused)
        }
    }

    /// Opens a session from a request card into the focused pane.
    func openInFocusedPane(hostID: HostID, sessionID: String) {
        let id = TabID(hostID: hostID, sessionID: sessionID)
        let primary = workspace.selected
        guard panesVisible, workspace.focused == .secondary, let primary, id != primary,
              id != workspace.secondary else {
            open(hostID: hostID, sessionID: sessionID)
            return
        }
        open(hostID: hostID, sessionID: sessionID)
        workspace.selected = primary
        place(id, in: .secondary)
    }
}
