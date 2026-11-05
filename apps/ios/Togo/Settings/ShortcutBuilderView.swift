import SwiftUI
import TogoTerminal

/// A shortcut being built: one key with modifiers, or a run of literal text. Choosing one
/// clears the other, so what the preview shows is always exactly what the key sends.
struct ShortcutDraft: Equatable {
    enum Key: CaseIterable, Sendable {
        case escape, tab, enter, backspace, up, down, left, right, home, end, pageUp, pageDown

        var terminalKey: TerminalKey {
            switch self {
            case .escape: .escape
            case .tab: .tab
            case .enter: .enter
            case .backspace: .backspace
            case .up: .up
            case .down: .down
            case .left: .left
            case .right: .right
            case .home: .home
            case .end: .end
            case .pageUp: .pageUp
            case .pageDown: .pageDown
            }
        }

        var title: String {
            switch self {
            case .escape: "Esc"
            case .tab: "Tab"
            case .enter: "Enter"
            case .backspace: "Backspace"
            case .up: "Up"
            case .down: "Down"
            case .left: "Left"
            case .right: "Right"
            case .home: "Home"
            case .end: "End"
            case .pageUp: "PgUp"
            case .pageDown: "PgDn"
            }
        }

        /// What VoiceOver reads for a key drawn as a symbol.
        var spokenTitle: String {
            switch self {
            case .up, .down, .left, .right: settingsText("settings.builder.arrow", "\(title) arrow")
            default: title
            }
        }

        var symbol: String? {
            switch self {
            case .up: "arrow.up"
            case .down: "arrow.down"
            case .left: "arrow.left"
            case .right: "arrow.right"
            case .backspace: "delete.left"
            default: nil
            }
        }
    }

    var ctrl = false
    var option = false
    var shift = false
    var key: Key? {
        didSet { if key != nil { customText = "" } }
    }
    var customText = "" {
        didSet { if !customText.isEmpty { key = nil } }
    }
    var label = ""

    var steps: [KeyStep] {
        if !customText.isEmpty { return [.text(customText)] }
        guard let key else { return [] }
        return [.stroke(KeyStroke(key.terminalKey, modifiers))]
    }

    /// The bytes the steps send, from the same encoder the terminal uses.
    var preview: String {
        steps.compactMap { step -> String? in
            switch step {
            case .stroke(let stroke): ByteNotation.describe(KeyEncoder.encode(stroke, applicationCursor: false))
            case .text(let text): ByteNotation.literal(text)
            case .delay: nil
            }
        }
        .joined(separator: " ")
    }

    var suggestedTitle: String {
        if !customText.isEmpty { return String(customText.prefix(12)) }
        guard let key else { return "" }
        let names = [(ctrl, "Ctrl"), (option, "Option"), (shift, "Shift")].filter(\.0).map(\.1)
        return (names + [key.title]).joined(separator: " ")
    }

    /// Nil until there is something to send.
    var item: KeyBarItem? {
        guard !steps.isEmpty else { return nil }
        let title = label.trimmingCharacters(in: .whitespacesAndNewlines)
        let symbol = customText.isEmpty && modifiers.isEmpty ? key?.symbol : nil
        return KeyBarItem(title: title.isEmpty ? suggestedTitle : title, symbol: symbol, action: .steps(steps))
    }

    private var modifiers: KeyModifiers {
        var modifiers: KeyModifiers = []
        if ctrl { modifiers.insert(.ctrl) }
        if option { modifiers.insert(.alt) }
        if shift { modifiers.insert(.shift) }
        return modifiers
    }
}

/// Bytes as space-separated tokens: control bytes by name (`ESC`, `TAB`, `^C`), space as
/// `SP`, everything else as the character it is. Typed text is shown quoted, so letters that
/// spell `ESC` never look like the byte.
enum ByteNotation {
    private static let names: [UInt8: String] = [
        0x00: "NUL", 0x08: "BS", 0x09: "TAB", 0x0A: "LF", 0x0D: "CR", 0x1B: "ESC", 0x20: "SP", 0x7F: "DEL",
    ]

    static func describe(_ bytes: [UInt8]) -> String {
        String(decoding: bytes, as: UTF8.self).unicodeScalars.map { scalar in
            guard scalar.isASCII, let byte = UInt8(exactly: scalar.value) else { return String(scalar) }
            if let name = names[byte] { return name }
            if byte < 0x20 { return "^" + String(UnicodeScalar(byte + 0x40)) }
            return String(scalar)
        }
        .joined(separator: " ")
    }

