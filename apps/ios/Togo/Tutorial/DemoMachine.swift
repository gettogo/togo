import SwiftUI
import TogoKit
import UIKit

/// Services for the demo machine. There is nothing behind them: no host, no address, no
/// connection. The sessions, chat, terminal and review play a recorded session from the app's
/// own resources. Renaming and muting change only what this object holds; anything that would
/// reach a machine, the keychain or a synced store is refused and noted in `attempts`. Nothing
/// is saved.
@MainActor final class DemoServices: AppServices {
    enum Attempt: Equatable, Sendable {
        case answer, kill, rename, setMute, create, agents, recentDirs, listDir, terminal, capture, upload, saveHost, removeHost, signIn
    }

    struct ReadOnly: LocalizedError {
        var errorDescription: String? {
            String(localized: "tutorial.demo.readOnly", defaultValue: "This is a demo, so nothing here changes.")
        }
    }

    static let shellID = "demo-shell"

    let hosts: [HostRecordView] = [DemoMachine.host]
    /// Kept for this visit only.
    var settings = AppSettings()
    private(set) var attempts: [Attempt] = []
    let script: DemoScript?
    let project: DemoProject?
    private let now: () -> Date
    private let opened: Date
    private var names: [String: String] = [:]
    private var muted: Set<String> = []
    private var since: [String: (status: SessionStatus, at: Date)] = [:]
    private var subscribers: [UUID: AsyncStream<HostViewSnapshot>.Continuation] = [:]
    /// The session's open requests, as the helper offers OpenCode's permissions: Allow once
    /// and Deny, from the sessions list, the Lock Screen or the Watch.
    private var requests: [RequestCard] = []
    private var terminals: [ObjectIdentifier: DemoTerminal] = [:]

    init(script: DemoScript?, project: DemoProject?, now: @escaping () -> Date = { .now }) {
        self.script = script
        self.project = project
        self.now = now
        opened = now()
        // One hook for the visit: the script's change reaches the list and every terminal open
        // now, however often the terminal tab was opened before.
        script?.onChange.append { [weak self] in
            self?.publish()
            self?.redrawTerminals()
        }
        script?.replay.onApproval = { [weak self] change, _ in self?.approval(change) }
    }

    private func redrawTerminals() {
        for terminal in terminals.values { terminal.redraw() }
    }

    private func approval(_ change: ReplayHarnessServices.ApprovalChange) {
        switch change {
        case .open(let approval):
            requests.removeAll { $0.id == approval.requestId }
            requests.append(RequestCard(
                id: approval.requestId, hostID: DemoMachine.id, sessionID: DemoScript.sessionID, bib: "01",
                agentName: script?.replay.script.agent?.name ?? "OpenCode", title: approval.title, detail: approval.detail,
                options: approval.options, allowText: approval.allowText, phase: .open
            ))
        case .closed(let requestID):
            requests.removeAll { $0.id == requestID }
        }
        publish()
    }

    // MARK: The machine

    static let capabilities: Set<String> = [
        Capability.events, Capability.mute, Capability.git, Capability.gitWrite, Capability.gitFiles,
        Capability.ports, Capability.capture, Capability.harness,
    ]

    func snapshot() -> HostViewSnapshot {
        var rows: [SessionRow] = []
        if let script { rows.append(script.row(hostID: DemoMachine.id, since: opened)) }
        rows.append(SessionRow(
            id: Self.shellID, hostID: DemoMachine.id, bib: nil, name: "shell", agent: "shell", agentName: "Shell",
            status: .idle, activity: nil, cwd: "~/status", muted: false, managed: false, meta: SessionMeta(),
            statusSince: opened.addingTimeInterval(-3_600)
        ))
        for index in rows.indices {
            let id = rows[index].id
            if let name = names[id] { rows[index].name = name }
            rows[index].muted = muted.contains(id)
            if let last = since[id], last.status == rows[index].status {
                rows[index].statusSince = last.at
            } else if since[id] != nil {
                since[id] = (rows[index].status, now())
                rows[index].statusSince = now()
            } else {
                since[id] = (rows[index].status, rows[index].statusSince)
            }
        }
        return HostViewSnapshot(
            hostID: DemoMachine.id, reachability: .online, helper: .present(version: "0.3.0", live: true),
            sessions: rows, requests: requests, at: now(), capabilities: Self.capabilities
        )
    }

