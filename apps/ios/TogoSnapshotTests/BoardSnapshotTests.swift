import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

@MainActor
struct BoardSnapshotTests {
    private static let one = TabID(hostID: Samples.studio, sessionID: "s-01")
    private static let four = TabID(hostID: Samples.studio, sessionID: "s-04")

    final class StillTimer: IdleTimerControl {
        var isIdleTimerDisabled = false
    }

    final class FullBattery: BatterySource {
        var level: Float = 1
        var isPluggedIn = true
    }

    /// More sessions than the samples, so the board fills a few rows.
    private static var controlCenter: ControlCenterState {
        var state = Samples.controlCenter
        state.machines[0].capabilities = [Capability.capture]
        state.machines[0].sessions += [
            Samples.row(bib: "06", name: "web", status: .working, activity: "Running the build", cwd: "/Users/sam/src/web"),
            Samples.row(bib: "07", name: "docs", agentName: "Codex", status: .done, activity: "Wrote the changelog", cwd: "/Users/sam/src/docs"),
        ]
        return state
    }

    private static let screen = [
        "  Running npm test",
        "    42 passing (1.8s)",
        "    1 failing",
        "",
        "    1) routes: returns 404 for unknown ids",
        "       expected 404, got 500",
        "",
        "  Reading src/routes.ts",
        "  The handler throws before it checks the id. Moving the lookup",
        "  into the try block and returning 404 when it comes back empty.",
        "",
        "  Editing src/routes.ts",
    ].joined(separator: "\n")

    private func services() -> BoardServices {
        let thumbnails = ThumbnailModel(capture: { _ in Self.screen }, canCapture: { $0.hostID == Samples.studio })
        for row in Self.controlCenter.machines[0].sessions {
            thumbnails.show(Self.screen, for: TabID(hostID: row.hostID, sessionID: row.id))
        }
        return BoardServices(idleTimer: StillTimer(), battery: FullBattery(), thumbnails: thumbnails)
    }

    private func store(boardShown: Bool = true, tabs: [TabID] = [], sidebarHidden: Bool? = nil) -> AppStore {
        let services = FakeAppServices()
        services.terminalPhases = []
        let store = AppStore(services: services, controlCenter: Self.controlCenter)
        for id in tabs {
            store.open(hostID: id.hostID, sessionID: id.sessionID)
        }
        store.workspaceOpen = !tabs.isEmpty
        store.boardShown = boardShown
        store.sidebar.hidden = sidebarHidden
        return store
    }

    private func window(_ store: AppStore, board: BoardServices) -> some View {
        let terminals = TerminalPool()
        terminals.sync(with: store)
        for tab in store.workspace.tabs {
            store.setLink(.live(transport: "SSH", note: nil), for: tab.id)
        }
        return AdaptiveRoot(store: store, terminals: terminals, now: Samples.now, board: board) { _ in EmptyView() }
            .togoControls()
    }

    /// Beside the sidebar on an iPad and an open Duo, where the fold rounds the columns to two.
    @Test func boardBesideSidebar() {
        assertScreen(named: "board", sizes: [.iPadPro13, .duoOpen]) {
            window(store(), board: services())
        }
    }

    /// The sidebar hidden on an open Duo: three cards would fit, two sit either side of the fold.
    @Test func boardAcrossTheFold() {
        assertScreen(named: "board-fold", sizes: [.duoOpen]) {
            window(store(sidebarHidden: true), board: services())
        }
    }

    /// Held as a book with the sidebar hidden: the bent fold is the middle gap, with no card on it.
    @Test func boardInBookPosture() {
        assertScreen(named: "board-book", sizes: [.duoBook]) {
            window(store(sidebarHidden: true), board: services())
        }
    }

    /// In a compact window the board takes the place of the list.
    @Test func boardInCompactWindow() {
        assertScreen(named: "board-compact", sizes: [.iPhone17, .duoFolded]) {
            window(store(), board: services())
        }
    }

    /// Ten minutes without a touch while the screen is kept on.
    @Test func boardDimmed() {
        assertScreen(named: "board-dimmed", sizes: [.iPadPro13]) {
            let board = services()
            let monitor = MonitorMode(enabled: true, idleTimer: board.idleTimer, battery: board.battery, now: Samples.now)
            monitor.freezeDimmed()
            return NavigationStack {
                BoardView(store: store(), services: board, monitor: monitor)
            }
            .togoControls()
        }
    }

    /// The workspace beside a hidden sidebar: one header, with the button that shows the sidebar.
    @Test func workspaceWithSidebarHidden() {
        assertScreen(named: "workspace-sidebar-hidden", sizes: [.iPadPro13], precision: 0.99, perceptualPrecision: 0.95) {
            window(store(boardShown: false, tabs: [Self.four], sidebarHidden: true), board: services())
        }
    }

    /// Zen: no header, tab strip or compose bar; a request still drops in.
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
