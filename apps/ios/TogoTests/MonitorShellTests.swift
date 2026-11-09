import CoreGraphics
import Foundation
import Testing
import TogoKit
@testable import Togo

/// Monitor mode: the roster full screen as a status display, in place of a separate board.
@MainActor struct MonitorShellTests {
    private let one = TabID(hostID: Samples.studio, sessionID: "s-01")
    private let four = TabID(hostID: Samples.studio, sessionID: "s-04")

    @Test func shortcutToggles() {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        #expect(store.workspaceOpen)
        // In a compact window the monitor comes up from under the workspace.
        store.toggleMonitor()
        #expect(store.monitorShown)
        #expect(!store.workspaceOpen)
        store.toggleMonitor()
        #expect(!store.monitorShown)

        store.setWorkspaceInline(true)
        store.workspaceOpen = true
        store.toggleMonitor()
        #expect(store.monitorShown)
        #expect(store.workspaceOpen)
        #expect(ShellLayout.resolve(sizeClass: .regular, workspaceOpen: true, hasTabs: true, monitorShown: true)
            == .split(detail: .monitor))
    }

    /// A line picked on the monitor leaves it for the terminal.
    @Test func showingASessionLeavesTheMonitor() {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.setWorkspaceInline(true)
        store.monitorShown = true
        let api = Samples.controlCenter.machines.flatMap(\.sessions).first { $0.id == one.sessionID }!
        store.showFromSidebar(api)
        #expect(store.workspace.selected == one)
        #expect(store.workspace.secondary == nil)
        #expect(store.workspaceOpen)
        #expect(!store.monitorShown)
    }

    /// With the monitor across the window the workspace is not on screen, so its shortcuts wait.
    @Test func workspaceShortcutsWaitBehindTheMonitor() {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.setWorkspaceInline(true)
        store.panesFit = true
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.open(hostID: four.hostID, sessionID: four.sessionID)
        store.monitorShown = true
        #expect(!store.workspaceOnScreen)
        #expect(!store.canToggleSplit)
        store.toggleZen()
        #expect(!store.zen)
        store.monitorShown = false
        #expect(store.workspaceOnScreen)
        #expect(store.canToggleSplit)
        store.toggleZen()
        #expect(store.zen)
    }
}
