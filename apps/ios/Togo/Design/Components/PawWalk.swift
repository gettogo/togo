import SwiftUI

/// The four paw prints from the mark, lit one after another while something is on its way.
/// Under Reduce Motion the paws stay still and the word carries the meaning.
struct PawWalk: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.loopsPaused) private var paused
    @ScaledMetric(relativeTo: .body) private var width: CGFloat = 26
    @State private var lit = 0
    var word: String?

    init(_ word: String? = nil) {
        self.word = word
    }

    /// Paw centres from `brand/togo-mark-tiny.svg`, relative to the trail's bounding box.
    private static let paws: [CGPoint] = [
        CGPoint(x: 27.5, y: 41.9), CGPoint(x: 33.5, y: 38.2), CGPoint(x: 39.5, y: 34.6), CGPoint(x: 45.5, y: 30.9),
    ]
    private static let radius: CGFloat = 2.3
    private static let origin = CGPoint(x: 27.5 - radius, y: 30.9 - radius)
    private static let box = CGSize(width: 18 + 2 * radius, height: 11 + 2 * radius)
    static let step = Duration.milliseconds(120)

    var body: some View {
        HStack(spacing: 10) {
            trail
            if let word {
                Text(word)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(word ?? String(localized: "Loading"))
        .task(id: reduceMotion || paused) {
            guard !reduceMotion, !paused else { return }
            while !Task.isCancelled {
                try? await Task.sleep(for: Self.step)
                withAnimation(Motion.animation(.pawStep, reduce: reduceMotion)) { lit = (lit + 1) % Self.paws.count }
            }
        }
    }

    private var trail: some View {
        let scale = width / Self.box.width
        return ZStack(alignment: .topLeading) {
            ForEach(Self.paws.indices, id: \.self) { index in
                Circle()
                    .fill(color(for: index))
                    .frame(width: 2 * Self.radius * scale, height: 2 * Self.radius * scale)
                    .position(
                        x: (Self.paws[index].x - Self.origin.x) * scale,
                        y: (Self.paws[index].y - Self.origin.y) * scale
                    )
            }
        }
        .frame(width: width, height: Self.box.height * scale)
    }

    private func color(for index: Int) -> Color {
        if reduceMotion { return .togoMute }
        return index == lit ? .togoSnow : .togoMuteDim
    }
}
