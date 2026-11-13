import SwiftUI
import TogoKit
import TogoTerminal

/// Everything the settings screens show. The screens never read a store; they get this and
/// report changes through `SettingsActions`.
struct SettingsValues: Equatable {
    var settings: AppSettings
    var machines: [SettingsMachine]
    /// Removed machines still to be told to stop pushing.
    var pendingRemovals: [PendingRemoval]
    var notifications: NotificationValues
    /// False when no iCloud account is signed in, so sync cannot run.
    var iCloudAvailable: Bool
    /// "Face ID", "Touch ID", "Optic ID" or "passcode", from `BiometryName`.
    var biometry: String
    var appVersion: String
    var fonts: [String]

    init(
        settings: AppSettings = AppSettings(),
        machines: [SettingsMachine] = [],
        pendingRemovals: [PendingRemoval] = [],
        notifications: NotificationValues = NotificationValues(),
        iCloudAvailable: Bool = true,
        biometry: String = BiometryName.current,
        appVersion: String = Self.bundleVersion,
        fonts: [String] = Self.systemFonts
    ) {
        self.settings = settings
        self.machines = machines
        self.pendingRemovals = pendingRemovals
        self.notifications = notifications
        self.iCloudAvailable = iCloudAvailable
        self.biometry = biometry
        self.appVersion = appVersion
        self.fonts = fonts
    }

    static let systemFonts = ["SF Mono", "Menlo", "Courier New"]

    static var bundleVersion: String {
        let info = Bundle.main.infoDictionary
        let version = info?["CFBundleShortVersionString"] as? String ?? "0"
        let build = info?["CFBundleVersion"] as? String ?? "0"
        return "\(version) (\(build))"
    }
}

struct SettingsMachine: Identifiable, Equatable, Sendable {
    var id: HostID
    var name: String
    var address: String
    var symbol: HostSymbol
    var helper: HelperState
    /// Removing it takes this device's key off the machine first.
    var removesKey = false
    /// The agent editor can look a program up here: the machine is online and its helper
    /// launches agents, so the check neither fails nor asks to sign in.
    var checksAgents = false

    static func checksAgents(_ section: MachineSection?) -> Bool {
        guard let section, section.reachability == .online, section.helper != .missing else { return false }
        return section.capabilities.contains(Capability.launch)
    }
}

struct SettingsActions {
    var update: (AppSettings) -> Void
    var addMachine: () -> Void
    var editMachine: (HostID) -> Void
    var removeMachine: (HostID) -> Void
    /// "Getting started": the presenter reopens the guide with `TutorialModel.reopen()`.
    var openGuide: () -> Void
    /// Asks iOS for notification permission.
    var turnOnNotifications: () -> Void
    /// Stops trying to unpair a removed machine.
    var forgetRemoval: (String) -> Void
    /// Nil hides the import button on the machines page.
    var machineImport: MachineImport?

    init(
        update: @escaping (AppSettings) -> Void,
        addMachine: @escaping () -> Void = {},
        editMachine: @escaping (HostID) -> Void = { _ in },
        removeMachine: @escaping (HostID) -> Void = { _ in },
        openGuide: @escaping () -> Void = {},
        turnOnNotifications: @escaping () -> Void = {},
        forgetRemoval: @escaping (String) -> Void = { _ in },
        machineImport: MachineImport? = nil
    ) {
        self.update = update
        self.addMachine = addMachine
        self.editMachine = editMachine
        self.removeMachine = removeMachine
        self.openGuide = openGuide
        self.turnOnNotifications = turnOnNotifications
        self.forgetRemoval = forgetRemoval
        self.machineImport = machineImport
    }
}

/// A settings pane: one row of the list and the page it opens. The raw value is what
/// `@SceneStorage` keeps, so the selected pane survives fold, unfold, a resize and a relaunch.
enum SettingsPane: String, CaseIterable, Hashable, Sendable {
    case machines, agents, notifications, terminal, keyboard, shortcuts, gestures, dictation, sync, privacy, diagnostics, about

