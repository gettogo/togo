import SwiftUI
import TogoKit

/// The rows the agent's TUI draws around its input line while the chat composer holds a "/"
/// command: its own menu, as it draws it, cropped to its rows. Shown above the composer
/// where the `/` menu shows, on every layout. A tap on a row takes it in the TUI; the close
/// control empties the TUI's line and puts the strip away.
struct TerminalPeekStrip: View {
    @Environment(\.displayScale) private var displayScale
    @Environment(\.colorSchemeContrast) private var contrast
    let model: CommandLineModel
    @State private var contentHeight: CGFloat = CommandMenuPanel.cap

    var body: some View {
        let rows = model.peekRows
        let screen = model.peek.screen
        let selected = screen.flatMap { PeekSync.selected($0, among: rows) }
        let texts = PeekSync.cropped(rows.map { row in screen?.rows[row].text ?? "" })
        VStack(spacing: 0) {
            header
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(zip(rows, texts)), id: \.0) { row, text in
                        Button { Task { await model.takePeekRow(row) } } label: {
                            PeekRow(text: text, selected: row == selected, agent: agent)
                        }
                        .buttonStyle(.plain)
                        .hoverEffect(.highlight)
                    }
                }
                .padding(.bottom, 6)
            }
            .scrollBounceBehavior(.basedOnSize)
            .scrollDismissesKeyboard(.interactively)
            .onScrollGeometryChange(for: CGFloat.self) { $0.contentSize.height } action: { _, height in
                contentHeight = height
            }
            // As the `/` menu: its own height up to the cap, and less when the window is short,
            // so the field keeps its place (a folded Duo with the keyboard up).
            .frame(minHeight: 0, idealHeight: min(contentHeight, CommandMenuPanel.cap),
                   maxHeight: min(contentHeight, CommandMenuPanel.cap))
        }
        .background(Color.togoRaised, in: .rect(cornerRadius: 20, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 20, style: .continuous)
                .strokeBorder(Color.togoLine, lineWidth: Contrast.hairline(displayScale, contrast))
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(ComposerCopy.peekTitle(agent))
        .accessibilityIdentifier("composer.peek")
    }

    /// Whose menu this is, and the way back: always visible.
    private var header: some View {
        HStack(spacing: 8) {
            Label(ComposerCopy.peekTitle(agent), systemImage: "terminal")
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Color.togoMute)
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityAddTraits(.isHeader)
            Button(ComposerCopy.closeMenu, systemImage: "xmark") { model.cancelPeek() }
                .labelStyle(.iconOnly)
                .font(.footnote.weight(.bold))
                .foregroundStyle(Color.togoMute)
                .frame(width: 44, height: 44)
                .contentShape(.rect)
                .padding(.vertical, -4)
                .buttonStyle(.plain)
                .hoverEffect(.highlight)
                .accessibilityHint(ComposerCopy.peekCloseHint(agent))
                .accessibilityIdentifier("composer.peek.close")
        }
        .padding(.leading, 14)
        .padding(.top, 2)
    }

    private var agent: String {
        let name = model.store.agentName
        return name.isEmpty ? ComposerCopy.peekAgentFallback : name
    }
}

/// One row as the TUI drew it, in mono, its selection marked as the `/` menu marks the
/// keyboard's row. Long rows end in an ellipsis rather than wrap: a wrapped row would no
/// longer be one of the TUI's.
private struct PeekRow: View {
    let text: String
    let selected: Bool
    let agent: String

    var body: some View {
        Text(verbatim: text)
            .font(.mono(.footnote))
            .foregroundStyle(selected ? Color.togoSnow : Color.togoMute)
            .lineLimit(1)
            .truncationMode(.tail)
            .padding(.horizontal, 14)
            .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
            .background(selected ? Color.togoGrouped : Color.clear)
            .contentShape(.rect)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(text.trimmingCharacters(in: .whitespaces))
            .accessibilityHint(ComposerCopy.peekRowHint(agent))
            .accessibilityAddTraits(selected ? [.isButton, .isSelected] : .isButton)
    }
}
