import Foundation
import TogoKit
import TogoTerminal
import UIKit

/// The workspace's view of the store: which tab is on screen, what other sessions are asking,
/// and the actions the header menu runs against the current session.
extension AppStore {
    var selectedTab: WorkspaceTab? {
        workspace.tabs.first { $0.id == workspace.selected }
    }

    /// Requests from every session except the one on screen, oldest first. The current
    /// session's question is already in its terminal. An answered card leaves at once, which
    /// is what lets it shrink into its tab.
    var incomingRequests: [RequestCard] {
        controlCenter.requests.filter { card in
            if let id = card.tabID, id == workspace.selected || (panesVisible && id == workspace.secondary) {
                return false
            }
            if case .done = card.phase { return false }
            return true
        }
    }

    /// ⌘1 to ⌘9 pick tabs in strip order. A number past the last tab does nothing.
    func selectTab(number: Int) {
        guard (1...9).contains(number), workspace.tabs.indices.contains(number - 1) else { return }
        let id = workspace.tabs[number - 1].id
        if panesVisible, workspace.secondary != nil {
            place(id, in: workspace.focused)
        } else {
            select(id)
        }
    }

    /// Moves along the strip and stops at either end, the way the pager does. Swipes on the
    /// terminal itself call this, so every gesture agrees on the order.
    func selectTab(offset: Int) {
        guard let current = workspace.tabs.firstIndex(where: { $0.id == workspace.selected }) else { return }
        let target = min(max(current + offset, 0), workspace.tabs.count - 1)
        select(workspace.tabs[target].id)
    }

    /// Shortcuts the focused terminal passes up. ⌘V is handled where it starts. While the review
    /// is open over the workspace, ⌘W closes the review and the rest wait until it is gone.
    func perform(_ shortcut: ShortcutAction) {
        if reviewing != nil {
            if case .detach = shortcut { closeReview() }
            return
        }
        switch shortcut {
        case .session(let n):
            selectTab(number: n)
        case .controlCenter:
            workspaceOpen = false
        case .detach:
            if let focused = focusedTab?.id {
                close(focused)
            }
        case .newSession:
            startNewSession()
        case .paste:
            break
        }
    }

    /// Closes the tabs of sessions an online machine no longer lists. An offline machine's
    /// snapshot says nothing about its sessions, so its tabs stay.
    func closeEndedTabs(on hostID: HostID, listed: Set<String>) {
        for tab in workspace.tabs where tab.hostID == hostID && !listed.contains(tab.sessionID) {
            closeEndedTab(tab.id)
        }
        // A review opened from the sessions list has no tab of its own.
        if let subject = reviewing?.subject, subject.hostID == hostID, !listed.contains(subject.sessionID) {
            endReview(hostID: hostID, sessionID: subject.sessionID, why: ReviewEntryCopy.sessionEnded)
        }
    }

    /// Everything shown for a machine that is gone: its section, cards, archive rows and tabs.
    func forgetMachine(_ hostID: HostID) {
        endReview(hostID: hostID, sessionID: nil, why: ReviewEntryCopy.machineRemoved(machineName(for: hostID)))
        removedMachines.insert(hostID)
        controlCenter.machines.removeAll { $0.id == hostID }
        controlCenter.requests.removeAll { $0.hostID == hostID }
        controlCenter.archived.removeAll { $0.hostID == hostID }
        // A sign-in or edit for the machine has nothing left to act on; closing it cancels it.
        if sheet == .signIn(hostID) || sheet == .editMachine(hostID) { sheet = nil }
        if composing?.hostID == hostID { composing = nil }
        for tab in workspace.tabs where tab.hostID == hostID {
            close(tab.id)
        }
    }

    func closeEndedTab(_ id: TabID) {
        guard let tab = workspace.tabs.first(where: { $0.id == id }) else { return }
        if workspace.selected == id && workspaceOpen {
            workspace.ended = WorkspaceCopy.ended(tab.bib ?? tab.title)
        }
        endReview(hostID: tab.hostID, sessionID: tab.sessionID, why: ReviewEntryCopy.sessionEnded)
        close(id)
    }

    func setLink(_ link: LinkPhase, for id: TabID) {
        guard let index = workspace.tabs.firstIndex(where: { $0.id == id }),
              workspace.tabs[index].link != link else { return }
        workspace.tabs[index].link = link
    }

    func session(for tab: WorkspaceTab) -> SessionRow? {
        controlCenter.machines.first { $0.id == tab.hostID }?.sessions.first { $0.id == tab.sessionID }
    }

    func machine(for hostID: HostID) -> MachineSection? {
        controlCenter.machines.first { $0.id == hostID }
    }

    func machineName(for hostID: HostID) -> String {
        machine(for: hostID)?.name ?? services.hosts.first { $0.id == hostID }?.name ?? ""
    }