    init(_ page: SettingsPage) {
        switch page {
        case .terminal: self = .terminal
        case .keyboard: self = .keyboard
        case .shortcuts: self = .shortcuts
        case .gestures: self = .gestures
        case .dictation: self = .dictation
        case .machines: self = .machines
        case .notifications: self = .notifications
        case .sync: self = .sync
        case .privacy: self = .privacy
        case .diagnostics: self = .diagnostics
        case .about: self = .about
        }
    }

    /// The route that pushes it where Settings sits on another stack; nil where there is none.
    var page: SettingsPage? {
        switch self {
        case .terminal: .terminal
        case .keyboard: .keyboard
        case .shortcuts: .shortcuts
        case .gestures: .gestures
        case .dictation: .dictation
        case .machines: .machines
        case .notifications: .notifications
        case .sync: .sync
        case .privacy: .privacy
        case .diagnostics: .diagnostics
        case .about: .about
        case .agents: nil
        }
    }
}

/// The settings list. Pushed onto another stack (the roster in a compact window), its rows push
/// `AppRoute.settingsPage`, so the pane lives in that stack's path. Given a `selection`, it is the
/// sidebar of `SettingsSplitView`, which owns the navigation and shows the pane beside it.
struct SettingsView: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.agentLibrary) private var agentLibrary
    let values: SettingsValues
    let actions: SettingsActions
    /// The selected pane where Settings owns its navigation; nil where it is pushed.
    var selection: Binding<SettingsPane?>?

    init(values: SettingsValues, actions: SettingsActions, selection: Binding<SettingsPane?>? = nil) {
        self.values = values
        self.actions = actions
        self.selection = selection
    }

    var body: some View {
        Group {
            if let selection {
                // The system sidebar: its own background and selection, no app tint (P17).
                List(selection: selection) { sections }
                    .listStyle(.sidebar)
            } else {
                List { sections }
                    .groupedList()
            }
        }
        .navigationTitle(settingsText("settings.title", "Settings"))
    }

    @ViewBuilder private var sections: some View {
        Section {
            row(.machines)
            if agentLibrary != nil { row(.agents) }
            row(.notifications)
        }
        .settingsRows(sidebar: selection != nil)
        Section {
            row(.terminal)
            row(.keyboard)
            row(.gestures)
            row(.dictation)
        }
        .settingsRows(sidebar: selection != nil)
        Section {
            row(.sync)
            row(.privacy)
        }
        .settingsRows(sidebar: selection != nil)
        Section {
            gettingStarted
            row(.diagnostics)
            row(.about)
        }
        .settingsRows(sidebar: selection != nil)
    }

    /// The page for a route pushed onto another stack.
    func page(_ route: SettingsPage) -> some View {
        pane(SettingsPane(route))
    }

    /// A pane's page, for the split view's detail column and the rows that push it.
    @ViewBuilder func pane(_ page: SettingsPane) -> some View {
        let settings = values.settings
        switch page {
        case .terminal:
            TerminalSettingsView(settings: settings, fonts: values.fonts, update: actions.update)
        case .keyboard:
            KeyboardSettingsView(keys: settings.terminalKeys, update: updateKeys,
                                 optionAsMeta: settings.binding(\.optionAsMeta, actions.update))
        case .shortcuts:
            ShortcutBuilderView { item in
                var keys = settings.terminalKeys
                keys.shortcuts.append(item)
                updateKeys(keys)
            }
        case .gestures:
            GestureSettingsView(gestures: settings.gestures) { gestures in
                var changed = settings
                changed.gestures = gestures
                actions.update(changed)
            }
        case .machines:
            MachinesSettingsView(
                machines: values.machines, add: actions.addMachine,
                edit: actions.editMachine, remove: actions.removeMachine,
                removing: values.pendingRemovals, forgetRemoval: actions.forgetRemoval,
                machineImport: actions.machineImport
            )
        case .notifications:
            NotificationSettingsView(
                settings: settings, notifications: values.notifications, update: actions.update,
                turnOn: actions.turnOnNotifications
            )
        case .sync:
            SyncSettingsView(settings: settings, iCloudAvailable: values.iCloudAvailable, update: actions.update)
        case .privacy:
            PrivacySettingsView(settings: settings, biometry: values.biometry, update: actions.update)
        case .dictation:
            DictationSettingsView(settings: settings, update: actions.update)
        case .diagnostics:
            DiagnosticsView(settings: settings)
        case .about:
            AboutView(appVersion: values.appVersion, settings: settings, machines: values.machines)
        case .agents:
            // Saved agents live in their own store rather than in `values`.
            if let agentLibrary {
                AgentsSettingsView(library: agentLibrary, machines: values.machines)
            }
        }
    }

    private func updateKeys(_ keys: TerminalKeysConfig) {
        var changed = values.settings
        changed.terminalKeys = keys
        actions.update(changed)
    }

    /// Opens the guide rather than pushing a page, so it has no chevron.
    private var gettingStarted: some View {
        Button(action: actions.openGuide) {
            let title = String(localized: "tutorial.settings.row", defaultValue: "Getting started")
            if dynamicTypeSize.isAccessibilitySize {
                Text(title)
            } else {
                Label(title, systemImage: "book")
            }
        }
        .foregroundStyle(Color.togoSnow)
    }

    /// A pane's row. In the sidebar its value is the pane, which the list's selection takes;
    /// pushed, it is the route, so the host stack's path keeps the pane across a fold.
    @ViewBuilder private func row(_ target: SettingsPane) -> some View {
        if selection != nil {
            NavigationLink(value: target) { label(target) }
        } else if let route = target.page {
            NavigationLink(value: AppRoute.settingsPage(route)) { label(target) }
        } else {
            NavigationLink { pane(target) } label: { label(target) }
        }
    }

    private func label(_ target: SettingsPane) -> some View {
        LabeledContent {
            // The system's secondary gray falls under 4.5:1 on the light grouped rows.
            if let detail = detail(target) {
                Text(detail).foregroundStyle(Color.togoMute)
            }
        } label: {
            // At accessibility sizes the symbol would squeeze titles into hyphenated fragments.
            if dynamicTypeSize.isAccessibilitySize {
                Text(title(target))
            } else {
                Label(title(target), systemImage: symbol(target))
            }
        }
    }

    private func title(_ page: SettingsPane) -> String {
        switch page {
        case .terminal: settingsText("settings.terminal.title", "Terminal")
        case .keyboard: settingsText("settings.keyboard.title", "Key bar")
        case .shortcuts: settingsText("settings.builder.title", "New shortcut")
        case .gestures: settingsText("settings.gestures.title", "Gestures")
        case .machines: settingsText("settings.machines.title", "Machines")
        case .agents: settingsText("settings.agents.title", "Agents")
        case .notifications: settingsText("settings.notifications.title", "Notifications")
        case .sync: settingsText("settings.sync.title", "iCloud sync")
        case .privacy: settingsText("settings.privacy.title", "Privacy")
        case .dictation: settingsText("settings.dictation.title", "Dictation")
        case .diagnostics: settingsText("settings.diagnostics.title", "Diagnostics")
        case .about: settingsText("settings.about.title", "About")
        }
    }

    private func symbol(_ page: SettingsPane) -> String {
        switch page {
        case .terminal: "terminal"
        case .keyboard: "keyboard"
        case .shortcuts: "command"
        case .gestures: "hand.tap"
        case .machines: "desktopcomputer"
        case .agents: "sparkles"
        case .notifications: "bell"
        case .sync: "icloud"
        case .privacy: "lock"
        case .dictation: "mic"
        case .diagnostics: "stethoscope"
        case .about: "info.circle"
        }
    }

    private func detail(_ page: SettingsPane) -> String? {
        let settings = values.settings
        switch page {
        case .terminal:
            return ThemeChoice.all(imported: settings.customThemes).first { $0.id == settings.themeID }?.name
        case .machines:
            return values.machines.isEmpty ? settingsText("settings.machines.none", "None") : String(values.machines.count)
        case .agents:
            guard let agentLibrary else { return nil }
            return agentLibrary.presets.isEmpty ? settingsText("settings.machines.none", "None") : String(agentLibrary.presets.count)
        case .notifications:
            return values.notifications.authorization.allowsPush ? settingsText("settings.on", "On") : settingsText("settings.off", "Off")
        case .sync:
            guard values.iCloudAvailable else { return settingsText("settings.sync.unavailable.short", "Signed out") }
            return settings.syncEnabled ? settingsText("settings.on", "On") : settingsText("settings.off", "Off")
        case .privacy:
            return settings.resumeLock ? values.biometry.prefix(1).uppercased() + values.biometry.dropFirst() : settingsText("settings.off", "Off")
        case .about:
            return values.appVersion
        case .keyboard, .shortcuts, .gestures, .dictation, .diagnostics:
            return nil
        }
    }
}

