import SwiftUI

/// The hairline between two panes on a window without a fold. Its grabber drags the line, and
/// on release the split snaps to a third, a half or two thirds, whichever keeps both panes wide
/// enough. The terminals resize once, on release, rather than on every step of the drag.
struct PaneDivider: View {
    let store: AppStore
    /// The width the two panes and the line share.
    let width: CGFloat
    let height: CGFloat
    /// Where the line sits now, from the leading edge.
    let position: CGFloat
    /// False on a device with a fold, where the panes meet at the fold and stay there.
    let handle: Bool

    @GestureState private var drag: CGFloat = 0

    var body: some View {
        Rectangle()
            .fill(drag == 0 ? Color.togoLine : Color.togoMute)
            .frame(width: PaneLayout.hairline, height: height)
            .offset(x: drag)
            .overlay {
                if handle {
                    grabber
                }
            }
    }

    private var grabber: some View {
        Capsule()
            .fill(drag == 0 ? Color.togoMute : Color.togoSnow)
            .frame(width: 5, height: 40)
            .frame(width: 44, height: 88)
            .contentShape(.rect)
            .offset(x: drag)
            .gesture(
                DragGesture(minimumDistance: 2)
                    .updating($drag) { value, state, _ in
                        state = clamped(value.translation.width)
                    }
                    .onEnded { value in
                        Self.settle(at: position + clamped(value.translation.width), width: width, store: store)
                    }
            )
            .accessibilityElement()
            .accessibilityLabel(PanesCopy.divider)
            .accessibilityValue(PanesCopy.dividerValue(Int((store.workspace.primaryFraction * 100).rounded())))
            .accessibilityHint(PanesCopy.dividerHint)
            .accessibilityAdjustableAction { direction in
                let fractions = PaneLayout.fractions
                guard let index = fractions.firstIndex(where: { abs($0 - store.workspace.primaryFraction) < 0.01 }) else { return }
                let next = direction == .increment ? min(index + 1, fractions.count - 1) : max(index - 1, 0)
                Self.settle(at: (width - PaneLayout.hairline) * fractions[next], width: width, store: store)
            }
    }

    /// Keeps the preview line inside the window while the finger wanders past an edge.
    private func clamped(_ translation: CGFloat) -> CGFloat {
        min(max(translation, -position), width - position)
    }

    /// The split for a line released `x` points from the leading edge.
    static func settle(at x: CGFloat, width: CGFloat, store: AppStore) {
        let available = width - PaneLayout.hairline
        guard available > 0 else { return }
        store.workspace.primaryFraction = PaneLayout.snap(x / available, width: width)
    }
}
