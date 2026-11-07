import SwiftUI
import TogoTerminal

/// The open terminal's input line: `>`, a field that sends on return, and the terminal keys
/// under it while no hardware keyboard is attached.
struct PromptInputView: View {
    @Environment(\.hardwareKeyboard) private var keyboard
    @Bindable var model: PromptBarModel
    let tab: TabID
    @FocusState private var focused: Bool

    var body: some View {
        let placeholder = PromptCopy.message(model.tabName(tab))
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 10) {
                PromptChevron()
                TextField(text: $model.inputDraft, prompt: Text(placeholder).foregroundStyle(Color.togoMute)) {
                    Text(placeholder)
                }
                .font(.body)
                .foregroundStyle(Color.togoSnow)
                .focused($focused)
                .submitLabel(.send)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .onSubmit {
                    model.sendInput()
                    focused = true
                }
            }
            .padding(.horizontal, 16)
            .frame(minHeight: 50)
            let keys = model.inputKeys
            if !keyboard.hardwareKeyboardConnected, !keys.isEmpty {
                PromptKeyRow(keys: keys) { model.tapKey($0) }
            }
        }
        .onChange(of: focused, initial: true) { _, focused in model.barTextFocused = focused }
        .onDisappear { model.barTextFocused = false }
    }
}

/// The terminal keys as small capsules, scrolling sideways when they outgrow the bar.
struct PromptKeyRow: View {
    let keys: [KeyBarItem]
    let onTap: (KeyBarItem) -> Void

    var body: some View {
        ScrollView(.horizontal) {
            HStack(spacing: 6) {
                ForEach(keys) { item in
                    Button { onTap(item) } label: {
                        Group {
                            if let symbol = item.symbol {
                                Image(systemName: symbol)
                                    .font(.caption2.weight(.semibold))
                            } else {
                                Text(verbatim: item.title)
                                    .font(.system(.caption2, design: .monospaced))
                            }
                        }
                        .foregroundStyle(Color.togoMute)
                        .padding(.horizontal, 10)
                        .frame(minWidth: 36, minHeight: 28)
                        .background(Color.togoSnow.opacity(0.06), in: .capsule)
                        .frame(minHeight: 44)
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel(item.title)
                }
            }
            .padding(.horizontal, 12)
        }
        .scrollIndicators(.hidden)
        .padding(.bottom, 2)
    }
}
