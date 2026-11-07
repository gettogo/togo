import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

/// The roster on its own, as a phone shows it under the prompt bar: headline, tally, machines and
/// one line per session, in every state a machine or a session can be in.
@MainActor
struct RosterSnapshotTests {
    private static let macbook = HostID(uuidString: "6F1C2D3E-0000-4000-8000-000000000003")!
    private static let now = Samples.now
    private static let hosts = [
        HostRecordView(id: macbook, name: "MacBook", symbol: .laptop, address: "macbook.local", etAvailable: true),
    ] + Samples.hosts
    private static let live = HelperState.present(version: "0.3.0", live: true)

    private func screen(_ state: ControlCenterState, hosts: [HostRecordView] = hosts) -> some View {
        let store = AppStore(services: FakeAppServices(hosts: hosts), controlCenter: state)
        return NavigationStack {
            RosterView(store: store, now: Self.now)
        }
        // RootView sets these for the whole app.
        .togoControls()
    }

    private static func machine(
        _ id: HostID, _ name: String, _ symbol: HostSymbol, reachability: Reachability = .online,
        helper: HelperState = live, sessions: [SessionRow], lastSeen: Date? = now
    ) -> MachineSection {
        MachineSection(
            id: id, name: name, symbol: symbol, reachability: reachability, helper: helper, sessions: sessions,
            lastSeen: lastSeen
        )
    }

    private static func row(
        _ host: HostID, _ bib: String?, _ name: String, agent: String = "Claude Code", _ status: SessionStatus,
        _ activity: String? = nil, cwd: String, muted: Bool = false
    ) -> SessionRow {
        var row = Samples.row(hostID: host, bib: bib, name: name, agentName: agent, status: status, activity: activity, cwd: cwd)
        row.muted = muted
        return row
    }

    private static var requests: [RequestCard] {
        var test = Samples.request(id: "r1", hostID: macbook, bib: "04", title: "Run npm test?")
        test.detail = "npm test -- --watch=false"
        var edit = Samples.request(id: "r2", hostID: Samples.studio, bib: "02", title: "Edit package.json?")
        edit.agentName = "Codex"
        edit.detail = "Add \"vitest\" to devDependencies and update the test script"
        edit.options = [
            ApprovalOption(n: 1, label: "Yes", role: .approve),
            ApprovalOption(n: 2, label: "Yes, and don't ask again", role: .approveAlways),
            ApprovalOption(n: 3, label: "No", role: .deny),
        ]
        return [test, edit]
    }

    private static var macbookSessions: [SessionRow] {
        [
            row(macbook, "01", "api", .working, "Editing routes.ts", cwd: "/Users/sam/src/api"),
            row(macbook, "04", "app", .needsYou, "Wants to run npm test", cwd: "/Users/sam/src/app"),
            row(macbook, "05", "docs", .idle, "Waiting for input", cwd: "/Users/sam/src/docs", muted: true),
        ]
    }

    private static var loaded: ControlCenterState {
        ControlCenterState(
            requests: requests,
            machines: [
                machine(macbook, "MacBook", .laptop, sessions: macbookSessions),
                machine(Samples.studio, "Studio", .desktop, sessions: [
                    row(Samples.studio, "02", "site", agent: "Codex", .needsYou, "Wants to edit package.json",
                        cwd: "/Users/sam/src/site"),
                    row(Samples.studio, "03", "notes", .done, "Wrote the summary", cwd: "/Users/sam/notes"),
                ]),
                machine(Samples.build, "Build box", .server, sessions: [
                    row(Samples.build, "06", "release", agent: "Codex", .working, "Running the release build",
                        cwd: "/home/sam/release"),
                    row(Samples.build, nil, "scratch", agent: "Shell", .idle, cwd: "/home/sam"),
                ]),
            ],
            archived: [row(Samples.build, "07", "ci-fix", .done, cwd: "/home/sam/ci")],
            loaded: true
        )
    }

    @Test func firstRun() {
        assertScreen(screen(ControlCenterState(), hosts: []), named: "roster-first-run")
    }

    @Test func loading() {
        assertScreen(screen(ControlCenterState()), named: "roster-loading")
    }

