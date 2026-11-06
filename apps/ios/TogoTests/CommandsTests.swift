import Foundation
import Testing
import TogoTerminal
@testable import Togo

@MainActor struct CommandsTests {
    private let one = TabID(hostID: Samples.studio, sessionID: "s-01")
    private let four = TabID(hostID: Samples.studio, sessionID: "s-04")
    private let scratch = TabID(hostID: Samples.studio, sessionID: "s-scratch")

    /// A regular-width window with three tabs open and the first on screen.
    private func store() -> AppStore {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.setWorkspaceInline(true)
        store.panesFit = true
        for id in [one, four, scratch] {
            store.open(hostID: id.hostID, sessionID: id.sessionID)
        }
        store.select(one)
        return store
    }

    @Test func commandKFocusesSidebarSearch() {
        let store = store()
        store.sidebar.hidden = true
        let requests = store.sidebar.searchRequests
        store.perform(.controlCenter)
        #expect(!store.sidebar.isHidden)
        #expect(store.sidebar.searchRequests == requests + 1)
        // The workspace stays beside the sidebar.
        #expect(store.workspaceOpen)

        // In a compact window ⌘K goes back to the list, from wherever the list was.
        store.setWorkspaceInline(false)
        store.path = [.settings]
        store.perform(.controlCenter)
        #expect(!store.workspaceOpen)
        #expect(store.path.isEmpty)
        #expect(store.sheet == nil)
        #expect(store.sidebar.searchRequests == requests + 1)
    }

    @Test func splitToggleOpensNextTabBeside() {
        let store = store()
        store.setPanesVisible(true)
        store.perform(.splitToggle)
        #expect(store.workspace.selected == one)
        #expect(store.workspace.secondary == four)
        #expect(store.workspace.focused == .secondary)

        store.perform(.splitToggle)
        #expect(store.workspace.secondary == nil)
        #expect(store.workspace.focused == .primary)

        // From the last tab, the next one wraps to the first.
        store.select(scratch)
        store.toggleSplit()
        #expect(store.workspace.secondary == one)
    }

    /// A regular window can still be too narrow for two panes; ⌘\ then does nothing.
    @Test func splitToggleNeedsRoomForTwoPanes() {
        let store = store()
        store.panesFit = false
        #expect(!store.canToggleSplit)
        store.perform(.splitToggle)
        #expect(store.workspace.secondary == nil)
        store.panesFit = true
        #expect(store.canToggleSplit)
    }

    @Test func splitToggleNeedsTwoTabsAndAWideWindow() {
        let lone = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        lone.setWorkspaceInline(true)
        lone.panesFit = true
        lone.open(hostID: one.hostID, sessionID: one.sessionID)
        lone.perform(.splitToggle)
        #expect(lone.workspace.secondary == nil)

        let narrow = store()
        narrow.setWorkspaceInline(false)
        narrow.perform(.splitToggle)
        #expect(narrow.workspace.secondary == nil)
    }

    @Test func focusShortcutsMovePaneFocus() {
        let store = store()
        store.setPanesVisible(true)
        store.openBeside(four)
        store.perform(.focusLeft)
        #expect(store.workspace.focused == .primary)
        #expect(store.focusedTab?.id == one)
        store.perform(.focusRight)
        #expect(store.workspace.focused == .secondary)
        #expect(store.focusedTab?.id == four)

        // ⌘W closes the focused pane's tab.
        store.perform(.detach)
        #expect(!store.workspace.tabs.contains { $0.id == four })
        #expect(store.workspace.selected == one)
    }

    @Test func tabStepsMoveTheFocusedPane() {
        let store = store()
        store.perform(.nextTab)
        #expect(store.workspace.selected == four)
        store.perform(.nextTab)
        store.perform(.nextTab)
        #expect(store.workspace.selected == scratch)
        store.perform(.previousTab)
        #expect(store.workspace.selected == four)

        // With two panes the focused one steps and skips the tab beside it.
        store.select(one)
        store.setPanesVisible(true)
        store.openBeside(four)
        store.focus(.primary)
        store.perform(.nextTab)
        #expect(store.workspace.selected == scratch)
        #expect(store.workspace.secondary == four)
    }

    @Test func toggleSidebarFlipsTheDefault() {
        let store = store()
        #expect(!store.sidebar.isHidden)
        store.perform(.toggleSidebar)
        #expect(store.sidebar.isHidden)
        store.perform(.toggleSidebar)
        #expect(!store.sidebar.isHidden)
    }

    /// Sheets hang off the split view itself, so they need not wait for a cover to close.
    @Test func sheetsOpenAtOnceBesideTheSidebar() {
        let store = store()
        store.showSheet(.addMachine)
        #expect(store.sheet == .addMachine)
        #expect(store.workspaceOpen)
    }
}
