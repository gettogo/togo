#if DEBUG
import Foundation
import TogoKit

/// What UI tests launch the app with. They never reach a network: `-ui-fakes` swaps every
/// service for a scripted machine set, and the other switches shape the first screen.
struct UITestLaunch {
    /// Fake services instead of SSH, Bonjour and iCloud.
    var fakes: Bool
    /// No launch animation and no guide, so the first screen is ready to use at once.
    var skipIntro: Bool
    /// Starts with no machines, on the first-run screen.
    var firstRun: Bool
    /// Only the first sample machine, so flows that skip choosing a machine start where it is chosen.
    var oneMachine: Bool
    /// The first machine runs a helper too old for review, for the update prompts.
    var oldHelper: Bool
    /// The large generated file's diff has lines 2,000 columns wide, for the scrolling budget.
    var wideDiff: Bool
    /// The presentation coordinator's state as an element (`presentation.probe`).
    var probes: Bool
    /// A recorded helper stream the harness layers replay (`-ui-replay <name>`, a JSONL file in
    /// the app's `Replays` resources), in place of a machine's helper.
    var replay: String?
    /// The second machine needs a password before it connects (its sign-in sheet asks for it).
    var authNeeded: Bool
    /// The busy demo roster (`Samples.demoHosts`, `Samples.demoControlCenter`): two machines and
    /// nine sessions across eight agents, for the website's reference captures.
    var demoRoster: Bool
    /// Acts as a plain iPhone with no hardware keyboard, so no shortcut hint shows. The
    /// Simulator otherwise reports the Mac's keyboard as attached.
    var noKeyboard: Bool
    /// Session 01 reviews the demo API and the new session sheet lists the registry's agents
    /// (`DemoAPIFixture`), for the website's screen captures.
    var demoAPI: Bool

    static let current = UITestLaunch(arguments: ProcessInfo.processInfo.arguments)

    init(arguments: [String]) {
        fakes = arguments.contains("-ui-fakes")
        skipIntro = arguments.contains("-ui-skip-launch")
        firstRun = arguments.contains("-ui-first-run")
        wideDiff = arguments.contains("-ui-wide-diff")
        oneMachine = arguments.contains("-ui-one-machine")
        oldHelper = arguments.contains("-ui-old-helper")
        if let index = arguments.firstIndex(of: "-ui-replay"), arguments.indices.contains(index + 1) {
            replay = arguments[index + 1]
        } else {
            replay = nil
        }
        probes = arguments.contains("-ui-probes") || replay != nil
        authNeeded = arguments.contains("-ui-auth-needed")
        demoRoster = arguments.contains("-ui-demo-roster")
        noKeyboard = arguments.contains("-ui-no-keyboard")
        demoAPI = arguments.contains("-ui-demo-api")
    }

    /// A defaults suite emptied on every launch, so one test's guide progress and settings
    /// never leak into the next.
    func defaults() -> UserDefaults {
        let name = "app.togo.ui-tests"
        let defaults = UserDefaults(suiteName: name) ?? .standard
        defaults.removePersistentDomain(forName: name)
        if skipIntro { defaults.set(true, forKey: TutorialModel.Keys.seen) }
        return defaults
    }
}

/// The machines UI tests drive: the sample control center, kept as state and published
/// through `FakeAppServices` again whenever a call changes it, as a helper's events would.
@MainActor final class UITestMachines {
    let services: FakeAppServices
    private var snapshots: [HostID: HostViewSnapshot] = [:]

    /// A helper new enough for the review screens: git reads and writes, and the port list.
    static let capabilities: Set<String> = [Capability.events, Capability.git, Capability.gitWrite, Capability.ports]

    /// The helper on the first machine before git and ports, which still lists sessions.
    static let oldHelper = HelperState.outdated(found: "0.2.1")

    /// A recorded helper the harness layers replay; its approvals join the first machine's requests.
    private let replay: ReplayHarnessServices?

