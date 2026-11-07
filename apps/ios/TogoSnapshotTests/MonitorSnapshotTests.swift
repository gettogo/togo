import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

@MainActor
/// Monitor mode, the roster full screen as a status display, and the workspace views that used
/// to sit beside the board.
struct MonitorSnapshotTests {
    private static let one = TabID(hostID: Samples.studio, sessionID: "s-01")
    private static let four = TabID(hostID: Samples.studio, sessionID: "s-04")

    final class StillTimer: IdleTimerControl {
        var isIdleTimerDisabled = false
    }

    final class FullBattery: BatterySource {
        var level: Float = 1
        var isPluggedIn = true
    }

    /// More sessions than the samples, so the roster fills a few lines.
    private static var controlCenter: ControlCenterState {
        var state = Samples.controlCenter
        state.machines[0].sessions += [
            Samples.row(bib: "06", name: "web", status: .working, activity: "Running the build", cwd: "/Users/sam/src/web"),
            Samples.row(bib: "07", name: "docs", agentName: "Codex", status: .done, activity: "Wrote the changelog", cwd: "/Users/sam/src/docs"),
        ]
        return state
    }

    private func services() -> MonitorServices {
        MonitorServices(idleTimer: StillTimer(), battery: FullBattery())
    }

    private func store(monitorShown: Bool = true, tabs: [TabID] = [], sidebarHidden: Bool? = nil) -> AppStore {
        let services = FakeAppServices()
        services.terminalPhases = []
        let store = AppStore(services: services, controlCenter: Self.controlCenter)
        for id in tabs {
            store.open(hostID: id.hostID, sessionID: id.sessionID)
        }
        store.workspaceOpen = !tabs.isEmpty
        store.monitorShown = monitorShown
        store.sidebar.hidden = sidebarHidden
        return store
    }

    private func window(_ store: AppStore, board: MonitorServices) -> some View {
        let terminals = TerminalPool()
        terminals.sync(with: store)
        for tab in store.workspace.tabs {
            store.setLink(.live(transport: "SSH", note: nil), for: tab.id)
        }
        return AdaptiveRoot(store: store, terminals: terminals, now: Samples.now, monitor: board) { _ in EmptyView() }
            .togoControls()
    }

    /// Across the whole window on an iPad and an open Duo, with no terminal column.
    @Test func monitorAcrossTheWindow() {
        assertScreen(named: "monitor", sizes: [.iPadPro13, .duoOpen]) {
            window(store(), board: services())
        }
    }

    /// Held as a book: the roster runs across the fold like any list.
    @Test func monitorInBookPosture() {
        assertScreen(named: "monitor-book", sizes: [.duoBook]) {
            window(store(), board: services())
        }
    }

    /// In a compact window the monitor takes the roster's place.
    @Test func monitorInCompactWindow() {
        assertScreen(named: "monitor-compact", sizes: [.iPhone17, .duoFolded]) {
            window(store(), board: services())
        }
    }

    /// Ten minutes without a touch while the screen is kept on.
    @Test func monitorDimmed() {
        assertScreen(named: "monitor-dimmed", sizes: [.iPadPro13]) {
            let board = services()
            let monitor = MonitorMode(enabled: true, idleTimer: board.idleTimer, battery: board.battery, now: Samples.now)
            monitor.freezeDimmed()
            return NavigationStack {
                RosterMonitor(store: store(), services: board, monitor: monitor, now: Samples.now)
            }
            .togoControls()
        }
    }

    /// The workspace beside a hidden roster: one header, with the button that shows the roster.
    @Test func workspaceWithSidebarHidden() {
        assertScreen(named: "workspace-sidebar-hidden", sizes: [.iPadPro13], precision: 0.99, perceptualPrecision: 0.95) {
            window(store(monitorShown: false, tabs: [Self.four], sidebarHidden: true), board: services())
        }
    }

    /// Zen: no header, tab strip or compose bar.
    @Test func zenWorkspace() {
        assertScreen(named: "zen-workspace", sizes: [.iPhone17, .duoFolded], precision: 0.99, perceptualPrecision: 0.95) {
            let services = FakeAppServices()
            services.terminalPhases = []
            var state = Self.controlCenter
            state.requests = [Samples.request(id: "r-01", bib: "01", title: "Edit src/routes.ts?")]
            let store = AppStore(services: services, controlCenter: state)
            store.open(hostID: Self.four.hostID, sessionID: Self.four.sessionID)
            store.open(hostID: Self.one.hostID, sessionID: Self.one.sessionID)
            store.select(Self.four)
            store.zen = true
            let terminals = TerminalPool()
            terminals.sync(with: store)
            store.setLink(.live(transport: "SSH", note: nil), for: Self.four)
            store.setLink(.live(transport: "SSH", note: nil), for: Self.one)
            return WorkspaceView(store: store, terminals: terminals)
                .tint(Color.togoSnow)
        }
    }
}
