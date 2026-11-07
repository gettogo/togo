import SwiftUI
import TogoTerminal

/// The bar with nothing waiting and no terminal open: `> New session`. Tapped, it opens into a
/// field with up to three places to start, and a way to the full New session sheet.
struct PromptStartView: View {
    @Environment(\.hardwareKeyboard) private var keyboard
    @Environment(\.loopsPaused) private var loopsPaused
    @Bindable var model: PromptBarModel
    let expanded: Bool
    @FocusState private var focused: Bool

    var body: some View {
        if expanded {
            open
        } else {
            closed
        }
    }

    private var closed: some View {
        Button { model.openStart() } label: {
            HStack(spacing: 10) {
                PromptChevron()
                Text(PromptCopy.newSession)
                    .font(.body)
                    .foregroundStyle(Color.togoMute)
                    .frame(maxWidth: .infinity, alignment: .leading)
                if let hint = keyboard.hint("⌘T") {
                    Text(verbatim: hint)
                        .font(.system(.caption2, design: .monospaced))
                        .foregroundStyle(Color.togoMuteDim)
                        .accessibilityHidden(true)
                }
            }
            .padding(.horizontal, 16)
            .frame(minHeight: 50)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(PromptCopy.newSession)
    }

    private var open: some View {
        let placeholder = PromptCopy.folderOrAgent
        return VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 10) {
                PromptChevron()
                TextField(text: $model.startQuery, prompt: Text(placeholder).foregroundStyle(Color.togoMute)) {
                    Text(placeholder)
                }
                .font(.body)
                .foregroundStyle(Color.togoSnow)
                .focused($focused)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .submitLabel(.go)
                .onSubmit { model.pickFirst() }
                .onKeyPress(.escape) {
                    model.cancelStart()
                    return .handled
                }
                Button(PromptCopy.cancel) { model.cancelStart() }
                    .buttonStyle(.promptCapsule(.plain))
            }
            .padding(.leading, 16)
            .padding(.trailing, 8)
            .padding(.vertical, 3)
            VStack(alignment: .leading, spacing: 0) {
                ForEach(model.visibleSuggestions) { suggestion in
                    Button { model.pick(suggestion) } label: {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(PromptCopy.agentOn(suggestion.agentName, machine: suggestion.machineName))
                                .font(.subheadline)
                                .foregroundStyle(Color.togoSnow)
                            Text(verbatim: suggestion.folder)
                                .font(.system(.caption, design: .monospaced))
                                .foregroundStyle(Color.togoMute)
                                .lineLimit(1)
                                .truncationMode(.middle)
                        }
                        .padding(.horizontal, 10)
                        .padding(.vertical, 8)
                        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                        .contentShape(.rect(cornerRadius: 12))
                    }
                    .buttonStyle(PromptRowStyle())
                }
                Button { model.openStartSheet() } label: {
                    Text(PromptCopy.moreOptions)
                        .font(.subheadline)
                        .foregroundStyle(Color.togoMute)
                        .padding(.horizontal, 10)
                        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                        .contentShape(.rect(cornerRadius: 12))
                }
                .buttonStyle(PromptRowStyle())
            }
            .padding(.horizontal, 8)
            .padding(.bottom, 8)
        }
        // A still snapshot leaves the field unfocused, so no caret blinks in it.
        .onAppear { focused = !loopsPaused }
        .onChange(of: focused, initial: true) { _, focused in model.barTextFocused = focused }
        .onDisappear { model.barTextFocused = false }
    }
}

/// A suggestion row: no fill until pressed, then a faint snow wash.
private struct PromptRowStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .background(
                Color.togoSnow.opacity(configuration.isPressed ? 0.06 : 0),
                in: .rect(cornerRadius: 12, style: .continuous)
            )
    }
}
