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

    static let current = UITestLaunch(arguments: ProcessInfo.processInfo.arguments)

    init(arguments: [String]) {
        fakes = arguments.contains("-ui-fakes")
        skipIntro = arguments.contains("-ui-skip-launch")
        firstRun = arguments.contains("-ui-first-run")
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

    init(firstRun: Bool) {
        services = FakeAppServices(hosts: firstRun ? [] : Samples.hosts)
        if !firstRun {
            let state = Samples.controlCenter
            for machine in state.machines {
                snapshots[machine.id] = HostViewSnapshot(
                    hostID: machine.id, reachability: machine.reachability, helper: machine.helper,
                    sessions: machine.sessions, requests: state.requests.filter { $0.hostID == machine.id },
                    at: Samples.now, capabilities: Self.capabilities
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
