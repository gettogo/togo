import SwiftUI
import TogoKit

/// The app frame: the first-run screen until a machine exists, then the control center in a
/// navigation stack, sheets for forms, and the workspace as a full-screen cover over both.
struct RootView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var store: AppStore
    /// Shared by every window, so a tab has one connection however many windows show it.
    private let terminals: TerminalPool
    /// Rows are zoom sources in this namespace, so the workspace grows out of the tapped row
    /// and shrinks back into the row of whichever tab is on screen when it closes.
    @Namespace private var zoom
    /// The last tab on screen, so closing the last tab shrinks into its row instead of nowhere.
    @State private var zoomSource: TabID?
    private let onboarding: any OnboardingServices
    /// The guide: offered on the first-run screen and reopened from settings.
    private let tutorial: TutorialModel?
    @State private var guideShown = false
    @State private var launchShown = LaunchGate.app.claim()
    #if DEBUG
    @State private var debugToolsShown = false
    #endif

    init(store: AppStore, terminals: TerminalPool, onboarding: any OnboardingServices, tutorial: TutorialModel? = nil) {
        _store = State(initialValue: store)
        self.terminals = terminals
        self.onboarding = onboarding
        self.tutorial = tutorial
    }

    var body: some View {
        Group {
            if !store.services.hostsLoaded {
                Color.togoInk.ignoresSafeArea()
            } else if hasNoMachines {
                FirstRunView(tutorial: tutorial) { store.sheet = .addMachine }
            } else {
                controlCenter
            }
        }
        .sheet(item: $store.sheet) { sheet in
            RootSheet(sheet: sheet, store: store, onboarding: onboarding, zoom: zoom, tutorial: tutorial)
        }
        .fullScreenCover(isPresented: $store.workspaceOpen) {
            WorkspaceView(store: store, terminals: terminals)
                .modifier(WorkspaceEntrance(sourceID: zoomSource, namespace: zoom))
        }
        // Under Reduce Motion the cover neither zooms nor slides; the workspace fades in itself.
        .transaction(value: store.workspaceOpen) { if reduceMotion { $0.disablesAnimations = true } }
        // Terminals follow the tabs even while the cover is closed, so a session that ends or
        // is killed from the list lets go of its connection at once.
        .onChange(of: store.workspace, initial: true) { terminals.sync(with: store) }
        .onChange(of: store.workspace.selected, initial: true) { _, id in
            if let id { zoomSource = id }
        }
        // While a sheet is up, the sheet carries the questions itself; one presenter shows one sheet.
        .attachPrompts(store.services.attachPrompts, active: !store.workspaceOpen && store.sheet == nil)
        .focusedSceneValue(\.appStore, store)
        .togoControls()
        .overlay { if launchShown { LaunchOverlay { launchShown = false } } }
    }

    private var hasNoMachines: Bool {
        store.services.hosts.isEmpty && store.controlCenter.machines.isEmpty
    }

    @ViewBuilder private var controlCenter: some View {
        if let tutorial {
            controlCenterStack.tutorialCover(tutorial, isPresented: $guideShown) { store.showSheet(.addMachine) }
        } else {
            controlCenterStack
        }
    }

    private var controlCenterStack: some View {
        NavigationStack(path: $store.path) {
            ControlCenterView(store: store, zoom: zoom)
                .navigationDestination(for: AppRoute.self) { route in
                    destination(route)
                }
                #if DEBUG
                .toolbar {
                    ToolbarItem(placement: .topBarLeading) {
                        Button("Debug tools", systemImage: "ladybug") { debugToolsShown = true }
                    }
                }
                .fullScreenCover(isPresented: $debugToolsShown) { DebugEntry.root() }
                #endif
        }
    }

    @ViewBuilder private func destination(_ route: AppRoute) -> some View {
        switch route {
        case .settings:
            SettingsView(values: settingsValues, actions: settingsActions)
        case .settingsPage(let page):
            SettingsView(values: settingsValues, actions: settingsActions).page(page)
        }
    }

    private var settingsValues: SettingsValues {
        let sections = Dictionary(store.controlCenter.machines.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        return SettingsValues(
            settings: store.services.settings,
            machines: store.services.hosts.map { host in
                SettingsMachine(
                    id: host.id, name: host.name, address: host.address, symbol: host.symbol,
                    helper: sections[host.id]?.helper ?? .checking
                )
            },
            iCloudAvailable: FileManager.default.ubiquityIdentityToken != nil
        )
    }

    /// "Getting started" in settings: the guide from its first page, with every tip again.
    private func openGuide() {
        guard let tutorial else { return }
        tutorial.reopen()
        guideShown = true
    }

    private var settingsActions: SettingsActions {
        let store = self.store
        return SettingsActions(
            update: { store.services.settings = $0 },
            addMachine: { store.sheet = .addMachine },
            editMachine: { store.sheet = .editMachine($0) },
            removeMachine: { id in
                Task {
                    do {
                        try await store.services.removeHost(id)
                    } catch {
                        store.notice = Notice(text: ServiceErrors.message(error), isError: true)
                    }
                }
            },
            openGuide: openGuide
        )
    }
}

/// Grows the workspace out of its row, or under Reduce Motion fades it in where it stands.
private struct WorkspaceEntrance: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var shown = false
    let sourceID: TabID?
    let namespace: Namespace.ID

    func body(content: Content) -> some View {
        if reduceMotion {
            content
                .opacity(shown ? 1 : 0)
                .onAppear { withAnimation(Motion.fade) { shown = true } }
        } else {
            content.navigationTransition(Motion.rowGrow(sourceID: sourceID, in: namespace))
        }
    }
}
