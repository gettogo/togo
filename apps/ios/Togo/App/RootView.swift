import SwiftUI
import TogoKit

/// The app frame: the first-run screen until a machine exists, then `AdaptiveRoot` (the sessions
/// list and the workspace, stacked or side by side), with sheets for forms over either.
struct RootView: View {
    @Environment(\.windowID) private var window
    @State private var store: AppStore
    /// Shared by every window, so a tab has one connection however many windows show it.
    private let terminals: TerminalPool
    /// Rows are zoom sources in this namespace, so the workspace grows out of the tapped row
    /// and shrinks back into the row of whichever tab is on screen when it closes.
    @Namespace private var zoom
    private let onboarding: any OnboardingServices
    /// The guide: offered on the first-run screen, opened once on a first launch that already
    /// has machines, and reopened from settings.
    private let tutorial: TutorialModel?
    @State private var guideShown = false
    /// The user tapped Add a machine on the welcome, passing the guide by.
    @State private var startedFromWelcome = false
    @State private var launchShown = LaunchGate.app.claim()
    /// A form opened from the Settings sheet takes the window's one sheet; Settings comes back after.
    @State private var settingsHandover = SettingsHandover()
    /// The window's width, for the presenters attached here (`AdaptiveRoot` measures its own).
    @State private var windowWidth: CGFloat?

    init(store: AppStore, terminals: TerminalPool, onboarding: any OnboardingServices, tutorial: TutorialModel? = nil) {
        _store = State(initialValue: store)
        self.terminals = terminals
        self.onboarding = onboarding
        self.tutorial = tutorial
    }

    var body: some View {
        // Loading, the first-run screen and the app swap as whole layers on `fade`: the first
        // swap plays under the launch overlay, the next when the first machine is added.
        ZStack {
            if !store.services.hostsLoaded {
                // motion: state change — the app's whole layer changed (loading → first run or app)
                Color.togoInk.ignoresSafeArea()
                    .transition(.opacity)
            } else if hasNoMachines {
                FirstRunView(tutorial: tutorial, launchPlaying: launchShown) {
                    startedFromWelcome = true
                    store.sheet = .addMachine
                }
                    .task { PerfSignposts.interactive() }
                    // motion: state change — the app's whole layer changed
                    .transition(.opacity)
            } else {
                // The control center, or the sidebar beside the workspace, is the app's first
                // usable screen; what launch put off starts once it is up.
                controlCenter
                    .task { PerfSignposts.interactive() }
                    // motion: state change — the app's whole layer changed
                    .transition(.opacity)
            }
        }
        // motion: state change — loading, first run and the app swap as whole layers
        .motion(.fade, value: RootStage(loaded: store.services.hostsLoaded, empty: hasNoMachines))
        .presentSheet("root.form", item: formSheet) { sheet in
            RootSheet(
                sheet: sheet, store: store, onboarding: onboarding, zoom: zoom, tutorial: tutorial,
                settings: { AnyView(settingsSheet) }
            )
        }
        .onChange(of: store.sheet) { _, sheet in
            guard sheet == nil, let back = settingsHandover.closed() else { return }
            store.sheet = back
        }
        // The window's one host for the review, above the split view: the workspace is a
        // column of it now, never a cover, so nothing needs a second host. A second host in the
        // workspace column handed an open review between the two whenever the workspace came on
        // or off screen, and sat in a column a fold collapses or expands.
        .reviewPresentation(store: store, terminals: terminals, active: true)
        // Terminals follow the tabs even while the workspace is closed, so a session that ends
        // or is killed from the list lets go of its connection at once.
        .onChange(of: store.workspace, initial: true) { terminals.sync(window: window, with: store) }
        // While a form sheet is up, the sheet carries the questions itself; one presenter shows
        // one sheet. Otherwise this is their one host, for the roster and the workspace alike
        // (behind a review, a question waits for it to close).
        .attachPrompts(store.services.attachPrompts, host: "root", active: formSheet.wrappedValue == nil)
        // Presenters at the root decide by the window's room too (`AdaptivePresentation`).
        .environment(\.windowWidth, windowWidth)
        .background {
            Color.clear
                .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { windowWidth = $0 }
                .ignoresSafeArea()
        }
        .focusedSceneValue(\.appStore, store)
        .togoControls()
        .readsFold()
        // Every cold launch; it hands over once the first screen is ready.
        .launchOverlay(shown: $launchShown, ready: LaunchReadiness.ready(
            hostsLoaded: store.services.hostsLoaded,
            onScreen: store.workspaceOnScreen ? store.focusedTab.map { store.layer(for: $0.id) } : nil
        ))
    }

