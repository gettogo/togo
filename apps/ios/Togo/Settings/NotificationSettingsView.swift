import SwiftUI
import TogoKit
import UIKit

/// What the notifications page shows besides the settings themselves.
struct NotificationValues: Equatable {
    var authorization: PushRegistrar.Authorization = .notDetermined
    var machines: [NotificationMachine] = []

    /// Each machine's push state from what the control center knows about it. `commandsOn`
    /// holds the paired machines whose helper last confirmed it sends command text, `pending`
    /// those sent a choice through the relay that they have not confirmed yet.
    static func machines(
        _ sections: [MachineSection], paired: Set<HostID>, commandsOn: Set<HostID> = [], pending: Set<HostID> = []
    ) -> [NotificationMachine] {
        sections.map { section in
            let state: NotificationMachine.State =
                if paired.contains(section.id) { .paired }
                else if section.helper == .missing { .needsHelper }
                else if section.reachability != .online { .offline }
                else if !section.capabilities.contains(Capability.push) { .helperTooOld }
                else { .notPaired }
            return NotificationMachine(
                id: section.id, name: section.name, state: state,
                helperCommand: state == .paired ? commandsOn.contains(section.id) : nil,
                commandPending: state == .paired && pending.contains(section.id),
                supportsNotify: section.capabilities.contains(Capability.notify),
                supportsAwake: section.capabilities.contains(Capability.awake),
                awake: section.awake,
                hasSettings: section.reachability == .online && section.helper != .missing
            )
        }
    }
}

struct NotificationMachine: Identifiable, Equatable, Sendable {
    enum State: Equatable, Sendable {
        case paired
        /// Online with a helper that sends pushes, not paired yet.
        case notPaired
        case helperTooOld
        case needsHelper
        case offline
    }

    var id: HostID
    var name: String
    var state: State
    /// Whether the machine's helper last confirmed it sends command text; nil when not paired.
    var helperCommand: Bool?
    /// A choice went through the relay and the machine has not confirmed it yet.
    var commandPending = false
    /// Whether the machine's helper advertised per-machine notification settings.
    var supportsNotify = false
    /// Whether the machine's helper advertised keep-awake, and the state its monitor last read.
    var supportsAwake = false
    var awake: AwakeStatus?
    /// Whether the machine can be asked for its settings now.
    var hasSettings = false

    /// Command text is a choice only where the helper can send Live Activity pushes.
    var canShowCommands: Bool { state == .paired || state == .notPaired }
}

struct NotificationSettingsView: View {
    let settings: AppSettings
    let notifications: NotificationValues
    let update: (AppSettings) -> Void
    /// Asks iOS for permission. Only offered before the user has been asked.
    let turnOn: () -> Void

    @Environment(\.openURL) private var openURL
    @State private var ownRelay: Bool
    @State private var relayText: String
    @State private var commandsFor: NotificationMachine?

    /// `relayDraft` starts the address field with text the user has not saved yet.
    init(
        settings: AppSettings, notifications: NotificationValues, update: @escaping (AppSettings) -> Void,
        turnOn: @escaping () -> Void, relayDraft: String? = nil
    ) {
        self.settings = settings
        self.notifications = notifications
        self.update = update
        self.turnOn = turnOn
        _ownRelay = State(initialValue: relayDraft != nil || settings.relayURL != nil)
        _relayText = State(initialValue: relayDraft ?? settings.relayURL?.absoluteString ?? "")
    }

    private var relayInvalid: Bool {
        ownRelay && !relayText.isEmpty && PushEnvironment.customRelay(relayText) == nil
    }

