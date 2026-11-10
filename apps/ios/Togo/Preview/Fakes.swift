#if DEBUG
import Foundation
import TogoKit
import TogoTerminal
import UIKit

/// In-memory services for previews and tests. Calls are recorded; `failure` makes every
/// throwing call fail with that error.
@MainActor final class FakeAppServices: AppServices {
    enum Call: Equatable {
        case answer(HostID, String, Decision)
        case kill(HostID, String)
        case rename(HostID, String, String)
        case setMute(HostID, String, Bool)
        case capture(HostID, String, Int)
        case upload(HostID, String, String)
        case create(HostID, CreateSessionParams)
        case listDir(HostID, String)
        case saveHost(HostID)
    }

    var hosts: [HostRecordView]
    var settings = AppSettings()
    var failure: (any Error)?
    /// Per-path wait before `listDir` answers, to reorder responses in tests.
    var listDirDelays: [String: Duration] = [:]
    private(set) var calls: [Call] = [] {
        didSet { if let call = calls.last, calls.count > oldValue.count { onCall(call) } }
    }
    /// Runs after each call is recorded, before it returns, so a scripted machine can answer it.
    var onCall: (Call) -> Void = { _ in }
    /// What every terminal reports as soon as it starts. Empty leaves each tab's link as the
    /// test or preview set it.
    var terminalPhases: [LinkPhase] = [.live(transport: "SSH", note: nil)]
    /// Tabs whose terminal was stopped, in order.
    private(set) var stoppedTerminals: [TabID] = []
    /// Tabs a terminal was made for, in order.
    private(set) var terminalsMade: [TabID] = []
    /// Whether each of those was started by the user, in the same order.
    private(set) var terminalsUserStarted: [Bool] = []
    /// Every subscriber gets every snapshot, as each window's store does from the live services.
    private let hub = SnapshotHub()

    init(hosts: [HostRecordView] = Samples.hosts) {
        self.hosts = hosts
    }

    func publish(_ snapshot: HostViewSnapshot) {
        hub.publish(snapshot)
    }

    func finish() {
        hub.finish()
    }

    func snapshots() -> AsyncStream<HostViewSnapshot> {
        hub.stream()
    }

    func answer(hostID: HostID, requestID: String, decision: Decision) async throws {
        calls.append(.answer(hostID, requestID, decision))
        try fail()
    }

    func kill(hostID: HostID, sessionID: String) async throws {
        calls.append(.kill(hostID, sessionID))
        try fail()
    }

    func rename(hostID: HostID, sessionID: String, name: String) async throws {
        calls.append(.rename(hostID, sessionID, name))
        try fail()
    }

    func setMute(hostID: HostID, sessionID: String, muted: Bool) async throws {
        calls.append(.setMute(hostID, sessionID, muted))
        try fail()
    }

    func create(hostID: HostID, params: CreateSessionParams) async throws -> SessionRow {
        calls.append(.create(hostID, params))
        try fail()
        return Samples.row(hostID: hostID, bib: "09", name: params.name ?? "shell", cwd: params.cwd ?? "~")
    }

    func agents(hostID: HostID) async throws -> [AgentInfo] {
        try fail()
        if DemoAPIFixture.enabled { return DemoAPIFixture.agents }
        return [
            AgentInfo(id: "claude", name: "Claude Code", installed: true),
            AgentInfo(id: "codex", name: "Codex", installed: true),
            AgentInfo(id: "shell", name: "Shell", installed: true),
        ]
    }

    func recentDirs(hostID: HostID) async throws -> [String] {
        try fail()
        if DemoAPIFixture.enabled { return DemoAPIFixture.recentDirs }
        return ["/Users/sam/src/app", "/Users/sam/src/site", "/Users/sam/notes"]
    }

    func listDir(hostID: HostID, path: String) async throws -> ListDirResult {
        calls.append(.listDir(hostID, path))
        if let delay = listDirDelays[path] { try await Task.sleep(for: delay) }
        try fail()
        return ListDirResult(path: path, entries: [])
    }

