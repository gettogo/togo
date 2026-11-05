import SwiftUI

/// The open sessions as chips under the header, in the order ⌘1 to ⌘9 pick them. The chip on
/// screen is the only one drawn on a surface; the rest are plain text.
struct TabStrip: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    struct Chip: Identifiable, Equatable {
        var id: TabID
        var bib: String?
        var name: String
    }

    let chips: [Chip]
    let selected: TabID?
    /// Incoming request cards shrink into the chip of their session.
    let namespace: Namespace.ID
    let onSelect: (TabID) -> Void
    let onClose: (TabID) -> Void

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
                withAnimation(Motion.motion(Motion.tabSettle, reduce: reduceMotion)) {
                    proxy.scrollTo(id, anchor: .center)
                }
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(WorkspaceCopy.tabs)
    }

    private func chipButton(_ chip: Chip) -> some View {
        let isSelected = chip.id == selected
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
                    .fill(isSelected ? Color.togoRaised : Color.clear)
            }
            .matchedGeometryEffect(id: chip.id, in: namespace, isSource: true)
            .frame(minHeight: 44)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .animation(Motion.motion(Motion.tabSettle, reduce: reduceMotion), value: isSelected)
        .accessibilityAddTraits(isSelected ? .isSelected : [])
        .contextMenu {
            Button(WorkspaceCopy.closeTab, systemImage: "xmark") { onClose(chip.id) }
        }
    }
}
