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
            if let id = card.tabID, id == workspace.selected { return false }
            if case .done = card.phase { return false }
            return true
        }
    }

    /// ⌘1 to ⌘9 pick tabs in strip order. A number past the last tab does nothing.
    func selectTab(number: Int) {
        guard (1...9).contains(number), workspace.tabs.indices.contains(number - 1) else { return }
        select(workspace.tabs[number - 1].id)
    }

    /// Moves along the strip and stops at either end, the way the pager does. Swipes on the
    /// terminal itself call this, so every gesture agrees on the order.
    func selectTab(offset: Int) {
        guard let current = workspace.tabs.firstIndex(where: { $0.id == workspace.selected }) else { return }
        let target = min(max(current + offset, 0), workspace.tabs.count - 1)
        select(workspace.tabs[target].id)
    }

    /// Shortcuts the focused terminal passes up. ⌘V is handled where it starts.
    func perform(_ shortcut: ShortcutAction) {
        switch shortcut {
        case .session(let n):
            selectTab(number: n)
        case .controlCenter:
            workspaceOpen = false
        case .detach:
            if let selected = workspace.selected {
                close(selected)
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
    }

    /// Everything shown for a machine that is gone: its section, cards, archive rows and tabs.
    func forgetMachine(_ hostID: HostID) {
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
