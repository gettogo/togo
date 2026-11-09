import Foundation
import Testing
import TogoKit
@testable import Togo

/// Where a window's taps and keys land beside the roster, and in the compact stack.
@MainActor struct ShellRoutingTests {
    private let one = TabID(hostID: Samples.studio, sessionID: "s-01")
    private let four = TabID(hostID: Samples.studio, sessionID: "s-04")

    private func store(inline: Bool = true, hosts: [HostRecordView] = Samples.hosts) -> AppStore {
        let store = AppStore(services: FakeAppServices(hosts: hosts), controlCenter: Samples.controlCenter)
        store.setWorkspaceInline(inline)
        return store
    }

    private func row(_ id: TabID) -> SessionRow {
        Samples.controlCenter.machines.flatMap(\.sessions).first { $0.id == id.sessionID }!
    }

    // MARK: Showing a session clears what the detail had pushed

    @Test func sidebarRowClearsTheDetailStack() {
        let store = store()
        store.showArchive()
        store.showFromSidebar(row(one))
        #expect(store.path.isEmpty)
        #expect(store.workspace.selected == one)
        #expect(store.workspaceOpen)
    }

    @Test func tabShortcutClearsTheDetailStack() {
        let store = store()
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.workspaceOpen = false
        store.path = [.archive]
        store.showTab(number: 1)
        #expect(store.path.isEmpty)
        #expect(store.workspaceOpen)
    }

    /// In a compact window the list's stack sits under the cover and keeps its place.
    @Test func compactKeepsTheListStack() {
        let store = store(inline: false)
        store.path = [.archive]
        store.showFromSidebar(row(one))
        #expect(store.path == [.archive])
    }

    // MARK: Settings

    @Test func settingsIsASheetBesideTheSidebar() {
        let store = store()
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.showSettings()
        #expect(store.sheet == .settings)
        #expect(store.path.isEmpty)
        #expect(store.workspaceOpen)
    }

    /// ⌘, while New session is being filled in: the form stays with what was typed.
    @Test func aFormInProgressIsNotReplaced() {
        let store = store()
        store.sheet = .newSession(one.hostID)
        store.showSettings()
        #expect(store.sheet == .newSession(one.hostID))
    }

    /// The same sheet on the compact list: Settings is presented one way in every layout.
    @Test func settingsIsASheetOnTheCompactList() {
        let store = store(inline: false)
        store.showSettings()
        #expect(store.sheet == .settings)
        #expect(store.path.isEmpty)
    }

    // MARK: Edge swipe

    @Test func edgeSwipeShowsTheSidebarBesideIt() {
        let store = store()
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.sidebar.hidden = true
        store.edgeSwipe()
        #expect(!store.sidebar.isHidden)
        #expect(store.workspaceOpen)
    }

    @Test func edgeSwipeClosesTheCompactCover() {
        let store = store(inline: false)
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.edgeSwipe()
        #expect(!store.workspaceOpen)
    }

    // MARK: Who presents reviews and sign-in questions

    @Test func workspaceIsOnScreenOnlyWhenItIsTheDetail() {
        let store = store()
        #expect(!store.workspaceOnScreen)
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        #expect(store.workspaceOnScreen)
        // The monitor covers the open workspace, so the root presents for it.
        store.monitorShown = true
        #expect(!store.workspaceOnScreen)

        let compact = self.store(inline: false)
        compact.open(hostID: one.hostID, sessionID: one.sessionID)
        #expect(compact.workspaceOnScreen)
    }

    // MARK: Showing a session by id

    @Test func showingASessionRowShowsIt() {
        let store = store()
        store.monitorShown = true
        store.path = [.archive]
        store.showSession(four)
        #expect(!store.monitorShown)
        #expect(store.path.isEmpty)
        #expect(store.workspace.selected == four)
        #expect(store.workspaceOpen)
    }
}