    /// "04 Claude Code" for a tab chip, falling back to the tab's own title for a session the
    /// control center does not list.
    func chipLabel(for tab: WorkspaceTab) -> (bib: String?, name: String) {
        if let row = session(for: tab) {
            return (row.bib, row.bib == nil ? SessionActions.label(row) : row.agentName)
        }
        return (tab.bib, tab.title)
    }

    /// Returns the line the header shows afterwards.
    func copyLastLines(of tab: WorkspaceTab) async -> String {
        do {
            let text = try await services.capture(hostID: tab.hostID, sessionID: tab.sessionID, lines: 500)
            UIPasteboard.general.string = text
            return WorkspaceCopy.copied
        } catch {
            return WorkspaceCopy.copyFailed(machineName(for: tab.hostID))
        }
    }

    /// Kills the session and closes its tab once the helper confirms. Returns why it failed.
    func end(_ tab: WorkspaceTab) async -> String? {
        do {
            try await services.kill(hostID: tab.hostID, sessionID: tab.sessionID)
            close(tab.id)
            return nil
        } catch {
            let label = session(for: tab).map(SessionActions.label) ?? tab.title
            return WorkspaceCopy.killFailed(label, ServiceErrors.message(error))
        }
    }
}

extension RequestCard {
    /// The tab this card belongs to, if it comes from a session.
    var tabID: TabID? {
        sessionID.map { TabID(hostID: hostID, sessionID: $0) }
    }
}

/// Two panes side by side. The same tab is never in both; the secondary stays in state while
/// the window is too narrow to show it.
extension AppStore {
    /// The tab ⌘1-9, ⌘W, find, compose and dictation act on.
    var focusedTab: WorkspaceTab? {
        let id = workspace.focused == .secondary ? workspace.secondary : workspace.selected
        return workspace.tabs.first { $0.id == id }
    }

    /// Opens the tab if needed and puts it in the secondary pane. Asked for the primary tab, the
    /// most recently used other tab takes the primary pane instead, or nothing happens.
    func openBeside(_ id: TabID) {
        if id == workspace.selected {
            guard let other = recentTabs.first(where: { $0 != id && hasTab($0) })
                ?? workspace.tabs.last(where: { $0.id != id })?.id else { return }
            workspace.selected = other
            touch(other)
        } else if !hasTab(id) {
            let primary = workspace.selected
            open(hostID: id.hostID, sessionID: id.sessionID)
            guard let primary else { return }
            workspace.selected = primary
        }
        workspace.secondary = id
        workspace.focused = panesVisible ? .secondary : .primary
        touch(id)
    }

    /// Called by the workspace when its arrangement changes. Collapsing to one pane hands focus
    /// back to the primary, the pane still on screen.
    func setPanesVisible(_ visible: Bool) {
        panesVisible = visible
        if !visible { workspace.focused = .primary }
    }

    func closeSecondary() {
        workspace.secondary = nil
        workspace.focused = .primary
    }

    func swapPanes() {
        guard let secondary = workspace.secondary, let primary = workspace.selected else { return }
        workspace.selected = secondary
        workspace.secondary = primary
        workspace.focused = workspace.focused == .primary ? .secondary : .primary
    }

    func focus(_ pane: Pane) {
        guard pane == .primary || (panesVisible && workspace.secondary != nil) else { return }
        workspace.focused = pane
    }

    /// Puts an open tab in a pane: a drop, or a pick from the sidebar. A tab already in the other
    /// pane trades places with the one there.
    func place(_ id: TabID, in pane: Pane) {
        guard hasTab(id) else { return }
        switch pane {
        case .primary:
            if id == workspace.secondary {
                workspace.secondary = workspace.selected
            }
            workspace.selected = id
        case .secondary:
            if id == workspace.selected {
                guard let secondary = workspace.secondary else {
                    openBeside(id)
                    return
                }
                workspace.selected = secondary
            }
            workspace.secondary = id
        }
        workspace.focused = panesVisible ? pane : .primary
        touch(id)
    }

    /// Keeps the panes apart when a tab leaves: the secondary's tab just goes, and the
    /// primary's is replaced by the secondary's.
    func releasePanes(closing id: TabID) {
        drafts[id] = nil
        recentTabs.removeAll { $0 == id }
        if id == workspace.secondary {
            workspace.secondary = nil
            workspace.focused = .primary
        } else if id == workspace.selected, let secondary = workspace.secondary {
            workspace.selected = secondary
            workspace.secondary = nil
            workspace.focused = .primary
        }
    }

    func touch(_ id: TabID) {
        recentTabs.removeAll { $0 == id }
        recentTabs.insert(id, at: 0)
    }

    private func hasTab(_ id: TabID) -> Bool {
        workspace.tabs.contains { $0.id == id }
    }
}
