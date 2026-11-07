import SwiftUI

/// A strip of 16 thin bars showing how much output a session produced over the last eight
/// minutes, oldest on the left. It has no track behind it and says nothing to VoiceOver: the
/// status word beside it carries the meaning. Empty samples draw nothing.
struct ActivityBars: View {
    static let barCount = 16
    static let barWidth: CGFloat = 2
    static let gap: CGFloat = 2
    static let minHeight: CGFloat = 2
    static let maxHeight: CGFloat = 12
    /// How many of the newest bars use the brighter step.
    static let recentCount = 5

    let samples: [Double]
    let live: Bool

    init(samples: [Double], live: Bool) {
        self.samples = samples
        self.live = live
    }

    /// Bar height in points for a sample: at least 2, at most 12; NaN counts as 0.
    static func height(for value: Double) -> CGFloat {
        let clamped = value.isNaN ? 0 : min(max(value, 0), 1)
        return max(minHeight, CGFloat(clamped) * maxHeight)
    }

    /// The newest 16 samples, oldest first.
    static func visible(_ samples: [Double]) -> [Double] {
        Array(samples.suffix(barCount))
    }

    private enum Shade { case older, recent, newest }

    private static func shade(at index: Int, of count: Int, live: Bool) -> Shade {
        if index == count - 1 { return live ? .newest : .recent }
        return index >= count - recentCount ? .recent : .older
    }

    private func color(_ shade: Shade) -> Color {
        switch shade {
        case .older: .togoMuteDim
        case .recent: .togoMute
        case .newest: .togoSnow
        }
    }

    var body: some View {
        let values = Self.visible(samples)
        if !values.isEmpty {
            HStack(alignment: .bottom, spacing: Self.gap) {
                ForEach(Array(values.enumerated()), id: \.offset) { index, value in
                    RoundedRectangle(cornerRadius: 1)
                        .fill(color(Self.shade(at: index, of: values.count, live: live)))
                        .frame(width: Self.barWidth, height: Self.height(for: value))
                }
            }
            .frame(height: Self.maxHeight, alignment: .bottom)
            .accessibilityHidden(true)
        }
    }
}