    var body: some View {
        List {
            status
            relay
            Section {
                Toggle(settingsText("settings.notifications.live", "Live Activities"), isOn: settings.binding(\.liveActivities, update))
                    .frame(minHeight: 44)
            } footer: {
                Text(settingsText(
                    "settings.notifications.live.footer",
                    "Shows what each machine is doing on the Lock Screen and in the Dynamic Island."
                ))
            }
            .groupedRows()
            Section {
                Toggle(
                    settingsText("settings.notifications.approveUnlocked", "Approve without unlocking"),
                    isOn: settings.binding(\.approveWithoutUnlock, update)
                )
                .frame(minHeight: 44)
            } footer: {
                Text(settingsText(
                    "settings.notifications.approveUnlocked.footer",
                    "Anyone holding your locked iPhone could approve commands."
                ))
            }
            .groupedRows()
            Section {
                Picker(settingsText("settings.notifications.done", "Done"), selection: settings.binding(\.doneSilent, update)) {
                    Text(settingsText("settings.notifications.done.sound", "Sound")).tag(false)
                    Text(settingsText("settings.notifications.done.silent", "Silent")).tag(true)
                }
                .frame(minHeight: 44)
            }
            .groupedRows()
            if !notifications.machines.isEmpty {
                machines
            }
        }
        .groupedList()
        .navigationTitle(settingsText("settings.notifications.title", "Notifications"))
        .navigationBarTitleDisplayMode(.inline)
        .alert(
            settingsText("settings.notifications.commands.alert", "Show commands in Live Activities?"),
            isPresented: Binding { commandsFor != nil } set: { if !$0 { commandsFor = nil } },
            presenting: commandsFor
        ) { machine in
            Button(settingsText("settings.notifications.commands.confirm", "Show commands")) {
                setCommands(true, for: machine.id)
            }
            Button(settingsText("settings.notifications.commands.cancel", "Cancel"), role: .cancel) {}
        } message: { _ in
            Text(settingsText(
                "settings.notifications.commands.message",
                "Live Activities can't be encrypted. With this on, the command text passes the relay unencrypted. Use it with a relay you run yourself."
            ))
        }
    }

    // MARK: Sections

    private var status: some View {
        Section {
            LabeledContent(settingsText("settings.notifications.title", "Notifications"), value: statusText)
                .frame(minHeight: 44)
            switch notifications.authorization {
            case .denied:
                Button(settingsText("settings.notifications.openSettings", "Open Settings")) {
                    if let url = URL(string: UIApplication.openNotificationSettingsURLString) { openURL(url) }
                }
                .frame(minHeight: 44)
            case .notDetermined:
                Button(settingsText("settings.notifications.turnOn", "Turn on"), action: turnOn)
                    .frame(minHeight: 44)
            case .authorized, .provisional:
                EmptyView()
            }
        }
        .groupedRows()
    }

    private var statusText: String {
        switch notifications.authorization {
        case .authorized, .provisional: settingsText("settings.on", "On")
        case .denied: settingsText("settings.notifications.denied", "Off in iOS Settings")
        case .notDetermined: settingsText("settings.notifications.notAsked", "Not asked yet")
        }
    }

    private var relay: some View {
        Section {
            Picker(selection: $ownRelay) {
                Text(settingsText("settings.notifications.relay.hosted", "Togo relay")).tag(false)
                Text(settingsText("settings.notifications.relay.own", "Your own relay")).tag(true)
            } label: {
                Text(settingsText("settings.notifications.relay", "Relay"))
            }
            .pickerStyle(.inline)
            .labelsHidden()
            .onChange(of: ownRelay) { _, own in
                if own { saveRelay() } else { setRelay(nil) }
            }
            if ownRelay {
                TextField(
                    settingsText("settings.notifications.relay.field", "Relay address"),
                    text: $relayText, prompt: Text(verbatim: "https://relay.example.com")
                )
                .keyboardType(.URL)
                .textContentType(.URL)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .submitLabel(.done)
                .onSubmit(saveRelay)
                .frame(minHeight: 44)
            }
        } header: {
            heading(settingsText("settings.notifications.relay", "Relay"))
        } footer: {
            VStack(alignment: .leading, spacing: 6) {
                if relayInvalid {
                    Label(
                        settingsText("settings.notifications.relay.invalid", "Use an https address, like https://relay.example.com."),
                        systemImage: "exclamationmark.circle"
                    )
                    .foregroundStyle(Color.togoSnow)
                }
                Text(settingsText(
                    "settings.notifications.relay.footer",
                    "Notifications are sealed on your machine and opened on this iPhone, so the relay can't read them. It does see when they're sent, this iPhone's push address, and command text where you turn it on."
                ))
            }
        }
        .groupedRows()
        .onDisappear(perform: saveRelay)
    }