    init(firstRun: Bool, oneMachine: Bool = false, oldHelper: Bool = false, authNeeded: Bool = false,
         demoRoster: Bool = false, replay: ReplayHarnessServices? = nil) {
        // The demo roster brings its own two machines; `oneMachine` narrows the usual samples.
        let hosts = demoRoster ? Samples.demoHosts : oneMachine ? Array(Samples.hosts.prefix(1)) : Samples.hosts
        services = FakeAppServices(hosts: firstRun ? [] : hosts)
        self.replay = replay
        if !firstRun {
            let state = demoRoster ? Samples.demoControlCenter : Samples.controlCenter
            for (index, machine) in state.machines.enumerated() where hosts.contains(where: { $0.id == machine.id }) {
                let old = oldHelper && index == 0
                var capabilities: Set<String> = old ? [Capability.events] : Self.capabilities
                // A replayed helper serves harness.v1, so the first machine's agents get a chat.
                if replay != nil, index == 0 { capabilities.insert(Capability.harness) }
                // A current helper takes an agent's flags.
                if DemoAPIFixture.enabled { capabilities.insert(Capability.launch) }
                snapshots[machine.id] = HostViewSnapshot(
                    hostID: machine.id,
                    reachability: authNeeded && index == 1 ? .authNeeded : machine.reachability,
                    helper: old ? Self.oldHelper : machine.helper,
                    sessions: machine.sessions,
                    // A replay brings its own requests.
                    requests: replay == nil ? state.requests.filter { $0.hostID == machine.id } : [],
                    at: Samples.now, capabilities: capabilities
                )
            }
            if authNeeded { services.signInPrompts = [.password(user: "sam", host: "build.local")] }
        }
        services.onCall = { [weak self] call in self?.handle(call) }
        replay?.onApproval = { [weak self] change, session in self?.approval(change, session: session) }
    }

    /// An approval event from the replay, as the first machine's monitor would map it.
    private func approval(_ update: ReplayHarnessServices.ApprovalChange, session: String) {
        guard let hostID = services.hosts.first?.id else { return }
        change(hostID) { snapshot in
            switch update {
            case .open(let approval):
                let bib = snapshot.sessions.first { $0.id == session }?.bib
                snapshot.requests.removeAll { $0.id == approval.requestId }
                snapshot.requests.append(RequestCard(
                    id: approval.requestId, hostID: hostID, sessionID: session, bib: bib,
                    agentName: self.replay?.script.agent?.name ?? AgentCatalog.name(approval.agent) ?? approval.agent,
                    title: approval.title, detail: approval.detail,
                    options: approval.options, allowText: approval.allowText, phase: .open, signIn: approval.signIn
                ))
            case .closed(let requestID):
                snapshot.requests.removeAll { $0.id == requestID }
            }
        }
    }

    /// The first machine's first session: the one a replay plays in.
    var firstSessionID: String? { firstSession?.id }

    /// The first machine's first session: the one a replay plays in.
    var firstSession: SessionRow? {
        services.hosts.first.flatMap { snapshots[$0.id]?.sessions.first }
    }

    /// A request answered outside the app (the Live Activity), as the helper would report it.
    func answeredElsewhere(_ requestID: String) {
        guard let hostID = services.hosts.first?.id else { return }
        change(hostID) { $0.requests.removeAll { $0.id == requestID } }
        replay?.trigger("approval.answer")
    }

    /// Publishes every machine once, the way monitors report on start.
    func start() {
        for id in services.hosts.map(\.id) {
            if let snapshot = snapshots[id] { services.publish(snapshot) }
        }
    }

    private func handle(_ call: FakeAppServices.Call) {
        switch call {
        case .answer(let hostID, let requestID, _):
            change(hostID) { $0.requests.removeAll { $0.id == requestID } }
            replay?.trigger("approval.answer")
        case .kill(let hostID, let sessionID):
            change(hostID) { $0.sessions.removeAll { $0.id == sessionID } }
        case .create(let hostID, let params):
            // The same row `FakeAppServices.create` returns, so the tab it opens finds it.
            change(hostID) { snapshot in
                snapshot.sessions.append(Samples.row(
                    hostID: hostID, bib: "09", name: params.name ?? "shell", cwd: params.cwd ?? "~"
                ))
            }
        case .saveHost(let hostID):
            snapshots[hostID] = snapshots[hostID] ?? HostViewSnapshot(
                hostID: hostID, reachability: .online, helper: .present(version: "0.3.0", live: true),
                sessions: [], requests: [], at: Samples.now, capabilities: [Capability.events]
            )
            change(hostID) { _ in }
        case .rename, .setMute, .capture, .upload, .listDir:
            break
        }
    }

    private func change(_ hostID: HostID, _ edit: (inout HostViewSnapshot) -> Void) {
        guard var snapshot = snapshots[hostID] else { return }
        edit(&snapshot)
        snapshots[hostID] = snapshot
        services.publish(snapshot)
    }
}
#endif
