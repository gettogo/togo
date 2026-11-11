import SwiftUI
import TogoKit
import TogoTerminal

/// The edits the keyboard page makes, kept apart from the view so they can be tested.
struct TerminalKeysEditor: Equatable {
    var config: TerminalKeysConfig

    /// `agent` nil: for every agent.
    func shortcuts(for agent: String?) -> [KeyBarItem] {
        guard let agent else { return config.shortcuts }
        return config.shortcutsByAgent[agent.lowercased()] ?? []
    }

    mutating func add(_ item: KeyBarItem, for agent: String?) {
        edit(agent) { $0.append(item) }
    }

    mutating func move(from source: IndexSet, to destination: Int, for agent: String?) {
        edit(agent) { $0.move(fromOffsets: source, toOffset: destination) }
    }

    mutating func remove(at offsets: IndexSet, for agent: String?) {
        edit(agent) { $0.remove(atOffsets: offsets) }
    }

    private mutating func edit(_ agent: String?, _ change: (inout [KeyBarItem]) -> Void) {
        guard let agent else {
            change(&config.shortcuts)
            return
        }
        let id = agent.lowercased()
        var items = config.shortcutsByAgent[id] ?? []
        change(&items)
        config.shortcutsByAgent[id] = items.isEmpty ? nil : items
    }
}

/// Settings → Keyboard: the terminal's keys. Three choices, then the shortcuts above the
/// keyboard for every agent and for each agent that has its own.
struct KeyboardSettingsView: View {
    let keys: TerminalKeysConfig
    let update: (TerminalKeysConfig) -> Void
    /// Where the next shortcut goes: nil for every agent.
    @State private var target: String?

    var body: some View {
        List {
            Section {
                Picker(selection: binding(\.showColumn)) {
                    Text(settingsText("settings.keys.show.typing", "While typing")).tag(TerminalKeysConfig.ShowColumn.whileTyping)
                    Text(settingsText("settings.keys.show.demand", "Also on demand")).tag(TerminalKeysConfig.ShowColumn.alsoOnDemand)
                } label: {
                    Text(settingsText("settings.keys.show", "Show keys"))
                        .foregroundStyle(Color.togoSnow)
                }
                Toggle(isOn: binding(\.controller)) {
                    settingsRow(settingsText("settings.keys.controller", "Directional controller"),
                                settingsText("settings.keys.controller.detail", "Flick the controller to move the cursor; tap it for more keys."))
                }
                Toggle(isOn: binding(\.promptKeys)) {
                    settingsRow(settingsText("settings.keys.prompts", "Keys for prompts"),
                                settingsText("settings.keys.prompts.detail", "The answers a waiting prompt takes, above the keyboard."))
                }
            } header: {
                header(settingsText("settings.keys.header", "Terminal keys"))
            } footer: {
                SectionFooter {
                    Text(settingsText("settings.keys.footer", "On an iPhone Duo the controller sits in the side bar. Elsewhere the keys stand beside the terminal while the keyboard is up; \"Also on demand\" adds \"Keep keys on screen\" to a session's menu."))
                }
            }
            .groupedRows()
            shortcuts(for: nil, title: settingsText("settings.keys.shortcuts", "Shortcuts above the keyboard"))
            ForEach(keys.shortcutsByAgent.keys.sorted(), id: \.self) { agent in
                shortcuts(for: agent, title: "\(settingsText("settings.keys.shortcuts.agent", "Shortcuts")) · \(AgentCatalog.name(agent))")
            }
            Section {
                Picker(selection: $target) {
                    Text(KeysCopy.everyAgent).tag(String?.none)
                    ForEach(AgentCatalog.entries, id: \.id) { entry in
                        Text(entry.name).tag(String?.some(entry.id))
                    }
                } label: {
                    Text(settingsText("settings.keys.for", "For"))
                        .foregroundStyle(Color.togoSnow)
                }
                NavigationLink {
                    ShortcutBuilderView { item in edit { $0.add(item, for: target) } }
                } label: {
                    Label(settingsText("settings.keyboard.add", "Add shortcut"), systemImage: "plus")
                }
            } header: {
                header(settingsText("settings.keys.add", "Add"))
            } footer: {
                SectionFooter {
                    Text(settingsText("settings.keys.add.footer", "Shortcuts type into the terminal. Each one is also in the controller's palette."))
                }
            }
            .groupedRows()
        }
        .groupedList()
        .navigationTitle(settingsText("settings.keyboard.title", "Keyboard"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { editItem }
    }

    @ViewBuilder private func shortcuts(for agent: String?, title: String) -> some View {
        let items = TerminalKeysEditor(config: keys).shortcuts(for: agent)
        if agent == nil || !items.isEmpty {
            Section {
                if items.isEmpty {
                    Text(settingsText("settings.keys.none", "None yet"))
                        .foregroundStyle(Color.togoMute)
                        .frame(minHeight: 44)
                }
                ForEach(items) { item in
                    ShortcutRow(item: item)
                }
                .onMove { source, destination in edit { $0.move(from: source, to: destination, for: agent) } }
                .onDelete { offsets in edit { $0.remove(at: offsets, for: agent) } }
            } header: {
                header(title)
            }
            .groupedRows()
        }
    }

    private func header(_ text: String) -> some View {
        Text(text)
            .font(.headline)
            .foregroundStyle(Color.togoSnow)
            .textCase(nil)
    }

    private func settingsRow(_ title: String, _ detail: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(title)
                .foregroundStyle(Color.togoSnow)
            Text(detail)
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
        }
    }

    private func binding<Value>(_ path: WritableKeyPath<TerminalKeysConfig, Value>) -> Binding<Value> {
        Binding(get: { keys[keyPath: path] }, set: { value in
            var changed = keys
            changed[keyPath: path] = value
            update(changed)
        })
    }

    private func edit(_ change: (inout TerminalKeysEditor) -> Void) {
        var editor = TerminalKeysEditor(config: keys)
        change(&editor)
        update(editor.config)
    }
}

private struct ShortcutRow: View {
    let item: KeyBarItem

    var body: some View {
        HStack(spacing: 14) {
            Group {
                if let symbol = item.symbol {
                    Image(systemName: symbol)
                } else {
                    Image(systemName: "keyboard")
                        .hidden()
                }
            }
            .frame(width: 24)
            .foregroundStyle(Color.togoMute)
            .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(item.title)
                    .foregroundStyle(Color.togoSnow)
                if let detail {
                    Text(detail)
                        .font(.subheadline.monospaced())
                        .foregroundStyle(Color.togoMute)
                }
            }
        }
        .frame(minHeight: 44)
        .accessibilityElement(children: .combine)
    }

    private var detail: String? {
        guard case .steps(let steps) = item.action else { return nil }
        // "Esc" sends `Esc`; repeating the title under itself says nothing.
        let text = KeySequence.format(steps)
        return text == item.title ? nil : text
    }
}

extension KeyboardSettingsView {
    /// Edit turns into Done, text either way, so on an iPhone Duo it stays in a horizontal bar
    /// rather than jumping between axes.
    @ToolbarContentBuilder var editItem: some ToolbarContent {
        if #available(iOS 27.1, *) {
            ToolbarItem(placement: .primaryAction) { EditButton() }
                .axisBehavior(.horizontalOnly)
        } else {
            ToolbarItem(placement: .primaryAction) { EditButton() }
        }
    }
}

extension AgentCatalog {
    /// The agent's name for an id, or the id itself for one the catalog does not know.
    static func name(_ id: String) -> String {
        entries.first { $0.id == id }?.name ?? id
    }
}
