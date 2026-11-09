import SwiftUI
import TogoKit

/// What `agents.check` said about a preset's program on one machine.
enum AgentCheckState: Equatable {
    case checking
    /// The path the program was found at, when the helper said.
    case found(String?)
    case notFound
    case failed(String)
}

/// Adds or edits a preset: its name, which agent or program it starts and the flags it passes.
struct AgentEditor: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.machineTools) private var tools
    @Environment(\.agentLibrary) private var library
    @State private var draft: AgentPreset
    @State private var flags: String
    @State private var checks: [HostID: AgentCheckState]
    /// "Other program" was just chosen: its field takes the keyboard as it appears.
    @State private var programRevealed = false
    @FocusState private var programFocused: Bool
    /// Back was pressed with changes not saved: ask before they go.
    @State private var confirmsDiscard = false
    private let isNew: Bool
    private let original: AgentPreset
    private let originalFlags: String
    let machines: [SettingsMachine]
    let save: (AgentPreset) -> Void

    init(
        preset: AgentPreset?, machines: [SettingsMachine], checks: [HostID: AgentCheckState] = [:],
        save: @escaping (AgentPreset) -> Void
    ) {
        let start = preset ?? AgentPreset(name: "", agent: AgentPresetText.registry[0].id)
        _draft = State(initialValue: start)
        _flags = State(initialValue: ArgsParser.join(start.args))
        _checks = State(initialValue: checks)
        isNew = preset == nil
        original = start
        originalFlags = ArgsParser.join(start.args)
        self.machines = machines
        self.save = save
    }

    /// Something was changed since the editor opened and not saved yet.
    private var hasChanges: Bool { draft != original || flags != originalFlags }

    /// The agents to pick from, favorites first.
    private var registry: [(id: String, name: String)] {
        AgentPresetText.ordered(favorites: library?.favorites ?? [])
    }

    /// The machines a check can reach without failing or asking to sign in.
    static func checkable(_ machines: [SettingsMachine]) -> [SettingsMachine] {
        machines.filter(\.checksAgents)
    }

    var body: some View {
        List {
            Section {
                TextField(settingsText("settings.agents.name", "Name"), text: $draft.name)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .frame(minHeight: 44)
            }
            .groupedRows()
            Section {
                Picker(settingsText("settings.agents.agent", "Agent"), selection: $draft.agent) {
                    ForEach(registry, id: \.id) { entry in
                        Text(verbatim: entry.name).tag(entry.id)
                    }
                    Text(settingsText("settings.agents.otherProgram", "Other program")).tag(AgentPreset.custom)
                }
                .pickerStyle(.inline)
                .labelsHidden()
                .onChange(of: draft.isCustom) { _, custom in programRevealed = custom }
                if draft.isCustom {
                    TextField(settingsText("settings.agents.program", "Program"), text: programText)
                        .font(.system(.body, design: .monospaced))
                        .commandInput(programText)
                        .focusOnReveal($programFocused, revealed: programRevealed)
                        .frame(minHeight: 44)
                }
            } header: {
                Text(settingsText("settings.agents.agent", "Agent"))
            } footer: {
                if draft.isCustom {
                    Text(settingsText("settings.agents.program.footer", "A program name on the machine, or its full path."))
                }
            }
            .groupedRows()
            Section {
                FlagsField(text: $flags, error: flagsError, underlined: false)
            } footer: {
                Text(settingsText("settings.agents.flags.footer", "Each flag reaches the agent exactly as written."))
            }
            .groupedRows()
            if !Self.checkable(machines).isEmpty {
                Section {
                    ForEach(Self.checkable(machines)) { machine in
                        CheckRow(machine: machine, state: checks[machine.id], enabled: !draft.command.isEmpty) {
                            check(machine.id)
                        }
                    }
                }
                .groupedRows()
            }
        }
        .groupedList()
        .navigationTitle(isNew ? settingsText("settings.agents.new", "New agent") : draft.name)
        .navigationBarTitleDisplayMode(.inline)
        // With unsaved changes Back asks first, as Cancel does in the app's other editors; the
        // system Back (and its swipe) would drop them without a word.
        .navigationBarBackButtonHidden(hasChanges)
        .toolbar {
            if hasChanges {
                ToolbarItem(placement: .cancellationAction) {
                    Button(settingsText("settings.agents.back", "Back"), systemImage: "chevron.backward") {
                        confirmsDiscard = true
                    }
                    .accessibilityIdentifier("settings.agents.back")
                }
            }
            ToolbarItem(placement: .confirmationAction) {
                Button(settingsText("settings.agents.save", "Save"), systemImage: "checkmark", role: .confirm) {
                    guard let preset = finished else { return }
                    save(preset)
                    dismiss()
                }
                .disabled(finished == nil)
            }
        }
        .confirmationDialog(
            settingsText("settings.agents.discardTitle", "Discard your changes?"),
            isPresented: $confirmsDiscard, titleVisibility: .visible
        ) {
            Button(settingsText("settings.agents.discard", "Discard changes"), role: .destructive) { dismiss() }
            Button(settingsText("settings.agents.keepEditing", "Keep editing"), role: .cancel) {}
        }
        .onChange(of: draft.agent) { checks = [:] }
        .onChange(of: draft.program) { checks = [:] }
    }

    private var programText: Binding<String> {
        Binding { draft.program ?? "" } set: { draft.program = $0.trimmingCharacters(in: .whitespacesAndNewlines) }
    }

    private var flagsError: String? { FlagsField.problem(flags) }

    private var finished: AgentPreset? { Self.preset(from: draft, flags: flags) }

    /// The preset to save, or nil while something required is missing or the flags could not
    /// start the agent.
    static func preset(from draft: AgentPreset, flags: String) -> AgentPreset? {
        var preset = draft
        preset.name = preset.name.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !preset.name.isEmpty, FlagsField.problem(flags) == nil, let args = try? ArgsParser.split(flags) else {
            return nil
        }
        preset.args = args
        if preset.isCustom {
            guard let program = preset.program, !program.isEmpty else { return nil }
        } else {
            preset.program = nil
        }
        return preset
    }

    private func check(_ host: HostID) {
        let program = draft.command
        guard !program.isEmpty else { return }
        checks[host] = .checking
        Task {
            let state: AgentCheckState
            do {
                let found = try await tools.call(host) { try await $0.checkAgent(program: program) }
                state = found.installed ? .found(found.path) : .notFound
            } catch {
                state = .failed(ServiceErrors.message(error))
            }
            // A newer check, or a change of program, replaces this answer.
            if checks[host] == .checking, draft.command == program { checks[host] = state }
        }
    }
}