    func snapshots() -> AsyncStream<HostViewSnapshot> {
        let (stream, continuation) = AsyncStream.makeStream(of: HostViewSnapshot.self)
        let id = UUID()
        subscribers[id] = continuation
        continuation.onTermination = { [weak self] _ in Task { @MainActor in self?.subscribers[id] = nil } }
        continuation.yield(snapshot())
        return stream
    }

    private func publish() {
        let latest = snapshot()
        for continuation in subscribers.values { continuation.yield(latest) }
    }

    // MARK: Sessions

    /// A request answered from the sessions list plays the recorded answer, as the helper
    /// answers the agent; anything else is refused.
    func answer(hostID: HostID, requestID: String, decision: Decision) async throws {
        guard requests.contains(where: { $0.id == requestID }), let script else { try refuse(.answer) }
        attempts.append(.answer)
        requests.removeAll { $0.id == requestID }
        publish()
        script.replay.trigger("approval.answer")
    }
    func kill(hostID: HostID, sessionID: String) async throws { try refuse(.kill) }
    func create(hostID: HostID, params: CreateSessionParams) async throws -> SessionRow { try refuse(.create) }
    func upload(hostID: HostID, sessionID: String, name: String, data: Data) async throws -> String { try refuse(.upload) }
    func signIn(hostID: HostID, prompter: any ConnectionPrompter) async throws { try refuse(.signIn) }
    func saveHost(_ record: HostRecord) async throws { try refuse(.saveHost) }
    func removeHost(_ id: HostID) async throws { try refuse(.removeHost) }

    func rename(hostID: HostID, sessionID: String, name: String) async throws {
        attempts.append(.rename)
        names[sessionID] = name
        publish()
    }

    func setMute(hostID: HostID, sessionID: String, muted on: Bool) async throws {
        attempts.append(.setMute)
        if on { muted.insert(sessionID) } else { muted.remove(sessionID) }
        publish()
    }

    /// Every agent the app knows, as a Mac with all of them installed would list them.
    func agents(hostID: HostID) async throws -> [AgentInfo] {
        attempts.append(.agents)
        return AgentCatalog.entries.map { AgentInfo(id: $0.id, name: $0.name, installed: true) }
    }

    func recentDirs(hostID: HostID) async throws -> [String] {
        attempts.append(.recentDirs)
        return [project?.root ?? DemoMachine.home + "/status"]
    }

    /// The home folder holds the project; the project lists its files.
    func listDir(hostID: HostID, path: String) async throws -> ListDirResult {
        attempts.append(.listDir)
        let home = DemoMachine.home
        if ["", "~", "~/", home, home + "/"].contains(path) {
            return ListDirResult(path: home, entries: [DirEntry(name: "status", dir: true, kind: .dir)])
        }
        guard let project, path.hasPrefix(project.root) || path.hasPrefix("~/status") else {
            return ListDirResult(path: path, entries: [])
        }
        let folder = DemoReviewServices.relative(path, root: project.root)
        return ListDirResult(path: path, entries: project.entries(in: folder, written: script?.written ?? []))
    }

    func terminal(hostID: HostID, sessionID: String, transport: TransportPreference?, userStarted: Bool) -> TerminalHandle {
        attempts.append(.terminal)
        let terminal = DemoTerminal(settings: settings) { [weak self] cols in
            self?.screen(sessionID, cols: cols) ?? ""
        }
        let key = ObjectIdentifier(terminal)
        terminal.onStopped = { [weak self] in self?.terminals[key] = nil }
        terminals[key] = terminal
        return terminal.handle
    }

