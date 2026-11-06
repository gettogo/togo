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

    init(
        update: @escaping (AppSettings) -> Void,
        addMachine: @escaping () -> Void = {},
        editMachine: @escaping (HostID) -> Void = { _ in },
        removeMachine: @escaping (HostID) -> Void = { _ in },
        openGuide: @escaping () -> Void = {},
        turnOnNotifications: @escaping () -> Void = {},
        forgetRemoval: @escaping (String) -> Void = { _ in }
    ) {
        self.update = update
        self.addMachine = addMachine
        self.editMachine = editMachine
        self.removeMachine = removeMachine
        self.openGuide = openGuide
        self.turnOnNotifications = turnOnNotifications
        self.forgetRemoval = forgetRemoval
    }
}

/// The settings root. Pages push inside whatever navigation stack presents it; `page(_:)` is
/// also the destination for `AppRoute.settingsPage`.
struct SettingsView: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let values: SettingsValues
    let actions: SettingsActions

    var body: some View {
        List {
            Section {
                row(.terminal)
                row(.keyboard)
                row(.gestures)
                row(.dictation)
            }
            .groupedRows()
            Section {
                row(.machines)
                row(.notifications)
                row(.sync)
                row(.privacy)
            }
            .groupedRows()
            Section {
                gettingStarted
                row(.diagnostics)
                row(.about)
            }
            .groupedRows()
        }
        .groupedList()
        .navigationTitle(settingsText("settings.title", "Settings"))
    }

    @ViewBuilder func page(_ page: SettingsPage) -> some View {
        let settings = values.settings
        switch page {
        case .terminal:
            TerminalSettingsView(settings: settings, fonts: values.fonts, update: actions.update)
        case .keyboard:
            KeyboardSettingsView(keyBar: settings.keyBarConfig, update: updateKeyBar)
        case .shortcuts:
            ShortcutBuilderView { item in
                var bar = settings.keyBarConfig
                bar.items.append(item)
                updateKeyBar(bar)
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
                removing: values.pendingRemovals, forgetRemoval: actions.forgetRemoval
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
            AboutView(appVersion: values.appVersion, machines: values.machines)
        }
    }

    private func updateKeyBar(_ bar: KeyBarConfig) {
        var changed = values.settings
        changed.keyBarConfig = bar
        actions.update(changed)
    }

    /// Opens the guide full screen rather than pushing a page, so it has no chevron.
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

    private func row(_ target: SettingsPage) -> some View {
        NavigationLink {
            page(target)
        } label: {
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
    }

    private func title(_ page: SettingsPage) -> String {
        switch page {
        case .terminal: settingsText("settings.terminal.title", "Terminal")
        case .keyboard: settingsText("settings.keyboard.title", "Keyboard bar")
        case .shortcuts: settingsText("settings.builder.title", "New shortcut")
        case .gestures: settingsText("settings.gestures.title", "Gestures")
        case .machines: settingsText("settings.machines.title", "Machines")
        case .notifications: settingsText("settings.notifications.title", "Notifications")
        case .sync: settingsText("settings.sync.title", "iCloud sync")
        case .privacy: settingsText("settings.privacy.title", "Privacy")
        case .dictation: settingsText("settings.dictation.title", "Dictation")
        case .diagnostics: settingsText("settings.diagnostics.title", "Diagnostics")
        case .about: settingsText("settings.about.title", "About")
        }
    }

    private func symbol(_ page: SettingsPage) -> String {
        switch page {
        case .terminal: "terminal"
        case .keyboard: "keyboard"
        case .shortcuts: "command"
        case .gestures: "hand.tap"
        case .machines: "desktopcomputer"
        case .notifications: "bell"
        case .sync: "icloud"
        case .privacy: "lock"
        case .dictation: "mic"
        case .diagnostics: "stethoscope"
        case .about: "info.circle"
        }
    }

    private func detail(_ page: SettingsPage) -> String? {
        let settings = values.settings
        switch page {
        case .terminal:
            return ThemeChoice.all(imported: settings.customThemes).first { $0.id == settings.themeID }?.name
        case .machines:
            return values.machines.isEmpty ? settingsText("settings.machines.none", "None") : String(values.machines.count)
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
        case .present(let version, false): settingsText("settings.helper.polling", "Helper \(version), refreshes every few seconds")
        case .missing: settingsText("settings.helper.missing", "Helper not installed")
        case .outdated(let found): settingsText("settings.helper.outdated", "Helper \(found) needs an update")
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
                    helper: .present(version: "0.3.0", live: true)
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
