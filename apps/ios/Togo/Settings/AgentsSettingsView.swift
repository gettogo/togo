import SwiftUI
import TogoKit

/// Saved agents: a registry agent or any program, each with its flags. Presets sync with the
/// user's other devices through iCloud.
struct AgentsSettingsView: View {
    let library: AgentLibrary
    let machines: [SettingsMachine]
    @State private var deleting: AgentPreset?
    @State private var adding = false

    var body: some View {
        Group {
            if library.presets.isEmpty {
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
        .confirmationDialog(
            settingsText("settings.agents.delete.title", "Delete \(deleting?.name ?? "")?"),
            isPresented: Binding { deleting != nil } set: { if !$0 { deleting = nil } },
            titleVisibility: .visible,
            presenting: deleting
        ) { preset in
            Button(settingsText("settings.agents.delete.confirm", "Delete \(preset.name)"), role: .destructive) {
                deleting = nil
                Task { await library.delete(preset.id) }
            }
        } message: { _ in
            Text(settingsText("settings.agents.delete.message", "Sessions already started with it keep running."))
        }
    }

    private var list: some View {
        List {
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
                Text(settingsText("settings.agents.footer", "Tap an agent to edit it. Swipe left to delete it."))
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
                .lineLimit(2)
                .truncationMode(.middle)
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

    /// `registry` with the user's favorites first, then recent agents, then the rest by name.
    static func ordered(favorites: [String], recent: [String]) -> [(id: String, name: String)] {
        AgentOrder.sorted(registry, id: { $0.id }, name: { $0.name }, favorites: favorites, recent: recent)
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