private extension View {
    /// Pushed, a section's rows sit on the grouped cards; in the sidebar they keep the system's.
    @ViewBuilder func settingsRows(sidebar: Bool) -> some View {
        if sidebar { self } else { groupedRows() }
    }
}

/// Settings copy. The key is looked up in Localizable.xcstrings under `settings.`; `english` is
/// what shows when the catalog has no entry for it.
func settingsText(_ key: StaticString, _ english: String.LocalizationValue) -> String {
    String(localized: key, defaultValue: english)
}

extension AppSettings {
    /// A binding that writes a changed copy through `update`, so pages edit plain values.
    @MainActor func binding<Value>(_ keyPath: WritableKeyPath<AppSettings, Value>, _ update: @escaping (AppSettings) -> Void) -> Binding<Value> {
        Binding {
            self[keyPath: keyPath]
        } set: { value in
            var changed = self
            changed[keyPath: keyPath] = value
            update(changed)
        }
    }
}

extension HelperState {
    /// The helper's state as a sentence fragment for machine rows.
    var settingsDescription: String {
        switch self {
        case .checking: settingsText("settings.helper.checking", "Checking for the helper")
        case .present(let version, true): settingsText("settings.helper.live", "Helper \(version)")
        case .present(let version, false): settingsText("settings.helper.polling", "Helper \(version)")
        case .missing: settingsText("settings.helper.missing", "Helper not installed")
        case .outdated(let found) where found.isEmpty: settingsText("settings.helper.outdatedUnknown", "Helper needs an update")
        case .outdated(let found): settingsText("settings.helper.outdated", "Helper \(found) needs an update")
        case .newer: settingsText("settings.helper.newer", "Helper is newer than this app; update Togo")
        }
    }
}

#if DEBUG
/// Settings with in-memory values, for previews.
struct SettingsPreviewHost: View {
    @State var values: SettingsValues

    var body: some View {
        NavigationStack {
            SettingsView(values: values, actions: SettingsActions { values.settings = $0 })
                .navigationDestination(for: AppRoute.self) { route in
                    if case .settingsPage(let page) = route {
                        SettingsView(values: values, actions: SettingsActions { values.settings = $0 }).page(page)
                    }
                }
        }
        .togoControls()
    }
}

extension SettingsValues {
    static var sample: SettingsValues {
        SettingsValues(
            machines: [
                SettingsMachine(
                    id: Samples.studio, name: "Studio", address: "studio.local", symbol: .desktop,
                    helper: .present(version: "0.3.0", live: true), checksAgents: true
                ),
                SettingsMachine(
                    id: Samples.build, name: "Build box", address: "build.tail1234.ts.net", symbol: .server,
                    helper: .outdated(found: "0.2.1")
                ),
            ],
            notifications: .sample, biometry: "Face ID", appVersion: "0.1.0 (1)"
        )
    }
}

#Preview {
    SettingsPreviewHost(values: .sample)
}
#endif
