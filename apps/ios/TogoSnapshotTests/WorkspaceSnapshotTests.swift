import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

@MainActor
struct WorkspaceSnapshotTests {
    private static let macbook = HostID(uuidString: "6F1C2D3E-0000-4000-8000-000000000003")!
    private static let hosts = [
        HostRecordView(id: macbook, name: "MacBook", symbol: .laptop, address: "macbook.local", etAvailable: true),
    ]
    private static let mosh = LinkPhase.live(transport: "Mosh", note: nil)

    private static func row(_ bib: String, _ name: String, agent: String = "Claude Code", _ status: SessionStatus,
                            cwd: String) -> SessionRow {
        Samples.row(hostID: macbook, bib: bib, name: name, agentName: agent, status: status, cwd: cwd)
    }

    private static func machine(_ reachability: Reachability) -> MachineSection {
        var failed = row("06", "tests", .failed, cwd: "/Users/sam/src/tests")
        failed.exit = SessionExit(code: 1, reason: .exit)
        return MachineSection(
            id: macbook, name: "MacBook", symbol: .laptop, reachability: reachability,
            helper: .present(version: "0.3.0", live: true),
            sessions: [
                row("01", "api", .working, cwd: "/Users/sam/src/api"),
                row("04", "app", .needsYou, cwd: "/Users/sam/src/app"),
                row("05", "docs", agent: "Codex", .idle, cwd: "/Users/sam/src/docs"),
                failed,
            ],
            lastSeen: Samples.now,
            capabilities: [Capability.events, Capability.launch, Capability.lifecycle]
        )
    }

    /// Builds a fresh store, pool and engine on every call, so each appearance renders its own.
    private func screen(
        tabs: [String] = ["s-04"], selected: String = "s-04", link: LinkPhase = mosh, requests: [RequestCard] = [],
        reachability: Reachability = .online, noteShown: Bool = false, shownElsewhere: Bool = false
    ) -> some View {
        let services = FakeAppServices(hosts: Self.hosts)
        services.terminalPhases = []
        let store = AppStore(
            services: services,
            controlCenter: ControlCenterState(requests: requests, machines: [Self.machine(reachability)], loaded: true)
        )
        for id in tabs {
            store.open(hostID: Self.macbook, sessionID: id)
        }
        store.select(TabID(hostID: Self.macbook, sessionID: selected))
        let terminals = TerminalPool()
        terminals.sync(with: store)
        if shownElsewhere {
            let other = WindowID()
            terminals.sync(window: other, with: store)
            terminals.show(TabID(hostID: Self.macbook, sessionID: selected), in: other)
        }
        for id in tabs {
            store.setLink(id == selected ? link : Self.mosh, for: TabID(hostID: Self.macbook, sessionID: id))
        }
        // RootView sets this tint for the whole app.
        return WorkspaceView(store: store, terminals: terminals, noteShown: noteShown)
            .tint(Color.togoSnow)
    }

    @Test func oneTabLive() {
        assertScreen(named: "one-tab") {
            screen()
        }
    }

    /// The agent stopped: the header says how, and Run again sits beside the other buttons.
    @Test func stoppedSession() {
        assertScreen(named: "stopped") {
            screen(tabs: ["s-06"], selected: "s-06")
        }
    }

    @Test func threeTabs() {
        assertScreen(named: "three-tabs") {
            screen(tabs: ["s-01", "s-04", "s-05"], selected: "s-04")
        }
    }

    @Test func shownInAnotherWindow() {
        assertScreen(named: "elsewhere") {
            screen(shownElsewhere: true)
        }
    }

    @Test func connecting() {
        assertScreen(named: "connecting") {
            screen(link: .connecting)
        }
    }

    @Test func waking() {
        assertScreen(named: "waking") {
            screen(link: .waking("MacBook"))
        }
    }

    @Test func reconnectingDimsTheTerminal() {
        assertScreen(named: "reconnecting") {
            screen(link: .reconnecting(2))
        }
    }

    @Test func failed() {
        assertScreen(named: "failed") {
            screen(link: .failed("Connection refused on port 22"))
        }
    }

    @Test func signedOut() {
        assertScreen(named: "signed-out") {
            screen(link: .signedOut)
        }
    }

    @Test func offlineMachine() {
        assertScreen(named: "offline") {
            screen(link: .failed("Timed out"), reachability: .offline(since: Samples.now, reason: "Asleep"))
        }
    }

    @Test func fallbackNoteShown() {
        assertScreen(named: "fallback-note") {
            screen(link: .live(transport: "SSH", note: "mosh-server not found on mini"), noteShown: true)
        }
    }

    @Test func incomingRequest() {
        var test = Samples.request(id: "r1", hostID: Self.macbook, bib: "01", title: "Run npm test?")
        test.detail = "npm test -- --watch=false"
        let others = [
            Samples.request(id: "r2", hostID: Self.macbook, bib: "05", title: "Edit README.md?"),
            Samples.request(id: "r3", hostID: Self.macbook, bib: "04", title: "Delete dist/?"),
            Samples.request(id: "r4", hostID: Self.macbook, bib: nil, title: "Allow network access?"),
        ]
        assertScreen(named: "incoming") {
            screen(tabs: ["s-01", "s-04", "s-05"], selected: "s-04", requests: [test] + others)
        }
    }
}
