import SwiftUI
import TogoKit

/// The / or @ suggestions above the field: starred and recent commands first, each with what
/// it does and whether it opens a native screen. The keyboard's row is marked.
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
                        CommandRow(
                            item: item, file: isFiles, highlighted: index == model.highlight,
                            favourite: !isFiles && model.isFavourite(item),
                            opens: item.screen.map(screenTitle)
                        ) { model.choose(item) }
                        .id(index)
                        .contextMenu {
                            if !isFiles {
                                let starred = model.isFavourite(item)
                                Button(starred ? ComposerCopy.unfavourite : ComposerCopy.favourite, systemImage: starred ? "star.slash" : "star") {
                                    model.toggleFavourite(item)
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
}

/// One suggestion: the command or file in mono with its argument hint, what it does, and
/// "Opens Model" when it has a native screen.
struct CommandRow: View {
    let item: HarnessCompletion
    let file: Bool
    let highlighted: Bool
    let favourite: Bool
    let opens: String?
    let onTap: () -> Void

    var body: some View {
        Button(action: onTap) {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Image(systemName: file ? "doc.text" : (favourite ? "star.fill" : "chevron.forward"))
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
                            .lineLimit(2)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                if let opens {
                    PrimitiveChip(text: ComposerCopy.opens(opens), tone: .strong, symbol: "rectangle.portrait.and.arrow.forward")
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 9)
            .frame(minHeight: 44)
            .background(highlighted ? Color.togoGrouped : Color.clear)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([item.title, item.hint, item.detail].compactMap { $0 }.joined(separator: ", "))
        .accessibilityValue([favourite ? ComposerCopy.favourites : nil, opens.map(ComposerCopy.opens)].compactMap { $0 }.joined(separator: ", "))
        .accessibilityAddTraits(highlighted ? [.isButton, .isSelected] : .isButton)
    }
}