    @Test func loaded() {
        assertScreen(screen(Self.loaded), named: "roster-loaded")
    }

    /// Stopped agents say how they stopped, never "Done", and in the row's usual muted words.
    @Test func stoppedSessions() {
        func stopped(_ bib: String, _ name: String, _ status: SessionStatus, _ exit: SessionExit) -> SessionRow {
            var row = Self.row(Self.macbook, bib, name, status, cwd: "/Users/sam/src/\(name)")
            row.exit = exit
            return row
        }
        let state = ControlCenterState(machines: [
            Self.machine(Self.macbook, "MacBook", .laptop, sessions: [
                stopped("01", "api", .failed, SessionExit(code: 1, reason: .exit)),
                stopped("02", "site", .ended, SessionExit(code: 0, reason: .exit)),
                stopped("03", "notes", .failed, SessionExit(code: nil, reason: .usageLimit, detail: "Usage limit reached")),
                stopped("04", "app", .ended, SessionExit(code: nil, reason: .restart)),
                Self.row(Self.macbook, "05", "docs", .done, "Wrote the summary", cwd: "/Users/sam/src/docs"),
            ]),
        ], loaded: true)
        assertScreen(screen(state, hosts: Array(Self.hosts.prefix(1))), named: "roster-stopped")
    }

    /// Sessions a restart ended sit under the live ones, each with its way back.
    @Test func restorableSessions() {
        func ended(_ bib: String, _ name: String, resumable: Bool) -> RestorableSession {
            RestorableSession(
                id: "s_\(name)", bib: bib, name: name, agent: "claude", command: ["claude"],
                cwd: "/Users/sam/src/\(name)", createdAt: Self.now, endedAt: Self.now, resumable: resumable
            )
        }
        var section = Self.machine(Self.macbook, "MacBook", .laptop, sessions: [
            Self.row(Self.macbook, "03", "api", .working, "Editing routes.ts", cwd: "/Users/sam/src/api"),
        ])
        section.restorable = [ended("01", "app", resumable: true), ended("02", "site", resumable: false)]
        let state = ControlCenterState(machines: [section], loaded: true)
        assertScreen(screen(state, hosts: Array(Self.hosts.prefix(1))), named: "roster-restorable")
    }

    /// With nothing live, the restorable rows are the first thing in the list at large text.
    @Test func restorableSessionsOnly() {
        var section = Self.machine(Self.macbook, "MacBook", .laptop, sessions: [])
        section.restorable = [RestorableSession(
            id: "s_app", bib: "01", name: "app", agent: "claude", command: ["claude"], cwd: "/Users/sam/src/app",
            createdAt: Self.now, endedAt: Self.now, resumable: true
        )]
        let state = ControlCenterState(machines: [section], loaded: true)
        assertScreen(screen(state, hosts: Array(Self.hosts.prefix(1))), named: "roster-restorable-only")
    }

