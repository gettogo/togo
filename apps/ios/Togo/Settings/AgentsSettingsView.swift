import SwiftUI
import TogoKit

/// Saved agents: a registry agent or any program, each with its flags. Presets sync with the
/// user's other devices through iCloud.
struct AgentsSettingsView: View {
    let library: AgentLibrary
    let machines: [SettingsMachine]
    @State private var deleting: AgentPreset?
    @State private var adding = false

    /// Machines whose helper can set agents up, for the integrations rows.
    private var helperMachines: [SettingsMachine] {
        machines.filter { if case .present = $0.helper { true } else { false } }
    }

    var body: some View {
        Group {
            if library.presets.isEmpty && helperMachines.isEmpty {
                StateView(
                    kind: .empty,
                    title: settingsText("settings.agents.empty.title", "No saved agents"),
                    message: settingsText(
                        "settings.agents.empty.message", "Save an agent with its flags to start it from New session."
                    ),
                    action: .init(settingsText("settings.agents.add", "Add an agent"), run: { adding = true })
                )
                .background(Color.togoInk)
            } else {
                list
            }
        }
        .navigationTitle(settingsText("settings.agents.title", "Agents"))
        .navigationBarTitleDisplayMode(.inline)
        .navigationDestination(isPresented: $adding) { editor(nil) }
        .coordinated("settings.deleteAgent", .alert, isPresented: Binding { deleting != nil } set: { if !$0 { deleting = nil } }) { view, shown in
            view.alert(
                settingsText("settings.agents.delete.title", "Delete \(deleting?.name ?? "")?"),
                isPresented: shown,
                presenting: deleting
            ) { preset in
                Button(settingsText("settings.agents.delete.confirm", "Delete \(preset.name)"), role: .destructive) {
                    deleting = nil
                    Task { await library.delete(preset.id) }
                }
                Button(settingsText("settings.confirm.cancel", "Cancel"), role: .cancel) {}
            } message: { _ in
                Text(settingsText("settings.agents.delete.message", "Sessions already started with it keep running."))
            }
        }
    }

    private var list: some View {
        List {
            if !helperMachines.isEmpty {
                Section {
                    ForEach(helperMachines) { machine in
                        NavigationLink {
                            MachineIntegrationsPage(machine: machine.id, name: machine.name)
                        } label: {
                            Text(verbatim: machine.name)
                                .frame(minHeight: 44, alignment: .leading)
                        }
                    }
                } header: {
                    Text(IntegrationsCopy.title)
                } footer: {
                    SectionFooter {
                        Text(settingsText(
                            "settings.agents.integrations.footer", "Set up each agent on a machine so its chat and requests reach this device."
                        ))
                    }
                }
                .groupedRows()
            }
            Section {
                ForEach(library.presets) { preset in
                    NavigationLink {
                        editor(preset)
                    } label: {
                        AgentPresetRow(preset: preset)
                    }
                    // No destructive role here: it would take the row away before the question is
                    // answered, and a cancelled delete would leave a gap.
                    .swipeActions {
                        Button {
                            deleting = preset
                        } label: {
                            Label(settingsText("settings.agents.delete", "Delete"), systemImage: "trash")
                        }
                        .tint(.red)
                    }
                    .contextMenu {
                        Button(settingsText("settings.agents.delete", "Delete"), systemImage: "trash", role: .destructive) {
                            deleting = preset
                        }
                    }
                    .accessibilityAction(named: settingsText("settings.agents.delete", "Delete")) { deleting = preset }
                }
                Button {
                    adding = true
                } label: {
                    Label(settingsText("settings.agents.add", "Add an agent"), systemImage: "plus")
                }
                .frame(minHeight: 44)
            } footer: {
                SectionFooter {
                    Text(settingsText("settings.agents.footer", "Tap an agent to edit it. Swipe left to delete it."))
                }
            }
            .groupedRows()
        }
        .groupedList()
    }

    private func editor(_ preset: AgentPreset?) -> some View {
        AgentEditor(preset: preset, machines: machines) { saved in
            Task { await library.save(saved) }
        }
    }
}

private struct AgentPresetRow: View {
    let preset: AgentPreset

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(verbatim: preset.name)
                .foregroundStyle(Color.togoSnow)
            Text(verbatim: AgentPresetText.commandLine(preset))
                .font(.system(.subheadline, design: .monospaced))
                .foregroundStyle(Color.togoMute)
        }
        .padding(.vertical, 4)
        .frame(minHeight: 44, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

/// How presets read on screen.
enum AgentPresetText {
    /// The agents the helper knows by id, in its registry order. The shell takes no flags, so
    /// no preset runs it.
    static let registry: [(id: String, name: String)] = AgentCatalog.entries
        .filter { !$0.command.isEmpty }
        .map { ($0.id, $0.name) }

    /// `registry` with the user's favorites first, then the rest, each by name.
    static func ordered(favorites: [String]) -> [(id: String, name: String)] {
        AgentOrder.sections(registry, id: { $0.id }, name: { $0.name }, favorites: favorites).all
    }

    /// The program and its flags as one line, quoted only where a word needs it.
    static func commandLine(_ preset: AgentPreset) -> String {
        ([preset.command].filter { !$0.isEmpty } + [ArgsParser.join(preset.args)].filter { !$0.isEmpty })
            .joined(separator: " ")
    }
}

#if DEBUG
#Preview {
    NavigationStack {
        AgentsSettingsView(
            library: .inMemory([
                AgentPreset(name: "Claude, no prompts", agent: "claude", args: ["--dangerously-skip-permissions"]),
                AgentPreset(name: "Kimi", agent: AgentPreset.custom, program: "kimi", args: ["--yolo"]),
            ]),
            machines: SettingsValues.sample.machines
        )
    }
}
#endif