    private struct RootStage: Equatable {
        var loaded: Bool
        var empty: Bool
    }

    /// Every sheet but the review, which has its own presentation.
    private var formSheet: Binding<AppSheet?> {
        Binding {
            if case .review? = store.sheet { return nil }
            return store.sheet
        } set: { store.sheet = $0 }
    }

    private var hasNoMachines: Bool {
        store.services.hosts.isEmpty && store.controlCenter.machines.isEmpty
    }

    @ViewBuilder private var controlCenter: some View {
        if let tutorial {
            adaptive
                .tutorialCover(tutorial, isPresented: $guideShown) { store.showSheet(.addMachine) }
                // A first launch with machines from iCloud skips the welcome, so the guide
                // comes to the user once; closing it marks it seen.
                .onChange(of: tutorial.opensOnLaunch(
                    hasMachines: !hasNoMachines, startedFromWelcome: startedFromWelcome,
                    launchPlaying: launchShown, sheetShown: store.sheet != nil
                ), initial: true) { _, opens in
                    if opens { guideShown = true }
                }
        } else {
            adaptive
        }
    }

    private var adaptive: some View {
        AdaptiveRoot(store: store, terminals: terminals, zoom: zoom, destination: destination)
    }

    @ViewBuilder private func destination(_ route: AppRoute) -> some View {
        switch route {
        case .archive:
            ArchiveScreen(store: store)
        case .settings:
            SettingsView(values: settingsValues, actions: settingsActions)
        case .settingsPage(let page):
            SettingsView(values: settingsValues, actions: settingsActions).page(page)
        }
    }

    /// Settings beside the sidebar: a page sheet that owns its navigation, so it never covers
    /// the session in the detail. Its panes sit beside the list where the sheet is wide.
    private var settingsSheet: some View {
        SettingsSplitView(values: settingsValues, actions: settingsActions) { store.sheet = nil }
            .togoControls()
    }

    private var settingsValues: SettingsValues {
        let sections = Dictionary(store.controlCenter.machines.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        return SettingsValues(
            settings: store.services.settings,
            machines: store.services.hosts.map { host in
                SettingsMachine(
                    id: host.id, name: host.name, address: host.address, symbol: host.symbol,
                    helper: sections[host.id]?.helper ?? .checking,
                    removesKey: store.services.removesKey(host.id),
                    checksAgents: SettingsMachine.checksAgents(sections[host.id])
                )
            },
            pendingRemovals: store.services.pendingRemovals,
            notifications: NotificationValues(
                authorization: store.services.pushAuthorization,
                machines: NotificationValues.machines(
                    store.controlCenter.machines, paired: store.services.pairedMachines,
                    commandsOn: store.services.confirmedCommands, pending: store.services.pendingCommands
                )
            ),
            iCloudAvailable: FileManager.default.ubiquityIdentityToken != nil
        )
    }

    /// "Getting started" in settings: the guide from its first page, with every tip again.
    private func openGuide() {
        guard let tutorial else { return }
        // The guide is a new root: the Settings sheet closes first, or the guide would wait behind it.
        if store.sheet == .settings { store.sheet = nil }
        tutorial.reopen()
        guideShown = true
    }

    private var settingsActions: SettingsActions {
        let store = self.store
        return SettingsActions(
            update: { store.services.settings = $0 },
            addMachine: { store.sheet = settingsHandover.open(.addMachine, from: store.sheet) },
            editMachine: { store.sheet = settingsHandover.open(.editMachine($0), from: store.sheet) },
            removeMachine: { id in
                Task {
                    do {
                        try await store.services.removeHost(id)
                    } catch {
                        store.notice = Notice(text: ServiceErrors.message(error), isError: true)
                    }
                }
            },
            openGuide: openGuide,
            turnOnNotifications: { Task { await store.services.turnOnNotifications() } },
            forgetRemoval: { store.services.forgetRemoval($0) },
            machineImport: MachineImport(
                make: {
                    let services = store.services
                    return ImportHostsModel(
                        existing: { services.hosts.compactMap { services.hostRecord($0.id) } },
                        deviceKeyID: { try await onboarding.deviceKey().id },
                        save: { try await services.saveHost($0) }
                    )
                },
                openInForm: { store.sheet = settingsHandover.open(.addMachineFilled($0), from: store.sheet) },
                finished: { count in
                    store.notice = Notice(
                        text: count == 1
                            ? String(localized: "import.hosts.doneOne", defaultValue: "Added 1 machine. It signs in with this iPhone's key.")
                            : String(localized: "import.hosts.done", defaultValue: "Added \(count) machines. They sign in with this iPhone's key."),
                        isError: false
                    )
                }
            )
        )
    }
}
