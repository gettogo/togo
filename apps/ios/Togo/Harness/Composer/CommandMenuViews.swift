import SwiftUI
import TogoKit

/// The / or @ suggestions above the field: starred and recent commands first, each with what
/// it does and a trailing glyph for what a tap does. A choice command shows its values as
/// capsules under it, so a tap applies one. The keyboard's row is marked.
struct CommandMenuPanel: View {
    @Environment(\.displayScale) private var displayScale
    let model: CommandLineModel
    let screenTitle: (String) -> String

    var body: some View {
        let items = model.menu.items
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
            .frame(maxHeight: 280)
            .fixedSize(horizontal: false, vertical: true)
            .onChange(of: model.highlight) { _, row in proxy.scrollTo(row) }
        }
        .background(Color.togoRaised, in: .rect(cornerRadius: 20, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 20, style: .continuous)
                .strokeBorder(Color.togoLine, lineWidth: 1 / displayScale)
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(isFiles ? ComposerCopy.mention : ComposerCopy.commands)
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