    /// Printable runs in double quotes with `\"` and `\\` escaped; control characters between
    /// them by name, as `describe` names them.
    static func literal(_ text: String) -> String {
        var tokens: [String] = []
        var run = ""
        func flush() {
            guard !run.isEmpty else { return }
            let escaped = run.replacingOccurrences(of: "\\", with: "\\\\").replacingOccurrences(of: "\"", with: "\\\"")
            tokens.append("\"\(escaped)\"")
            run = ""
        }
        for scalar in text.unicodeScalars {
            if scalar.value < 0x20 || scalar.value == 0x7F {
                flush()
                tokens.append(describe([UInt8(scalar.value)]))
            } else {
                run.unicodeScalars.append(scalar)
            }
        }
        flush()
        return tokens.joined(separator: " ")
    }
}

struct ShortcutBuilderView: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let onAdd: (KeyBarItem) -> Void
    @State private var draft: ShortcutDraft

    init(draft: ShortcutDraft = ShortcutDraft(), onAdd: @escaping (KeyBarItem) -> Void) {
        _draft = State(initialValue: draft)
        self.onAdd = onAdd
    }

    var body: some View {
        List {
            Section {
                Text(draft.steps.isEmpty
                     ? settingsText("settings.builder.preview.empty", "Choose a key or type some text.")
                     : draft.preview)
                    .font(draft.steps.isEmpty ? .body : .body.monospaced())
                    .foregroundStyle(draft.steps.isEmpty ? Color.togoMute : Color.togoSnow)
                    .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                    .accessibilityLabel(draft.steps.isEmpty
                        ? settingsText("settings.builder.preview.empty", "Choose a key or type some text.")
                        : settingsText("settings.builder.preview.label", "Sends \(draft.preview)"))
            } footer: {
                Text(settingsText("settings.builder.preview.footer", "What the terminal receives."))
            }
            .groupedRows()
            Section {
                Group {
                    Toggle("Ctrl", isOn: $draft.ctrl)
                    Toggle("Option", isOn: $draft.option)
                    Toggle("Shift", isOn: $draft.shift)
                }
                // Modifiers have no meaning for literal text.
                .disabled(!draft.customText.isEmpty)
                keyGrid
                    .listRowInsets(EdgeInsets(top: 12, leading: 16, bottom: 12, trailing: 16))
            }
            .groupedRows()
            Section {
                TextField(settingsText("settings.builder.text", "Custom text"), text: $draft.customText, axis: .vertical)
                    .font(.body.monospaced())
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
            } footer: {
                Text(settingsText("settings.builder.text.footer", "Sent exactly as typed, in place of a key."))
            }
            .groupedRows()
            Section {
                TextField(
                    settingsText("settings.builder.label", "Label"), text: $draft.label,
                    prompt: Text(draft.suggestedTitle.isEmpty ? settingsText("settings.builder.label", "Label") : draft.suggestedTitle)
                )
            } footer: {
                Text(settingsText("settings.builder.label.footer", "Shown on the key bar."))
            }
            .groupedRows()
        }
        .groupedList()
        .navigationTitle(settingsText("settings.builder.title", "New shortcut"))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .confirmationAction) {
                Button(settingsText("settings.builder.add", "Add")) {
                    if let item = draft.item {
                        onAdd(item)
                        dismiss()
                    }
                }
                .disabled(draft.item == nil)
            }
        }
    }

    private var keyGrid: some View {
        let columns = dynamicTypeSize.isAccessibilitySize
            ? [GridItem(.flexible()), GridItem(.flexible())]
            : Array(repeating: GridItem(.flexible(), spacing: 8), count: 4)
        return LazyVGrid(columns: columns, spacing: 8) {
            ForEach(ShortcutDraft.Key.allCases, id: \.self) { key in
                KeyCap(key: key, selected: draft.key == key) {
                    draft.key = draft.key == key ? nil : key
                }
            }
        }
    }
}

private struct KeyCap: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let key: ShortcutDraft.Key
    let selected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Group {
                if let symbol = key.symbol {
                    Image(systemName: symbol)
                        .accessibilityLabel(key.spokenTitle)
                } else {
                    Text(key.title)
                }
            }
            .font(.body.weight(.medium))
            .lineLimit(1)
            .minimumScaleFactor(0.5)
            .foregroundStyle(selected ? Color.togoInk : Color.togoSnow)
            .frame(maxWidth: .infinity, minHeight: 44)
            .background(selected ? Color.togoSnow : Color.clear, in: .rect(cornerRadius: 8))
            .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(Color.togoLine))
            .contentShape(.rect(cornerRadius: 8))
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(selected ? .isSelected : [])
        .animation(Motion.motion(Motion.confirm, reduce: reduceMotion), value: selected)
    }
}

#if DEBUG
#Preview {
    var draft = ShortcutDraft()
    draft.ctrl = true
    draft.key = .up
    return NavigationStack { ShortcutBuilderView(draft: draft) { _ in } }
}
#endif