    /// The row alone, so the stacked buttons at the largest text size are on record.
    @Test func restorableRow() {
        let session = RestorableSession(
            id: "s_app", bib: "01", name: "app", agent: "claude", command: ["claude"], cwd: "/Users/sam/src/app",
            createdAt: Self.now, endedAt: Self.now, resumable: true
        )
        assertScreen(
            RestorableSessionRow(session: session) { _ in }
                .padding(20)
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading),
            named: "restorable-row"
        )
    }

    @Test func emptyMachine() {
        let state = ControlCenterState(machines: [
            Self.machine(Self.macbook, "MacBook", .laptop, sessions: []),
        ], loaded: true)
        assertScreen(screen(state, hosts: Array(Self.hosts.prefix(1))), named: "roster-empty-machine")
    }

    @Test func offline() {
        var state = Self.loaded
        state.requests = []
        state.machines[0].reachability = .offline(since: Self.now.addingTimeInterval(-720), reason: "Asleep")
        state.machines[0].lastSeen = Self.now.addingTimeInterval(-720)
        assertScreen(screen(state), named: "roster-offline")
    }

    @Test func wakingAndConnecting() {
        var state = Self.loaded
        state.requests = []
        state.machines[0].reachability = .waking
        state.machines[1].reachability = .connecting
        state.machines[1].sessions = []
        assertScreen(screen(state), named: "roster-waking")
    }

    @Test func error() {
        var state = Self.loaded
        state.requests = []
        state.machines[0].reachability = .authNeeded
        state.machines[1].reachability = .failed("The connection was refused on port 22.")
        state.machines[1].sessions = []
        assertScreen(screen(state), named: "roster-error")
    }

    @Test func keyMissing() {
        var state = Self.loaded
        state.requests = []
        state.machines[1].reachability = .keyMissing
        assertScreen(screen(state), named: "roster-key-missing")
    }

    @Test func noHelper() {
        let state = ControlCenterState(machines: [
            Self.machine(Self.macbook, "MacBook", .laptop, helper: .missing, sessions: [
                Self.row(Self.macbook, nil, "togo-1", agent: "Shell", .idle, cwd: "/Users/sam"),
                Self.row(Self.macbook, nil, "work", agent: "Shell", .idle, cwd: "/Users/sam/src"),
            ]),
        ], loaded: true)
        assertScreen(screen(state, hosts: Array(Self.hosts.prefix(1))), named: "roster-no-helper")
    }

    @Test func archive() {
        let state = Self.loaded
        let archived = [
            Self.row(Self.macbook, "08", "migrate", .done, "Moved the tables to the new schema", cwd: "/Users/sam/src/db"),
            Self.row(Self.macbook, "09", "lint", agent: "Codex", .exited, cwd: "/Users/sam/src/app"),
            Self.row(Samples.build, "07", "ci-fix", .done, cwd: "/home/sam/ci"),
        ]
        let view = NavigationStack {
            ArchiveView(rows: archived, machines: state.machines) { _ in }
        }
        .togoControls()
        assertScreen(view, named: "roster-archive")
    }

    /// One row per screen, so each case is in view even at the largest text size.
    @Test func longNamesAX5() {
        let long = String(String(repeating: "refactor-the-session-monitor-", count: 3).prefix(64))
        let path = "/Users/sam/" + Array(repeating: "deeply-nested-folder", count: 9).joined(separator: "/") + "/end"
        let rows: [(name: String, row: SessionRow)] = [
            ("roster-long-name", Self.row(Self.macbook, "01", long, .needsYou, "Wants to run the migration on production data",
                                      cwd: "/Users/sam/src/app", muted: true)),
            ("roster-rtl-name", Self.row(Self.macbook, "02", "مراجعة الكود قبل الدمج", .working, "Reading the diff",
                                     cwd: "/Users/sam/src/ar")),
            ("roster-long-path", Self.row(Self.macbook, "03", "paths", .idle, cwd: String(path.prefix(200)))),
        ]
        for (name, row) in rows {
            let state = ControlCenterState(machines: [Self.machine(Self.macbook, "MacBook", .laptop, sessions: [row])],
                                           loaded: true)
            assertScreen(screen(state, hosts: Array(Self.hosts.prefix(1))), named: name)
        }
    }

    /// The whole phone window: the roster with the bar at its foot holding the oldest request.
    @Test func phoneWithTheBar() {
        assertScreen(named: "roster-phone", sizes: [.iPhone17]) { phoneWindow() }
    }

    /// A fresh store and pool on every call, so each appearance renders its own.
    private func phoneWindow() -> some View {
        let services = FakeAppServices(hosts: Self.hosts)
        services.terminalPhases = []
        let store = AppStore(services: services, controlCenter: Self.loaded)
        let terminals = TerminalPool()
        terminals.sync(with: store)
        return AdaptiveRoot(store: store, terminals: terminals, now: Self.now) { _ in EmptyView() }
            .readsFold()
            .togoControls()
    }

    /// All clear: the headline unlit and the bar back to its start line.
    @Test func allClear() {
        var state = Self.loaded
        state.requests = []
        state.machines = state.machines.map { machine in
            var machine = machine
            machine.sessions = machine.sessions.map { row in
                var row = row
                if row.status == .needsYou { row.status = .working }
                return row
            }
            return machine
        }
        assertScreen(screen(state), named: "roster-all-clear")
    }
}
