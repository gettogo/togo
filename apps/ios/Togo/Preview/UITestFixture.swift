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

    static let current = UITestLaunch(arguments: ProcessInfo.processInfo.arguments)

    init(arguments: [String]) {
        fakes = arguments.contains("-ui-fakes")
        skipIntro = arguments.contains("-ui-skip-launch")
        firstRun = arguments.contains("-ui-first-run")
        wideDiff = arguments.contains("-ui-wide-diff")
        oneMachine = arguments.contains("-ui-one-machine")
        oldHelper = arguments.contains("-ui-old-helper")
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

    init(firstRun: Bool, oneMachine: Bool = false, oldHelper: Bool = false) {
        let hosts = oneMachine ? Array(Samples.hosts.prefix(1)) : Samples.hosts
        services = FakeAppServices(hosts: firstRun ? [] : hosts)
        if !firstRun {
            let state = Samples.controlCenter
            for (index, machine) in state.machines.enumerated() where hosts.contains(where: { $0.id == machine.id }) {
                let old = oldHelper && index == 0
                snapshots[machine.id] = HostViewSnapshot(
                    hostID: machine.id, reachability: machine.reachability, helper: old ? Self.oldHelper : machine.helper,
                    sessions: machine.sessions, requests: state.requests.filter { $0.hostID == machine.id },
                    at: Samples.now, capabilities: old ? [Capability.events] : Self.capabilities
                )
            }
        }
        services.onCall = { [weak self] call in self?.handle(call) }
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
