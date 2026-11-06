import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

/// The workspace on wide windows and on the Duo in each way it is held. One terminal fills the
/// window until the user puts a second tab beside it.
@MainActor
struct PanesSnapshotTests {
    private static let macbook = HostID(uuidString: "6F1C2D3E-0000-4000-8000-000000000003")!
    private static let hosts = [
        HostRecordView(id: macbook, name: "MacBook", symbol: .laptop, address: "macbook.local", etAvailable: true),
    ]
    private static let mosh = LinkPhase.live(transport: "Mosh", note: nil)

    private static func row(_ bib: String, _ name: String, agent: String = "Claude Code", _ status: SessionStatus,
                            cwd: String) -> SessionRow {
        Samples.row(hostID: macbook, bib: bib, name: name, agentName: agent, status: status, cwd: cwd)
    }

    private static var machine: MachineSection {
        MachineSection(
            id: macbook, name: "MacBook", symbol: .laptop, reachability: .online,
            helper: .present(version: "0.3.0", live: true),
            sessions: [
                row("01", "api", .working, cwd: "/Users/sam/src/api"),
                row("04", "app", .needsYou, cwd: "/Users/sam/src/app"),
                row("05", "docs", agent: "Codex", .needsYou, cwd: "/Users/sam/src/docs"),
            ],
            lastSeen: Samples.now
        )
    }

    private static var request: RequestCard {
        RequestCard(
            id: "r-05", hostID: macbook, sessionID: "s-05", bib: "05", agentName: "Codex",
            title: "Run the migration?", detail: nil,
            options: [ApprovalOption(n: 1, label: "Yes", role: .approve), ApprovalOption(n: 2, label: "No", role: .deny)],
            allowText: false, phase: .open
        )
    }

    /// A fresh store and pool per appearance, with 01 and 04 open and 04 beside 01 when `beside`.
    private func screen(beside: Bool, focusBeside: Bool = false, request: Bool = false) -> some View {
        let services = FakeAppServices(hosts: Self.hosts)
        services.terminalPhases = []
        let store = AppStore(
            services: services,
            controlCenter: ControlCenterState(
                requests: request ? [Self.request] : [], machines: [Self.machine], loaded: true
            )
        )
        let one = TabID(hostID: Self.macbook, sessionID: "s-01")
        let four = TabID(hostID: Self.macbook, sessionID: "s-04")
        for id in [one, four] {
            store.open(hostID: id.hostID, sessionID: id.sessionID)
        }
        store.select(one)
        if beside {
            store.openBeside(four)
        }
        store.setPanesVisible(beside)
        if focusBeside {
            store.focus(.secondary)
        }
        let terminals = TerminalPool()
        terminals.sync(with: store)
        for id in [one, four] {
            store.setLink(Self.mosh, for: id)
        }
        return WorkspaceView(store: store, terminals: terminals)
            .tint(Color.togoSnow)
    }

    @Test func duoFolded() {
        assertScreen(named: "panes", sizes: [.duoFolded]) {
            screen(beside: true)
        }
    }

    @Test func duoOpenTwoPanes() {
        assertScreen(named: "panes-two", sizes: [.duoOpen]) {
            screen(beside: true, request: true)
        }
    }

    @Test func duoOpenOnePane() {
        assertScreen(named: "panes-one", sizes: [.duoOpen]) {
            screen(beside: false)
        }
    }

    @Test func duoBook() {
        assertScreen(named: "panes-two", sizes: [.duoBook]) {
            screen(beside: true, focusBeside: true)
        }
    }

    @Test func duoTabletop() {
        assertScreen(named: "panes", sizes: [.duoTabletop]) {
            screen(beside: true, request: true)
        }
    }

    @Test func iPadTwoPanes() {
        assertScreen(named: "panes-two", sizes: [.iPadPro13]) {
            screen(beside: true, focusBeside: true, request: true)
        }
    }

    @Test func iPadOnePane() {
        assertScreen(named: "panes-one", sizes: [.iPadPro13]) {
            screen(beside: false, request: true)
        }
    }

    @Test func iPadNarrow() {
        assertScreen(named: "panes", sizes: [.iPadNarrow]) {
            screen(beside: true)
        }
    }
}
