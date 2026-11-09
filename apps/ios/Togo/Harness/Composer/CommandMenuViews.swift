import SwiftUI
import TogoKit

/// The / or @ suggestions above the field: starred and recent commands first, each with what
/// it does and a trailing glyph for what a tap does. A choice command shows its values as
/// capsules under it, so a tap applies one. The keyboard's row is marked.
struct CommandMenuPanel: View {
    @Environment(\.displayScale) private var displayScale
    @Environment(\.colorSchemeContrast) private var contrast
    let model: CommandLineModel
    let screenTitle: (String) -> String
    @State private var contentHeight: CGFloat = CommandMenuPanel.cap

    /// The most the list takes when the window has room.
    static let cap: CGFloat = 280

    var body: some View {
        let items = model.menu.items
        VStack(spacing: 0) {
            header
            list(items)
        }
        .background(Color.togoRaised, in: .rect(cornerRadius: 20, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 20, style: .continuous)
                .strokeBorder(Color.togoLine, lineWidth: Contrast.hairline(displayScale, contrast))
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(isFiles ? ComposerCopy.mention : ComposerCopy.commands)
        .accessibilityIdentifier(isFiles ? "composer.menu.files" : "composer.menu.commands")
    }

    /// What the menu lists, and its close control: always a visible way back to the field.
    private var header: some View {
        HStack(spacing: 8) {
            Text(isFiles ? ComposerCopy.mention : ComposerCopy.commands)
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Color.togoMute)
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityAddTraits(.isHeader)
            Button(ComposerCopy.closeMenu, systemImage: "xmark") { model.dismissMenu() }
                .labelStyle(.iconOnly)
                .font(.footnote.weight(.bold))
                .foregroundStyle(Color.togoMute)
                // A 44 pt target; the header keeps its 36 pt row.
                .frame(width: 44, height: 44)
                .contentShape(.rect)
                .padding(.vertical, -4)
                .buttonStyle(.plain)
                .hoverEffect(.highlight)
                .keyboardShortcut(.cancelAction)
                .accessibilityIdentifier("composer.menu.close")
        }
        .padding(.leading, 14)
        .padding(.top, 2)
    }

    /// The rows at their own height up to a cap, and less when the window has less room
    /// (the field keeps its place below); past what fits they scroll.
    private func list(_ items: [HarnessCompletion]) -> some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(Array(items.enumerated()), id: \.offset) { index, item in
                        VStack(alignment: .leading, spacing: 0) {
                            Button { model.choose(item) } label: {
                                CommandRow(
                                    item: item, file: isFiles, highlighted: index == model.highlight,
                                    favourite: !isFiles && model.isFavourite(item),
                                    value: choiceValue(item)
                                )
                            }
                            .buttonStyle(.plain)
                            .hoverEffect(.highlight)
                            if !isFiles, case let .choose(choices, _) = CommandKind.dispatch(item) {
                                ChoiceCapsules(choices: choices, current: CommandKind.current(choices)?.id) { choice in
                                    model.closeMenu()
                                    model.draft = ""
                                    Task { await model.pick(choice, of: item, setting: setting(item)) }
                                }
                            }
                        }
                        .id(index)
                        .contextMenu {
                            if !isFiles {
                                let starred = model.isFavourite(item)
                                Button(starred ? ComposerCopy.unfavourite : ComposerCopy.favourite, systemImage: starred ? "star.slash" : "star") {
                                    model.toggleFavourite(item)
                                }
                                if case .run(let run) = CommandKind.dispatch(item), run.instructions != nil, !run.destructive {
                                    Button(ComposerCopy.addInstructions, systemImage: "text.cursor") { model.addInstructions(item) }
                                }
                            }
                        }
                    }
                }
                .padding(.vertical, 6)
            }
            .scrollBounceBehavior(.basedOnSize)
            // A swipe down on the list puts the keyboard away, and with it the menu.
            .scrollDismissesKeyboard(.interactively)
            .onScrollGeometryChange(for: CGFloat.self) { $0.contentSize.height } action: { _, height in
                contentHeight = height
            }
            // Ideal: the rows, up to the cap. Minimum zero, so a short window squeezes the
            // list rather than pushing the field off screen.
            .frame(minHeight: 0, idealHeight: min(contentHeight, Self.cap), maxHeight: min(contentHeight, Self.cap))
            .onChange(of: model.highlight) { _, row in proxy.scrollTo(row) }
        }
    }

    private var isFiles: Bool {
        if case .files = model.menu { true } else { false }
    }

    private func choiceValue(_ item: HarnessCompletion) -> String? {
        guard !isFiles, case let .choose(choices, _) = CommandKind.dispatch(item) else { return nil }
        return CommandKind.current(choices)?.label
    }

    private func setting(_ item: HarnessCompletion) -> String {
        item.screen.map(screenTitle) ?? String(CommandKind.line(item).drop { $0 == "/" })
    }
}

/// A choice command's values under its row in the `/` menu: a tap applies one.
private struct ChoiceCapsules: View {
    let choices: [HarnessCompletion.Choice]
    let current: String?
    let pick: (HarnessCompletion.Choice) -> Void

    var body: some View {
        ScrollView(.horizontal) {
            HStack(spacing: 6) {
                ForEach(choices) { choice in
                    Button { if choice.id != current { pick(choice) } } label: {
                        Text(verbatim: choice.label)
                    }
                    .buttonStyle(PrimitiveToggleChipStyle(on: choice.id == current))
                    .accessibilityAddTraits(choice.id == current ? .isSelected : [])
                }
            }
            .padding(.leading, 42)
            .padding(.trailing, 14)
            .padding(.bottom, 8)
        }
        .scrollIndicators(.hidden)
    }
}

/// One suggestion: the command or file in mono with its argument hint, what it does, and a
/// trailing glyph for its kind: a menu (with the current value), a list, the cursor, the
/// terminal. A command that simply runs has none.
struct CommandRow: View {
    let item: HarnessCompletion
    let file: Bool
    let highlighted: Bool
    let favourite: Bool
    /// A choice's current value, in mono before its glyph.
    var value: String?

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Image(systemName: file ? "doc.text" : (favourite ? "star.fill" : "slash.circle"))
                .font(.footnote.weight(.semibold))
                .foregroundStyle(Color.togoMute)
                .frame(width: 18)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text(verbatim: item.title)
                        .font(.mono(.subheadline, weight: .semibold))
                        .foregroundStyle(Color.togoSnow)
                    if let hint = item.hint, !hint.isEmpty {
                        Text(verbatim: hint).font(.mono(.caption)).foregroundStyle(Color.togoMute)
                    }
                }
                if let detail = item.detail, !detail.isEmpty {
                    Text(verbatim: detail)
                        .font(file ? .mono(.caption) : .footnote)
                        .foregroundStyle(Color.togoMute)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if let value {
                Text(verbatim: value)
                    .font(.mono(.subheadline))
                    .foregroundStyle(Color.togoMute)
            }
            if !file, let glyph = CommandKind.glyph(item) {
                Image(systemName: glyph)
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color.togoMute)
                    .accessibilityHidden(true)
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 9)
        .frame(minHeight: 44)
        .background(highlighted ? Color.togoGrouped : Color.clear)
        .contentShape(.rect)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([item.title, item.hint, item.detail].compactMap { $0 }.joined(separator: ", "))
        .accessibilityValue([favourite ? ComposerCopy.favourites : nil, value].compactMap { $0 }.joined(separator: ", "))
        .accessibilityAddTraits(highlighted ? [.isButton, .isSelected] : .isButton)
    }
}
