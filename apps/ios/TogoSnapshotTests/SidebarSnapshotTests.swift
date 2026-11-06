import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

@MainActor
struct SidebarSnapshotTests {
    private static let one = TabID(hostID: Samples.studio, sessionID: "s-01")
    private static let four = TabID(hostID: Samples.studio, sessionID: "s-04")

    /// The window below the first-run screen, with `tabs` open and the first on screen. Builds
    /// a fresh store, pool and engines on every call, so each appearance renders its own.
    private func window(
        tabs: [TabID] = [], open: Bool = true, beside: TabID? = nil, sidebarShown: Bool? = nil
    ) -> some View {
        let services = FakeAppServices()
        services.terminalPhases = []
        let store = AppStore(services: services, controlCenter: Samples.controlCenter)
        for id in tabs {
            store.open(hostID: id.hostID, sessionID: id.sessionID)
        }
        if let first = tabs.first {
            store.select(first)
        }
        if let beside {
            store.setPanesVisible(true)
            store.openBeside(beside)
        }
        store.workspaceOpen = open && !tabs.isEmpty
        store.sidebar.hidden = sidebarShown.map { !$0 }
        let terminals = TerminalPool()
        terminals.sync(with: store)
        for id in tabs {
            store.setLink(.live(transport: "SSH", note: nil), for: id)
        }
        return AdaptiveRoot(store: store, terminals: terminals, now: Samples.now) { _ in EmptyView() }
            .togoControls()
    }

    @Test func sidebarBesideEmptyDetail() {
        assertScreen(named: "sidebar-empty", sizes: [.iPadPro13, .duoOpen]) {
            window()
        }
    }

    /// One large terminal is the default on a wide screen.
    @Test func sidebarBesideOneTerminal() {
        assertScreen(named: "sidebar-one-pane", sizes: [.iPadPro13, .duoOpen], precision: 0.99, perceptualPrecision: 0.95) {
            window(tabs: [Self.four, Self.one])
        }
    }

    /// The user opened a second pane and showed the sidebar again.
    @Test func sidebarBesideTwoPanes() {
        assertScreen(named: "sidebar-two-panes", sizes: [.iPadPro13], precision: 0.99, perceptualPrecision: 0.95) {
            window(tabs: [Self.one, Self.four], beside: Self.four, sidebarShown: true)
        }
    }

    @Test func bookPostureSidebarFillsTheLeadingPage() {
        assertScreen(named: "sidebar-book", sizes: [.duoBook], precision: 0.99, perceptualPrecision: 0.95) {
            window(tabs: [Self.four])
        }
    }

    /// Too narrow for a sidebar: the sessions list, with the workspace as a cover.
    @Test func narrowWindowShowsTheList() {
        assertScreen(named: "sidebar-narrow", sizes: [.iPadNarrow]) {
            window(tabs: [Self.four], open: false)
        }
    }
}
