import SwiftUI
import TogoTerminal

enum KeyBarPreset: CaseIterable, Sendable {
    case standard, claude, omp

    var title: String {
        switch self {
        case .standard: settingsText("settings.keyboard.preset.default", "Default")
        case .claude: "Claude"
        case .omp: "OMP"
        }
    }

    var config: KeyBarConfig {
        switch self {
        case .standard: .standard
        case .claude: .claude
        case .omp: .omp
        }
    }
}

/// The edits the keyboard bar page makes, kept apart from the view so they can be tested.
struct KeyBarEditor: Equatable {
    var config: KeyBarConfig

    /// The preset the bar still matches exactly, or nil once the user changed anything.
    var activePreset: KeyBarPreset? {
        KeyBarPreset.allCases.first { $0.config == config }
    }

    mutating func apply(_ preset: KeyBarPreset) {
        config = preset.config
    }

    mutating func move(from source: IndexSet, to destination: Int) {
        config.items.move(fromOffsets: source, toOffset: destination)
    }

    mutating func remove(at offsets: IndexSet) {
        config.items.remove(atOffsets: offsets)
    }

    mutating func add(_ item: KeyBarItem) {
        config.items.append(item)
    }
}

struct KeyboardSettingsView: View {
    let keyBar: KeyBarConfig
    let update: (KeyBarConfig) -> Void
    @State private var replacing: KeyBarPreset?

    var body: some View {
        List {
            Section {
                ForEach(keyBar.items) { item in
                    KeyBarItemRow(item: item)
                }
                .onMove { source, destination in edit { $0.move(from: source, to: destination) } }
                .onDelete { offsets in edit { $0.remove(at: offsets) } }
                NavigationLink {
                    ShortcutBuilderView { item in edit { $0.add(item) } }
                } label: {
                    Label(settingsText("settings.keyboard.add", "Add shortcut"), systemImage: "plus")
                }
            } footer: {
                Text(settingsText("settings.keyboard.footer", "These keys sit above the keyboard in every session. Tap Edit to reorder them."))
            }
            .groupedRows()
            Section {
                ForEach(KeyBarPreset.allCases, id: \.self) { preset in
                    presetRow(preset)
                }
            } header: {
                Text(settingsText("settings.keyboard.presets", "Presets"))
                    .font(.headline)
                    .foregroundStyle(Color.togoSnow)
                    .textCase(nil)
            } footer: {
                Text(settingsText("settings.keyboard.presets.footer", "A preset replaces the keys above."))
            }
            .groupedRows()
        }
        .groupedList()
        .navigationTitle(settingsText("settings.keyboard.title", "Keyboard bar"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { EditButton() }
        .confirmationDialog(
            settingsText("settings.keyboard.replace.title", "Replace your keys?"),
            isPresented: Binding { replacing != nil } set: { if !$0 { replacing = nil } },
            titleVisibility: .visible,
            presenting: replacing
        ) { preset in
            Button(settingsText("settings.keyboard.replace.confirm", "Use \(preset.title)"), role: .destructive) {
                edit { $0.apply(preset) }
            }
        } message: { _ in
            Text(settingsText("settings.keyboard.replace.message", "Your changes to the bar will be lost."))
        }
    }

    private func presetRow(_ preset: KeyBarPreset) -> some View {
        let active = KeyBarEditor(config: keyBar).activePreset
        return Button {
            // A bar that still matches a preset has nothing to lose, so it switches at once.
            if active == nil {
                replacing = preset
            } else {
                edit { $0.apply(preset) }
            }
        } label: {
            HStack {
                Text(preset.title)
                    .foregroundStyle(Color.togoSnow)
                Spacer(minLength: 8)
                if active == preset {
                    Image(systemName: "checkmark")
                        .fontWeight(.semibold)
                        .foregroundStyle(Color.togoSnow)
                }
            }
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
        .accessibilityAddTraits(active == preset ? .isSelected : [])
    }

    private func edit(_ change: (inout KeyBarEditor) -> Void) {
        var editor = KeyBarEditor(config: keyBar)
        change(&editor)
        update(editor.config)
    }
}

private struct KeyBarItemRow: View {
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
                        .font(isSequence ? .subheadline.monospaced() : .subheadline)
                        .foregroundStyle(Color.togoMute)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }
        }
        .frame(minHeight: 44)
        .accessibilityElement(children: .combine)
    }

    private var isSequence: Bool {
        if case .steps = item.action { true } else { false }
    }

    private var detail: String? {
        switch item.action {
        case .steps(let steps):
            // "Esc" sends `Esc`; repeating the title under itself says nothing.
            KeySequence.format(steps) == item.title ? nil : KeySequence.format(steps)
        case .ctrlLatch: settingsText("settings.keyboard.latch.ctrl", "Holds Ctrl for the next key")
        case .altLatch: settingsText("settings.keyboard.latch.alt", "Holds Option for the next key")
        case .paste, .hideKeyboard, .dpad, .compose, .dictate: nil
        }
    }
}
