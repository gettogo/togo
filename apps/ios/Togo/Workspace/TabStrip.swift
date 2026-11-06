import SwiftUI

/// The open sessions as chips under the header, in the order ⌘1 to ⌘9 pick them. Chips of tabs
/// on screen are drawn on a surface, the focused one in snow and the one beside it in mute; the
/// rest are plain text. A chip drags into a pane or another window.
struct TabStrip: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.supportsMultipleWindows) private var supportsMultipleWindows

    struct Chip: Identifiable, Equatable {
        var id: TabID
        var bib: String?
        var name: String
    }

    let chips: [Chip]
    /// The focused pane's tab.
    let selected: TabID?
    /// The other pane's tab while two panes are on screen.
    let beside: TabID?
    /// The window can show a second pane, so chips offer Open Beside.
    let canOpenBeside: Bool
    /// Incoming request cards shrink into the chip of their session.
    let namespace: Namespace.ID
    let onSelect: (TabID) -> Void
    let onOpenBeside: (TabID) -> Void
    let onClose: (TabID) -> Void

    init(
        chips: [Chip], selected: TabID?, beside: TabID? = nil, canOpenBeside: Bool = false, namespace: Namespace.ID,
        onSelect: @escaping (TabID) -> Void, onOpenBeside: @escaping (TabID) -> Void = { _ in },
        onClose: @escaping (TabID) -> Void
    ) {
        self.chips = chips
        self.selected = selected
        self.beside = beside
        self.canOpenBeside = canOpenBeside
        self.namespace = namespace
        self.onSelect = onSelect
        self.onOpenBeside = onOpenBeside
        self.onClose = onClose
    }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView(.horizontal) {
                HStack(spacing: 2) {
                    ForEach(chips) { chip in
                        chipButton(chip)
                            .id(chip.id)
                    }
                }
                .padding(.horizontal, 12)
            }
            .scrollIndicators(.hidden)
            .onChange(of: selected) { _, id in
                guard let id else { return }
                withAnimation(Motion.animation(.tabSettle, reduce: reduceMotion)) {
                    proxy.scrollTo(id, anchor: .center)
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(WorkspaceCopy.tabs)
    }

    private func chipButton(_ chip: Chip) -> some View {
        let isSelected = chip.id == selected
        let isBeside = chip.id == beside
        return Button {
            onSelect(chip.id)
        } label: {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                if let bib = chip.bib {
                    Text(verbatim: bib)
                        .font(.system(.subheadline, design: .monospaced))
                        .monospacedDigit()
                }
                Text(verbatim: chip.name)
                    .font(.subheadline.weight(isSelected ? .semibold : .regular))
            }
            .lineLimit(1)
            .foregroundStyle(isSelected ? Color.togoSnow : Color.togoMute)
            .padding(.horizontal, 12)
            .padding(.vertical, 7)
            .background {
                Capsule()
                    .fill(isSelected || isBeside ? Color.togoRaised : Color.clear)
            }
            .matchedGeometryEffect(id: chip.id, in: namespace, isSource: true)
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .motion(.tabSettle, value: isSelected)
        .accessibilityAddTraits(isSelected || isBeside ? .isSelected : [])
        .modifier(ChipDrag(id: chip.id, enabled: canOpenBeside || supportsMultipleWindows))
        .contextMenu {
            if canOpenBeside && !isBeside {
                Button(PanesCopy.openBeside, systemImage: "rectangle.split.2x1") { onOpenBeside(chip.id) }
            }
            OpenInWindowButton(tab: chip.id)
            Button(WorkspaceCopy.closeTab, systemImage: "xmark") { onClose(chip.id) }
        }
    }
}

/// Lifts a chip only where something can take it: a second pane or another window. Elsewhere
/// the long press belongs to the chip's menu.
private struct ChipDrag: ViewModifier {
    let id: TabID
    let enabled: Bool

    func body(content: Content) -> some View {
        if enabled {
            content.draggable(id)
        } else {
            content
        }
    }
}
