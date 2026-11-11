import SwiftUI
import TogoKit

/// The ways out of the demo, for the app's own screens to offer while it shows: Done in the
/// sessions list, and the real thing where the demo has nothing to show.
struct DemoExit: Equatable {
    let leave: @MainActor () -> Void
    /// Leaves the demo and starts adding a machine; nil where the demo was opened from a place
    /// that cannot add one.
    var addMachine: (@MainActor () -> Void)?
    /// Leaves the demo for the guide.
    var openGuide: (@MainActor () -> Void)?

    static func == (lhs: Self, rhs: Self) -> Bool { true }
}

extension EnvironmentValues {
    /// Set while the app's screens show the demo machine.
    @Entry var demoExit: DemoExit? = nil
}

/// The demo machine in the app's own window frame: the same sessions list, workspace, chat,
/// terminal, review and settings a paired machine gets, over `DemoVisit`'s services. Covers
/// the first-run screen or the guide; Done in the sessions list closes it.
struct DemoControlCenter: View {
    @Environment(\.windowID) private var window
    let visit: DemoVisit
    let exit: DemoExit

    init(visit: DemoVisit, exit: DemoExit) {
        self.visit = visit
        self.exit = exit
    }

    var body: some View {
        RootView(store: visit.store, terminals: visit.terminals, onboarding: DemoOnboardingServices(), tutorial: nil)
            .environment(\.demoExit, ending)
            .environment(\.machineTools, visit.tools)
            .environment(\.agentLibrary, visit.library)
            .defaultAppStorage(visit.defaults)
            .task { await visit.store.run() }
            // As the window does for its own store: the tab on screen gets its terminal.
            .onChange(of: visit.store.workspace.selected) { _, id in show(id) }
            .onChange(of: visit.store.workspace.secondary) { _, id in show(id) }
            .onChange(of: visit.store.workspaceOpen) { _, open in
                if open { show(visit.store.workspace.selected) }
            }
            .onChange(of: visit.store.workspace.tabs.map(\.id), initial: true) { _, ids in visit.tabsOpened(ids) }
    }

    /// Every way out ends the visit first. Not on disappearing: a review opened as a cover on a
    /// phone takes this view off screen while the visit goes on.
    private var ending: DemoExit {
        let visit = self.visit, exit = self.exit
        var ending = DemoExit(leave: {
            visit.end()
            exit.leave()
        })
        if let add = exit.addMachine {
            ending.addMachine = {
                visit.end()
                add()
            }
        }
        if let open = exit.openGuide {
            ending.openGuide = {
                visit.end()
                open()
            }
        }
        return ending
    }

    private func show(_ id: TabID?) {
        if let id { visit.terminals.show(id, in: window) }
    }
}

/// Done at the head of the sessions list, while it shows the demo: the close of the cover the
/// demo is, first in the bar as a close is.
struct DemoDoneToolbarItem: ToolbarContent {
    let exit: DemoExit

    var body: some ToolbarContent {
        ToolbarItem(placement: .cancellationAction) {
            Button(DemoCopy.done, systemImage: "xmark", role: .close) { exit.leave() }
                .accessibilityIdentifier("demo.done")
        }
    }
}

/// In place of a form that would change a machine: the demo machine has nothing to change, so
/// the sheet says so and offers the person's own machine instead.
struct DemoNotHere: View {
    let exit: DemoExit
    let close: () -> Void

    var body: some View {
        NavigationStack {
            DemoNotHereContent(exit: exit)
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button(DemoCopy.close, systemImage: "xmark", role: .close, action: close)
                    }
                }
        }
    }
}

/// "Not in the demo", calmly, with a way to add a real machine: a sheet's content, or a page
/// pushed in Settings in place of one that would ask the demo machine's helper.
struct DemoNotHereContent: View {
    let exit: DemoExit

    var body: some View {
        StateView(
            kind: .empty, title: DemoCopy.notHereTitle, message: DemoCopy.notHereMessage,
            action: exit.addMachine.map { add in StateView.Action(DemoCopy.addMachine) { add() } }
        )
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Color.togoInk)
    }
}

enum DemoCopy {
    static let done = String(localized: "tutorial.demo.done", defaultValue: "Done")
    static let close = String(localized: "tutorial.demo.close", defaultValue: "Close")
    static let notHereTitle = String(localized: "tutorial.demo.unavailable.title", defaultValue: "Not in the demo")
    static let notHereMessage = String(
        localized: "tutorial.demo.unavailable.machine",
        defaultValue: "The demo machine can't be changed. Add your own machine to connect to it."
    )
    static let addMachine = String(localized: "onboard.firstRun.add", defaultValue: "Add a machine")
    static let notifications = String(
        localized: "tutorial.demo.notifications",
        defaultValue: "Notifications come from your own machines once one is paired. The demo machine sends none."
    )
}

/// Adding a machine never starts inside the demo (`DemoNotHere` stands in for its sheets), so
/// these only refuse: no browsing the network, no keys, no connections.
@MainActor final class DemoOnboardingServices: OnboardingServices {
    func discover() -> AsyncStream<DiscoveryEvent> { AsyncStream { $0.finish() } }
    func deviceKey() async throws -> KeyInfo { throw DemoServices.ReadOnly() }
    func importKey(openSSH: String, passphrase: String?, label: String) async throws -> ImportedKey {
        throw DemoServices.ReadOnly()
    }
    func connect(
        _ profile: HostProfile, password: String?, answers: [String]?,
        trustNewKey: @escaping @Sendable (HostKeyPrompt) async -> Bool
    ) async -> ConnectOutcome {
        .failed(DemoServices.ReadOnly().errorDescription ?? "")
    }
    func forgetHostKey(_ hostID: String) async throws { throw DemoServices.ReadOnly() }
    func setSavedPassword(_ password: String?, for machine: UUID) throws { throw DemoServices.ReadOnly() }
    func keyLabel(id: String) -> String? { nil }
}
