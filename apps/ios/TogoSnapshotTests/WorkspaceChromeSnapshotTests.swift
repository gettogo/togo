import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

@MainActor
/// The workspace's chrome at its edges: beside a hidden roster, and in zen.
struct WorkspaceChromeSnapshotTests {
    private static let one = TabID(hostID: Samples.studio, sessionID: "s-01")
    private static let four = TabID(hostID: Samples.studio, sessionID: "s-04")

    /// More sessions than the samples, so the roster fills a few lines.
    private static var controlCenter: ControlCenterState {
        var state = Samples.controlCenter
        state.machines[0].sessions += [
            Samples.row(bib: "06", name: "web", status: .working, activity: "Running the build", cwd: "/Users/sam/src/web"),
            Samples.row(bib: "07", name: "docs", agentName: "Codex", status: .done, activity: "Wrote the changelog", cwd: "/Users/sam/src/docs"),
        ]
        return state
    }

    private func store(tabs: [TabID] = [], sidebarHidden: Bool? = nil) -> AppStore {
        let services = FakeAppServices()
        services.terminalPhases = []
        let store = AppStore(services: services, controlCenter: Self.controlCenter)
        for id in tabs {
            store.open(hostID: id.hostID, sessionID: id.sessionID)
        }
        store.workspaceOpen = !tabs.isEmpty
        store.sidebar.hidden = sidebarHidden
        return store
    }

    private func window(_ store: AppStore) -> some View {
        let terminals = TerminalPool()
        terminals.sync(with: store)
        for tab in store.workspace.tabs {
            store.setLink(.live(transport: "SSH", note: nil), for: tab.id)
        }
        return AdaptiveRoot(store: store, terminals: terminals, now: Samples.now) { _ in EmptyView() }
            .togoControls()
    }

    /// The workspace beside a hidden roster: one header, the split view's toggle, Settings last.
    @Test func workspaceWithSidebarHidden() {
        assertScreen(named: "workspace-sidebar-hidden", sizes: [.iPadPro13], precision: 0.99, perceptualPrecision: 0.95) {
            window(store(tabs: [Self.four], sidebarHidden: true))
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