    func terminal(hostID: HostID, sessionID: String, transport: TransportPreference?, userStarted: Bool) -> TerminalHandle {
        terminalsMade.append(TabID(hostID: hostID, sessionID: sessionID))
        terminalsUserStarted.append(userStarted)
        return TerminalHandle.preview(text: Samples.terminalText, phases: terminalPhases) { [weak self] in
            self?.stoppedTerminals.append(TabID(hostID: hostID, sessionID: sessionID))
        }
    }

    func capture(hostID: HostID, sessionID: String, lines: Int) async throws -> String {
        calls.append(.capture(hostID, sessionID, lines))
        try fail()
        return Samples.terminalText
    }

    func upload(hostID: HostID, sessionID: String, name: String, data: Data) async throws -> String {
        calls.append(.upload(hostID, sessionID, name))
        try fail()
        return "/tmp/togo-uploads/\(name)"
    }

    /// What the machine asks during sign-in, in order.
    var signInPrompts: [AuthPrompt] = []
    /// The answers sign-in got, nil where the user cancelled.
    private(set) var signInAnswers: [AuthAnswer?] = []

    /// Runs as sign-in starts, before the machine asks anything.
    var beforeSignInPrompts: () -> Void = {}

    func signIn(hostID: HostID, prompter: any ConnectionPrompter) async throws {
        beforeSignInPrompts()
        for prompt in signInPrompts {
            let answer = await prompter.answer(prompt)
            signInAnswers.append(answer)
            if answer == nil { throw SSHError.cancelled }
        }
        try fail()
    }

    /// A saved machine joins `hosts` the way a stored one would, replacing any with its id.
    func saveHost(_ record: HostRecord) async throws {
        try fail()
        let view = LiveServices.view(of: record)
        if let index = hosts.firstIndex(where: { $0.id == record.id }) {
            hosts[index] = view
        } else {
            hosts.append(view)
        }
        calls.append(.saveHost(record.id))
    }

    func removeHost(_ id: HostID) async throws {
        try fail()
    }

    private func fail() throws {
        if let failure { throw failure }
    }
}

enum Samples {
    static let studio = HostID(uuidString: "6F1C2D3E-0000-4000-8000-000000000001")!
    static let build = HostID(uuidString: "6F1C2D3E-0000-4000-8000-000000000002")!
    /// Fixed so previews and snapshots render the same relative times every run.
    static let now = Date(timeIntervalSinceReferenceDate: 815_000_000)

    static let hosts = [
        HostRecordView(id: studio, name: "Studio", symbol: .desktop, address: "studio.local"),
        HostRecordView(id: build, name: "Build box", symbol: .server, address: "build.tail1234.ts.net"),
    ]

    static func row(
        hostID: HostID = studio, bib: String?, name: String, agentName: String = "Claude Code",
        status: SessionStatus = .working, activity: String? = nil, cwd: String, since: Date = now
    ) -> SessionRow {
        SessionRow(
            id: "s-\(bib ?? name)", hostID: hostID, bib: bib, name: name, agent: agentName.lowercased(),
            agentName: agentName, status: status, activity: activity, cwd: cwd, muted: false,
            managed: bib != nil, meta: SessionMeta(), statusSince: since
        )
    }

    static func request(id: String, hostID: HostID = studio, bib: String?, title: String) -> RequestCard {
        RequestCard(
            id: id, hostID: hostID, sessionID: bib.map { "s-\($0)" }, bib: bib, agentName: "Claude Code",
            title: title, detail: nil,
            options: [
                ApprovalOption(n: 1, label: "Yes", role: .approve),
                ApprovalOption(n: 2, label: "No", role: .deny),
            ],
            allowText: false, phase: .open
        )
    }

    /// A Claude Code screen mid-task, with CRLF line ends as a pty sends them.
    static let terminalText = [
        "~/src/app",
        "> Run the test suite and fix anything that fails",
        "",
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
        "    \u{1b}[32m+  const item = await store.find(id)\u{1b}[0m",
        "    \u{1b}[32m+  if (!item) return res.status(404).end()\u{1b}[0m",
        "",
    ].joined(separator: "\r\n")