    private var machines: some View {
        NotificationMachinesSection(machines: notifications.machines, settings: settings) { machine, on in
            if on { commandsFor = machine } else { setCommands(false, for: machine.id) }
        }
    }

    private func heading(_ text: String) -> some View {
        Text(text)
            .font(.headline)
            .foregroundStyle(Color.togoSnow)
            .textCase(nil)
    }

    // MARK: Changes

    private func saveRelay() {
        guard ownRelay, let url = PushEnvironment.customRelay(relayText) else { return }
        setRelay(url)
    }

    private func setRelay(_ url: URL?) {
        guard settings.relayURL != url else { return }
        var changed = settings
        changed.relayURL = url
        update(changed)
    }

    private func setCommands(_ on: Bool, for machine: HostID) {
        var changed = settings
        if on {
            changed.liveActivityCommandMachines.insert(machine)
        } else {
            changed.liveActivityCommandMachines.remove(machine)
        }
        update(changed)
    }
}

/// Each machine's push state, with its command text switch where the helper sends Live
/// Activity pushes. Turning a switch on goes through `change`, which asks first.
struct NotificationMachinesSection: View {
    let machines: [NotificationMachine]
    let settings: AppSettings
    let change: (NotificationMachine, Bool) -> Void

    var body: some View {
        Section {
            ForEach(machines) { machine in
                row(machine)
                if machine.canShowCommands {
                    Toggle(isOn: Binding { settings.liveActivityCommand(for: machine.id) } set: { change(machine, $0) }) {
                        Text(settingsText("settings.notifications.commands", "Show command text"))
                        if let note = Self.commandNote(machine, showing: settings.liveActivityCommand(for: machine.id)) {
                            Text(note)
                                .font(.subheadline)
                                .foregroundStyle(Color.togoMute)
                        }
                    }
                    .frame(minHeight: 44)
                    .accessibilityHint(machine.name)
                }
            }
        } header: {
            Text(settingsText("settings.notifications.machines", "Machines"))
                .font(.headline)
                .foregroundStyle(Color.togoSnow)
                .textCase(nil)
        }
        .groupedRows()
    }

    @ViewBuilder private func row(_ machine: NotificationMachine) -> some View {
        let label = VStack(alignment: .leading, spacing: 2) {
            Text(machine.name)
            Text(Self.stateText(machine.state))
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
        }
        .frame(minHeight: 44, alignment: .leading)
        .accessibilityElement(children: .combine)
        if machine.hasSettings {
            NavigationLink {
                MachineHelperView(machine: machine.id, name: machine.name, supportsNotify: machine.supportsNotify) {
                    if machine.supportsAwake { MachineAwakeSection(machine: machine) }
                }
            } label: { label }
        } else {
            label
        }
    }

    /// Says a changed choice is on its way while the machine has not confirmed it.
    static func commandNote(_ machine: NotificationMachine, showing: Bool) -> String? {
        guard let confirmed = machine.helperCommand, confirmed != showing || machine.commandPending else { return nil }
        let name = machine.name
        return showing
            ? settingsText("settings.notifications.commands.pendingOn", "Turns on when \(name) receives it")
            : settingsText("settings.notifications.commands.pendingOff", "Turns off when \(name) receives it")
    }

    static func stateText(_ state: NotificationMachine.State) -> String {
        switch state {
        case .paired: settingsText("settings.notifications.paired", "Paired")
        case .notPaired: settingsText("settings.notifications.notPaired", "Not paired yet")
        case .helperTooOld: settingsText("settings.notifications.tooOld", "Helper too old for notifications")
        case .needsHelper: settingsText("settings.notifications.needsHelper", "Needs the Togo helper")
        case .offline: settingsText("settings.notifications.offline", "Offline")
        }
    }
}

#if DEBUG
extension NotificationValues {
    static var sample: NotificationValues {
        NotificationValues(
            authorization: .authorized,
            machines: [
                NotificationMachine(id: Samples.studio, name: "Studio", state: .paired),
                NotificationMachine(id: Samples.build, name: "Build box", state: .helperTooOld),
            ]
        )
    }
}

#Preview {
    NavigationStack {
        NotificationSettingsView(settings: AppSettings(), notifications: .sample, update: { _ in }, turnOn: {})
    }
    .togoControls()
}
#endif
