import SwiftUI
import TogoKit

/// The live activity pill at the top of the chat: "2 agents  1 shell  plan 3/7" in mono, a
/// small plan ring or a working mark at its leading edge, and the beacon while something needs
/// the user. Tap it, or pull it down, to open mission control.
struct MissionPill: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @ScaledMetric(relativeTo: .footnote) private var mark: CGFloat = 12
    let summary: MissionSummary
    let onOpen: () -> Void

    /// How far the pill follows a pull before it lets go and opens.
    @GestureState private var pull: CGFloat = 0

    private var needsYou: Bool { summary.needsYou > 0 }
    private var text: String { MissionText.pill(summary) }

    var body: some View {
        Button(action: onOpen) {
            HStack(spacing: 8) {
                leading
                Text(verbatim: text)
                    .font(.mono(.footnote, weight: needsYou ? .semibold : .medium))
                    .monospacedDigit()
                    .foregroundStyle(needsYou ? Color.togoSnow : Color.togoMute)
                    .lineLimit(1)
                    .contentTransition(.numericText())
                Image(systemName: "chevron.down")
                    .font(.caption2.weight(.semibold))
                    .foregroundStyle(Color.togoMuteDim)
                    .accessibilityHidden(true)
            }
            .padding(.horizontal, 14)
            .frame(minHeight: 34)
            .background { MissionPillSurface(lit: needsYou) }
            .contentShape(.capsule)
        }
        .buttonStyle(MissionPillPress())
        .hoverEffect(.highlight)
        .offset(y: pull)
        .simultaneousGesture(pullDown)
        .motion(.tabSettle, value: pull == 0)
        .motion(.fade, value: text)
        .motion(.fade, value: needsYou)
        .padding(.top, 6)
        .padding(.bottom, 4)
        .frame(maxWidth: .infinity)
        .accessibilityLabel(MissionCopy.title)
        .accessibilityValue(MissionText.spoken(summary))
        .accessibilityHint(MissionCopy.openHint)
        .accessibilityIdentifier("mission.pill")
    }

    @ViewBuilder private var leading: some View {
        if needsYou {
            BeaconDot()
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

    /// Pulling the pill down opens mission control, the way a sheet's grabber would.
    private var pullDown: some Gesture {
        DragGesture(minimumDistance: 6)
            .updating($pull) { value, state, _ in
                state = min(max(value.translation.height, 0), 28) * 0.5
            }
            .onEnded { value in
                if value.translation.height > 20 || value.predictedEndTranslation.height > 60 { onOpen() }
            }
    }
}

/// The pill's ground: the raised surface with the one hairline, lit with the beacon's own
/// blue while something needs the user.
private struct MissionPillSurface: View {
    let lit: Bool

    var body: some View {
        Capsule()
            .fill(lit ? Color.togoAccent.opacity(0.16) : Color.togoRaised)
            .overlay {
                Capsule().strokeBorder(lit ? Color.togoAccent.opacity(0.55) : Color.togoLine, lineWidth: 1)
            }
    }
}

/// A pressed pill dims a little, like a system bar button.
private struct MissionPillPress: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .opacity(configuration.isPressed ? 0.7 : 1)
            .scaleEffect(configuration.isPressed ? 0.98 : 1)
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
        }
        .frame(width: side, height: side)
        .accessibilityHidden(true)
    }
}

#if DEBUG
#Preview("Mission pill") {
    VStack(spacing: 16) {
        MissionPill(summary: MissionSummary(agents: 2, background: 1, plan: .init(done: 3, total: 7), working: true)) {}
        MissionPill(summary: MissionSummary(needsYou: 1, agents: 1)) {}
        MissionPill(summary: MissionSummary(working: true)) {}
        MissionPill(summary: MissionSummary()) {}
    }
    .padding()
    .background(Color.togoInk)
}
#endif