/// `Check on <machine>` and what the machine answered.
private struct CheckRow: View {
    let machine: SettingsMachine
    let state: AgentCheckState?
    let enabled: Bool
    let action: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Button(settingsText("settings.agents.check", "Check on \(machine.name)"), action: action)
                .disabled(!enabled || state == .checking)
                .frame(minHeight: 44, alignment: .leading)
            if let state {
                result(state)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
                    .fixedSize(horizontal: false, vertical: true)
                    .contentTransition(.interpolate)
                    // motion: cause → effect — the tapped Check shows its result under it
                    .transition(.opacity)
            }
        }
        .padding(.vertical, 2)
        // The result line arrives on `resize`.
        .resizesOnRows(state != nil)
        // motion: cause → effect — Checking → Found swaps its words in place
        .motion(.respond, value: state.map { CaseName.of($0) })
    }

    @ViewBuilder private func result(_ state: AgentCheckState) -> some View {
        switch state {
        case .checking:
            Text(settingsText("settings.agents.checking", "Checking"))
        case .found(let path?):
            Text(Self.foundAt(path))
        case .found(nil):
            Text(settingsText("settings.agents.foundOn", "Found on \(machine.name)"))
        case .notFound:
            Text(settingsText("settings.agents.notFound", "Not found on \(machine.name)"))
        case .failed(let message):
            Text(message)
        }
    }
}

extension CheckRow {
    /// "Found at <path>" with the path, a machine fact, in SF Mono.
    static func foundAt(_ path: String) -> AttributedString {
        var line = AttributedString(settingsText("settings.agents.found", "Found at \(path)"))
        if let range = line.range(of: path, options: .backwards) {
            line[range].font = .system(.subheadline, design: .monospaced)
        }
        return line
    }
}

#if DEBUG
#Preview {
    NavigationStack {
        AgentEditor(
            preset: AgentPreset(name: "Kimi", agent: AgentPreset.custom, program: "kimi", args: ["--yolo"]),
            machines: SettingsValues.sample.machines,
            checks: [Samples.studio: .found("/opt/homebrew/bin/kimi"), Samples.build: .notFound]
        ) { _ in }
    }
}
#endif