    static var controlCenter: ControlCenterState {
        ControlCenterState(
            requests: [request(id: "r1", bib: "04", title: "Run npm test?")],
            machines: [
                MachineSection(
                    id: studio, name: "Studio", symbol: .desktop, reachability: .online,
                    helper: .present(version: "0.3.0", live: true),
                    sessions: [
                        row(bib: "01", name: "api", status: .working, activity: "Editing routes.ts", cwd: "/Users/sam/src/api"),
                        row(bib: "04", name: "app", status: .needsYou, activity: "Wants to run npm test", cwd: "/Users/sam/src/app"),
                        row(bib: nil, name: "scratch", agentName: "Shell", status: .idle, cwd: "/Users/sam"),
                    ],
                    lastSeen: now
                ),
                MachineSection(
                    id: build, name: "Build box", symbol: .server,
                    reachability: .offline(since: now.addingTimeInterval(-1_800), reason: "Asleep"),
                    helper: .present(version: "0.3.0", live: true),
                    sessions: [row(hostID: build, bib: "02", name: "release", status: .done, cwd: "/home/sam/release")],
                    lastSeen: now.addingTimeInterval(-1_800)
                ),
            ],
            loaded: true
        )
    }
}

extension TerminalHandle {
    /// A real terminal engine showing `text`, for previews and snapshots. It follows the
    /// appearance with the Default theme, as the app does by default.
    static func preview(
        text: String, phases: [LinkPhase] = [.live(transport: "SSH", note: nil)],
        onStop: @escaping @MainActor () -> Void = {}
    ) -> TerminalHandle {
        let engine = SwiftTermEngine(settings: TerminalSettings(), theme: BuiltInThemes.all[0])
        engine.view.accessibilityLabel = WorkspaceCopy.terminal
        engine.inputAccessory = KeyBarView(config: .claude, engine: engine)
        engine.feed(Array(text.utf8)[...])
        let (stream, continuation) = AsyncStream.makeStream(of: LinkPhase.self)
        phases.forEach { continuation.yield($0) }
        return TerminalHandle(
            engineView: PreviewTerminalController(engine: engine), phases: stream,
            search: TerminalSearch(
                find: { engine.find($0, caseSensitive: false, regex: false, backwards: $1) },
                clear: { engine.clearFind() }
            )
        ) {
            continuation.finish()
            onStop()
        }
    }
}

/// Lays the engine out the way `TerminalViewController` does, minus the switch to Metal, whose
/// frames never reach a snapshot. Swaps the Default theme between its dark and light builds
/// with the appearance.
private final class PreviewTerminalController: UIViewController {
    private let engine: SwiftTermEngine

    init(engine: SwiftTermEngine) {
        self.engine = engine
        super.init(nibName: nil, bundle: nil)
    }

    required init?(coder: NSCoder) {
        nil
    }

    override func viewDidLoad() {
        super.viewDidLoad()
        let terminal = engine.view
        terminal.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(terminal)
        NSLayoutConstraint.activate([
            terminal.leadingAnchor.constraint(equalTo: view.safeAreaLayoutGuide.leadingAnchor),
            terminal.trailingAnchor.constraint(equalTo: view.safeAreaLayoutGuide.trailingAnchor),
            terminal.topAnchor.constraint(equalTo: view.safeAreaLayoutGuide.topAnchor),
            terminal.bottomAnchor.constraint(equalTo: view.keyboardLayoutGuide.topAnchor),
        ])
        registerForTraitChanges([UITraitUserInterfaceStyle.self]) { (self: Self, _) in self.applyTheme() }
    }

    /// Runs on every appearance because the controller can move to a host with a different
    /// appearance between renders.
    override func viewIsAppearing(_ animated: Bool) {
        super.viewIsAppearing(animated)
        applyTheme()
    }

    private func applyTheme() {
        let light = traitCollection.userInterfaceStyle == .light
        engine.apply(BuiltInThemes.all[light ? 1 : 0])
        view.backgroundColor = engine.backgroundColor
    }
}
#endif