    func capture(hostID: HostID, sessionID: String, lines: Int) async throws -> String {
        attempts.append(.capture)
        let plain = screen(sessionID, cols: 80)
            .replacingOccurrences(of: "\u{1B}\\[[0-9;]*m", with: "", options: .regularExpression)
            .replacingOccurrences(of: "\r", with: "")
        return plain.split(separator: "\n", omittingEmptySubsequences: false).suffix(lines).joined(separator: "\n")
    }

    private func screen(_ sessionID: String, cols: Int) -> String {
        guard sessionID == DemoScript.sessionID, let script else {
            return DemoScreen.shell(written: script?.written ?? [], project: project)
        }
        return DemoScreen.agent(items: script.items, prompt: script.prompt, phase: script.phase, cols: cols)
    }

    private func refuse(_ attempt: Attempt) throws -> Never {
        attempts.append(attempt)
        throw ReadOnly()
    }
}

/// The machine "Explore with a demo machine" shows: one OpenCode session in a small project,
/// ready to run a recorded task, and a shell beside it.
@MainActor enum DemoMachine {
    static let id = HostID(uuidString: "D3E30000-0000-4000-8000-00000000DE30")!
    static let name = String(localized: "tutorial.demo.machine", defaultValue: "Demo")
    static let host = HostRecordView(id: id, name: name, symbol: .laptop, address: "")
    static let home = "/Users/jamie"
    static let notice = String(
        localized: "tutorial.demo.notice",
        defaultValue: "This is a demo machine. It never connects anywhere, and nothing you do here is saved."
    )
}

/// Tools for the demo machine's settings pages: there is no helper to ask.
@MainActor final class DemoMachineTools: MachineTools {
    func call<T: Sendable>(_ host: HostID, _ body: @Sendable (HelperClient) async throws -> T) async throws -> T {
        throw DemoServices.ReadOnly()
    }
}

/// One visit to the demo: its own store, terminals, chat, review and defaults, made when the
/// person opens the demo and dropped when they leave. Nothing in it is shared with the app's
/// machines.
@MainActor final class DemoVisit: Identifiable {
    let services: DemoServices
    let store: AppStore
    let terminals = TerminalPool()
    let script: DemoScript?
    let review: DemoReviewServices?
    let harness: DemoHarnessServices?
    let tools = DemoMachineTools()
    let library = AgentLibrary.inMemory()
    /// `@AppStorage` and the composer's history for this visit, emptied as it starts and ends.
    let defaults: UserDefaults

    static let defaultsSuite = "app.togo.demo"

    /// `pace` caps the wait between the recording's steps, seconds.
    init(now: Date = .now, bundle: Bundle = .main, pace: Double = 0.5) {
        let defaults = UserDefaults(suiteName: Self.defaultsSuite) ?? UserDefaults()
        defaults.removePersistentDomain(forName: Self.defaultsSuite)
        self.defaults = defaults
        let script = DemoScript.load(bundle: bundle, longestGap: pace)
        let project = DemoProject.load(bundle: bundle)
        self.script = script
        services = DemoServices(script: script, project: project, now: { .now })
        review = project.map { project in
            DemoReviewServices(project: project, started: now) { [weak script] in script?.written ?? [] }
        }
        harness = script.map { DemoHarnessServices(script: $0, defaults: defaults) }
        store = AppStore(services: services, reviewServices: review, harnessServices: harness)
        store.teachesTips = false
        store.apply(services.snapshot())
        store.notice = Notice(text: DemoMachine.notice, isError: false)
    }

    /// The prompt waits in the composer of the scripted session's tab until it is sent.
    func tabsOpened(_ ids: [TabID]) {
        guard let script, script.phase == .ready else { return }
        for id in ids where id.sessionID == DemoScript.sessionID && store.pendingInput[id] == nil {
            store.pendingInput[id] = script.prompt
        }
    }

    /// The person left: chats stop, terminals let go, and the visit's defaults are emptied.
    func end() {
        HarnessTabsRegistry.shared.release(store)
        store.workspace.tabs.forEach { terminals.handle(for: $0.id)?.stop() }
        defaults.removePersistentDomain(forName: Self.defaultsSuite)
    }
}
