import SwiftUI
import TogoKit

/// A session's mission at a glance, as the preview over a roster line's long-press menu:
/// "2 agents  1 shell  plan 3/7" in mono, a small plan ring or a working mark at its leading
/// edge, and the beacon while something needs the user. Still: it is a snapshot, not a control.
struct MissionPill: View {
    @ScaledMetric(relativeTo: .footnote) private var mark: CGFloat = 12
    @ScaledMetric(relativeTo: .footnote) private var slotSide: CGFloat = 16
    let summary: MissionSummary

    init(summary: MissionSummary) {
        self.summary = summary
    }

    /// The stress fixtures' call shape from when the pill was a button; the preview has no action.
    init(summary: MissionSummary, _ unused: () -> Void) {
        self.init(summary: summary)
    }

    private var needsYou: Bool { summary.needsYou > 0 }
    private var text: String { MissionText.pill(summary) }

    var body: some View {
        HStack(spacing: 8) {
            leading
                .frame(width: slotSide, height: slotSide)
            // One line when it fits; at large sizes each count takes a line of its own, so
            // none is lost and none breaks inside its number.
            ViewThatFits(in: .horizontal) {
                countText(text)
                VStack(spacing: 2) {
                    ForEach(lines, id: \.self) { countText($0) }
                }
                VStack(spacing: 2) {
                    ForEach(lines, id: \.self) { countText($0, wraps: true) }
                }
            }
        }
        .padding(.horizontal, 14)
        .frame(minHeight: 34)
        .background { MissionPillSurface(lit: needsYou) }
        .padding(8)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(MissionCopy.title)
        .accessibilityValue(MissionText.spoken(summary))
        .accessibilityIdentifier("mission.preview")
    }

    private var lines: [String] {
        let parts = MissionText.parts(summary)
        return parts.isEmpty ? [text] : parts
    }

    /// One count; `wraps` lets it break between its words ("plan" over "128/345") when even a
    /// line of its own is too narrow.
    private func countText(_ words: String, wraps: Bool = false) -> some View {
        Text(verbatim: words)
            .font(.mono(.footnote, weight: needsYou ? .semibold : .medium))
            .monospacedDigit()
            .foregroundStyle(needsYou ? Color.togoSnow : Color.togoMute)
            .multilineTextAlignment(.center)
            .fixedSize(horizontal: !wraps, vertical: true)
    }

    @ViewBuilder private var leading: some View {
        if needsYou {
            Circle()
                .fill(Color.togoAccent)
                .frame(width: mark * 0.66, height: mark * 0.66)
                .accessibilityHidden(true)
        } else if let fraction = MissionText.planFraction(summary) {
            MissionPlanRing(fraction: fraction, side: mark)
        } else {
            Circle()
                .fill(summary.working ? Color.togoSnow : Color.togoMuteDim)
                .frame(width: mark * 0.5, height: mark * 0.5)
                .frame(width: mark, height: mark)
                .accessibilityHidden(true)
        }
    }
}

/// The pill's ground: the opaque raised surface with the one hairline, lit from inside with the
/// beacon's own blue while something needs the user.
private struct MissionPillSurface: View {
    let lit: Bool

    var body: some View {
        RoundedRectangle(cornerRadius: 17, style: .continuous)
            .fill(Color.togoRaised)
            .overlay {
                if lit { RoundedRectangle(cornerRadius: 17, style: .continuous).fill(Color.togoAccent.opacity(0.16)) }
            }
            .overlay {
                RoundedRectangle(cornerRadius: 17, style: .continuous).strokeBorder(lit ? Color.togoAccent.opacity(0.55) : Color.togoLine, lineWidth: 1)
            }
    }
}

/// How much of the plan is done, as a small ring in the text's colour.
struct MissionPlanRing: View {
    let fraction: Double
    let side: CGFloat
    var lineWidth: CGFloat = 2

    var body: some View {
        ZStack {
            Circle().stroke(Color.togoLine, lineWidth: lineWidth)
            Circle()
                .trim(from: 0, to: max(0.02, fraction))
                .stroke(Color.togoSnow, style: StrokeStyle(lineWidth: lineWidth, lineCap: .round))
                .rotationEffect(.degrees(-90))
                // motion: continuity — the ring fills from the old share of the plan to the new.
                .motion(.progress, value: fraction)
        }
        .frame(width: side, height: side)
        .accessibilityHidden(true)
    }
}

#if DEBUG
#Preview("Mission preview") {
    VStack(spacing: 16) {
        MissionPill(summary: MissionSummary(agents: 2, background: 1, plan: .init(done: 3, total: 7), working: true))
        MissionPill(summary: MissionSummary(needsYou: 1, agents: 1))
        MissionPill(summary: MissionSummary(working: true))
        MissionPill(summary: MissionSummary())
    }
    .padding()
    .background(Color.togoInk)
}
#endif
