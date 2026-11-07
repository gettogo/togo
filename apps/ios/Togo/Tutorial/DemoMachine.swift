import SwiftUI
import TogoKit
import UIKit

/// Services for the demo machine. There is nothing behind them: no host, no address, no
/// connection. Every action is refused and noted in `attempts`, and nothing is ever saved, so
/// the demo cannot reach a network or a synced store.
@MainActor final class DemoServices: AppServices {
    enum Attempt: Equatable, Sendable {
        case answer, kill, rename, setMute, create, agents, recentDirs, listDir, terminal, capture, upload, saveHost, removeHost, signIn
    }

    struct ReadOnly: LocalizedError {
        var errorDescription: String? {
            String(localized: "tutorial.demo.readOnly", defaultValue: "This is a demo, so nothing here changes.")
        }
    }

    let hosts: [HostRecordView] = [DemoMachine.host]
    var settings = AppSettings()
    private(set) var attempts: [Attempt] = []

    func snapshots() -> AsyncStream<HostViewSnapshot> {
        AsyncStream { $0.finish() }
    }

    func answer(hostID: HostID, requestID: String, decision: Decision) async throws { try refuse(.answer) }
    func kill(hostID: HostID, sessionID: String) async throws { try refuse(.kill) }
    func rename(hostID: HostID, sessionID: String, name: String) async throws { try refuse(.rename) }
    func setMute(hostID: HostID, sessionID: String, muted: Bool) async throws { try refuse(.setMute) }
    func create(hostID: HostID, params: CreateSessionParams) async throws -> SessionRow { try refuse(.create) }
    func agents(hostID: HostID) async throws -> [AgentInfo] { try refuse(.agents) }
    func recentDirs(hostID: HostID) async throws -> [String] { try refuse(.recentDirs) }
    func listDir(hostID: HostID, path: String) async throws -> ListDirResult { try refuse(.listDir) }
    func capture(hostID: HostID, sessionID: String, lines: Int) async throws -> String { try refuse(.capture) }
    func upload(hostID: HostID, sessionID: String, name: String, data: Data) async throws -> String { try refuse(.upload) }
    func signIn(hostID: HostID, prompter: any ConnectionPrompter) async throws { try refuse(.signIn) }
    func saveHost(_ record: HostRecord) async throws { try refuse(.saveHost) }
    func removeHost(_ id: HostID) async throws { try refuse(.removeHost) }

    func terminal(hostID: HostID, sessionID: String, transport: TransportPreference?, userStarted: Bool) -> TerminalHandle {
        attempts.append(.terminal)
        let phases = AsyncStream<LinkPhase> { continuation in
            continuation.yield(.failed(ReadOnly().errorDescription ?? ""))
            continuation.finish()
        }
        return TerminalHandle(engineView: UIViewController(), phases: phases, onStop: {})
    }

    private func refuse(_ attempt: Attempt) throws -> Never {
        attempts.append(attempt)
        throw ReadOnly()
    }
}

/// The machine "Explore with a demo machine" shows: a few sessions and one request, enough to
/// see what the control center looks like with the helper.
@MainActor enum DemoMachine {
    static let id = HostID(uuidString: "D3E30000-0000-4000-8000-00000000DE30")!
    static let name = String(localized: "tutorial.demo.machine", defaultValue: "Demo")
    static let host = HostRecordView(id: id, name: name, symbol: .laptop, address: "")

    /// A store over `DemoServices` that already shows the demo machine.
    static func store(now: Date = .now) -> AppStore {
        let store = AppStore(services: DemoServices())
        store.apply(snapshot(now: now))
        store.notice = Notice(
            text: String(
                localized: "tutorial.demo.notice",
                defaultValue: "This is a demo machine. It never connects anywhere, and nothing you do here is saved."
            ),
            isError: false
        )
        return store
    }

    static func snapshot(now: Date) -> HostViewSnapshot {
        HostViewSnapshot(
            hostID: id, reachability: .online, helper: .present(version: "0.3.0", live: true),
            sessions: [
                row("01", "api", "Claude Code", .working, "Editing routes.ts", "~/src/api", now.addingTimeInterval(-240)),
                row("04", "app", "Claude Code", .needsYou, "Wants to run npm test", "~/src/app", now.addingTimeInterval(-60)),
                row("07", "docs", "Codex", .done, "Updated the README", "~/src/docs", now.addingTimeInterval(-1_500)),
                row(nil, "scratch", "Shell", .idle, nil, "~", now.addingTimeInterval(-3_600)),
            ],
            requests: [
                RequestCard(
                    id: "demo-request", hostID: id, sessionID: "demo-04", bib: "04", agentName: "Claude Code",
                    title: "Run npm test?", detail: "npm test -- --watch=false",
                    options: [
                        ApprovalOption(n: 1, label: "Yes", role: .approve),
                        ApprovalOption(n: 2, label: "No", role: .deny),
                    ],
                    allowText: false, phase: .open
                ),
            ],
            at: now
        )
    }

    private static func row(
        _ bib: String?, _ name: String, _ agentName: String, _ status: SessionStatus, _ activity: String?,
        _ cwd: String, _ since: Date
    ) -> SessionRow {
        SessionRow(
            id: "demo-\(bib ?? name)", hostID: id, bib: bib, name: name, agent: AgentCatalog.entries.first { $0.name == agentName }?.id ?? agentName.lowercased(),
            agentName: agentName, status: status, activity: activity, cwd: cwd, muted: false,
            managed: bib != nil, meta: SessionMeta(), statusSince: since
        )
    }
}

/// The demo machine's control center, full screen over the guide. Places the demo cannot show,
/// such as settings or a new session, say so instead of opening.
struct DemoControlCenter: View {
    @State private var store: AppStore
    let now: Date?
    let onDone: () -> Void

    init(store: AppStore, now: Date? = nil, onDone: @escaping () -> Void) {
        _store = State(initialValue: store)
        self.now = now
        self.onDone = onDone
    }

    var body: some View {
        NavigationStack(path: $store.path) {
            RosterView(store: store, now: now)
                .toolbar {
                    ToolbarItem(placement: .topBarLeading) {
                        Button(String(localized: "tutorial.demo.done", defaultValue: "Done"), systemImage: "checkmark", role: .confirm, action: onDone)
                            .foregroundStyle(Color.togoSnow)
                    }
                }
                .navigationDestination(for: AppRoute.self) { _ in
                    unavailable
                }
        }
        .presentSheet("tutorial.demoSheet", item: $store.sheet) { _ in
            NavigationStack {
                unavailable
                    .toolbar {
                        ToolbarItem(placement: .cancellationAction) {
                            Button(String(localized: "tutorial.demo.close", defaultValue: "Close"), systemImage: "xmark", role: .close) { store.sheet = nil }
                        }
                    }
            }
            .presentationDetents([.medium])
        }
        .presentCover("tutorial.demoWorkspace", isPresented: $store.workspaceOpen) {
            NavigationStack {
                unavailable
                    .toolbar {
                        ToolbarItem(placement: .cancellationAction) {
                            Button(String(localized: "tutorial.demo.close", defaultValue: "Close"), systemImage: "xmark", role: .close) { store.workspaceOpen = false }
                        }
                    }
            }
        }
        .togoControls()
    }

    private var unavailable: some View {
        StateView(
            kind: .empty,
            title: String(localized: "tutorial.demo.unavailable.title", defaultValue: "Not in the demo"),
            message: String(
                localized: "tutorial.demo.unavailable.message",
                defaultValue: "Add a machine to open sessions, start agents and change settings."
            )
        )
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.togoInk)
    }
}
