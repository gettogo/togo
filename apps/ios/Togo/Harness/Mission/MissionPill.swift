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
    /// A new request arrived: the pill dips once and settles back.
    @State private var nudged = false

    private var needsYou: Bool { summary.needsYou > 0 }
    private var text: String { MissionText.pill(summary) }

    var body: some View {
        Button(action: onOpen) {
            HStack(spacing: 8) {
                leading
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
                // Like the system's bar items the pill stops growing at AX2; VoiceOver and the
                // sheet it opens carry the counts at any size.
                .dynamicTypeSize(...DynamicTypeSize.accessibility2)
                Image(systemName: "chevron.down")
                    .font(.caption2.weight(.semibold))
                    .foregroundStyle(Color.togoMuteDim)
                    .accessibilityHidden(true)
            }
            .padding(.horizontal, 14)
            .frame(minHeight: 34)
            .background { MissionPillSurface(lit: needsYou) }
            .contentShape(.rect(cornerRadius: 17, style: .continuous))
        }
        .buttonStyle(MissionPillPress())
        .hoverEffect(.highlight)
        .scaleEffect(nudged ? 1.06 : 1)
        .motion(.dropIn, value: nudged)
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
        .onChange(of: summary.needsYou) { old, new in
            guard new > old else { return }
            NeedsAnnouncement.post(MissionCopy.needs(new), after: .seconds(1))
            guard !reduceMotion else { return }
            nudged = true
            Task {
                try? await Task.sleep(for: .milliseconds(220))
                nudged = false
            }
        }
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
            .contentTransition(.numericText())
    }

    @ViewBuilder private var leading: some View {
        if needsYou {
            BreathingBeacon()
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

/// The pill's ground: the opaque raised surface with the one hairline, so the chat never
/// shows through it, lit from inside with the beacon's own blue while something needs the user.
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

/// The beacon dot breathing slowly while something waits: its halo swells and fades over
/// about three seconds. Still under Reduce Motion, Low Power Mode, in paused snapshots and
/// while the app is not in front.
private struct BreathingBeacon: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.loopsPaused) private var loopsPaused
    @Environment(\.scenePhase) private var scenePhase
    @ScaledMetric(relativeTo: .headline) private var side: CGFloat = 8
    @State private var lowPower = ProcessInfo.processInfo.isLowPowerModeEnabled

    /// One breath, in seconds.
    static let breath: TimeInterval = 3.2

    var body: some View {
        Group {
            if breathes {
                TimelineView(.periodic(from: .now, by: 1.0 / 20)) { context in
                    dot(Self.level(at: context.date.timeIntervalSinceReferenceDate))
                }
            } else {
                dot(0.5)
            }
        }
        .accessibilityHidden(true)
        .onReceive(NotificationCenter.default.publisher(for: .NSProcessInfoPowerStateDidChange)) { _ in
            lowPower = ProcessInfo.processInfo.isLowPowerModeEnabled
        }
    }

    private var breathes: Bool {
        !reduceMotion && !loopsPaused && !lowPower && scenePhase == .active
    }

    /// 0 at the out-breath, 1 at the in-breath, eased like a sine.
    static func level(at time: TimeInterval) -> Double {
        0.5 - 0.5 * cos(time / breath * 2 * .pi)
    }

    /// The core stays put; only the faint halo around it breathes, inside the pill.
    private func dot(_ level: Double) -> some View {
        Circle()
            .fill(Color.togoAccent)
            .frame(width: side, height: side)
            .background {
                Circle()
                    .fill(Color.togoAccent.opacity(0.10 + 0.20 * level))
                    .padding(-(2 + 3 * level))
            }
            .frame(width: side + 8, height: side + 8)
    }
}

/// Says a new request needs the user, once: the chat's own line for a request names it and
/// goes first, so a second announcement within a few seconds is dropped.
@MainActor enum NeedsAnnouncement {
    private static var last: Date?
    static let window: TimeInterval = 3

    static func post(_ text: String, after delay: Duration = .zero) {
        guard UIAccessibility.isVoiceOverRunning else { return }
        Task { @MainActor in
            if delay > .zero { try? await Task.sleep(for: delay) }
            if let last, Date.now.timeIntervalSince(last) < window { return }
            last = .now
            var line = AttributedString(text)
            line.accessibilitySpeechAnnouncementPriority = .high
            AccessibilityNotification.Announcement(line).post()
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
