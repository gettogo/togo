import AppIntents
import SwiftUI
import TogoPush

// The Dynamic Island is always black, so every region resolves the palette in dark.

/// The beacon: the one mark that says a session needs the user.
private struct IslandBeacon: View {
    var size: CGFloat = 8

    var body: some View {
        Circle()
            .fill(Color.togoAccent)
            .frame(width: size, height: size)
            .accessibilityHidden(true)
    }
}

/// Compact leading: the agent's mark, hugging the camera.
struct IslandCompactLeading: View {
    let card: LiveCard

    var body: some View {
        LiveMark(agent: card.agentID)
            .font(.subheadline)
            .foregroundStyle(Color.togoSnow)
            .environment(\.colorScheme, .dark)
            .dynamicTypeSize(...LiveCopy.largestType)
    }
}

/// Compact trailing: the state in the fewest marks. The beacon (with the count when more than
/// one waits) while something needs the user, the working clock at a fixed width, else a
/// symbol for how it ended.
struct IslandCompactTrailing: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let state: TogoActivityAttributes.ContentState
    let card: LiveCard
    var isStale = false

    private var status: LiveStatus { LiveStatus(state.status, stale: isStale) }

    var body: some View {
        Group {
            switch status {
            case .needsYou:
                HStack(spacing: 4) {
                    IslandBeacon()
                    if state.waiting > 1 {
                        Text(verbatim: "\(state.waiting)")
                            .font(.subheadline)
                            .fontWeight(.semibold)
                            .monospacedDigit()
                            .foregroundStyle(Color.togoAccent)
                            .contentTransition(reduceMotion ? .identity : .numericText(value: Double(state.waiting)))
                    }
                }
            case .working, .other:
                LiveClock(since: state.since)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
            default:
                Image(systemName: Self.symbol(status))
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
            }
        }
        // motion: state change — the system crossfades between entries in place; nothing moves.
        .transition(.opacity)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(state.waiting > 1 && status == .needsYou ? LiveCopy.waiting(state.waiting) : status.word(reason: state.reason))
        .environment(\.colorScheme, .dark)
        .dynamicTypeSize(...LiveCopy.largestType)
    }

    static func symbol(_ status: LiveStatus) -> String {
        switch status {
        case .done: "checkmark"
        case .ended: "stop.fill"
        case .failed: "exclamationmark.triangle"
        case .offline: "wifi.slash"
        default: "clock"
        }
    }
}

/// Minimal, shared with another app's activity: the beacon while something needs the user,
/// otherwise the agent's mark.
struct IslandMinimal: View {
    let state: TogoActivityAttributes.ContentState
    let card: LiveCard
    var isStale = false

    var body: some View {
        Group {
            if LiveStatus(state.status, stale: isStale) == .needsYou {
                IslandBeacon(size: 10)
                    .accessibilityHidden(false)
                    .accessibilityLabel(LiveCopy.waiting(max(1, state.waiting)))
            } else {
                LiveMark(agent: card.agentID)
                    .font(.caption)
                    .foregroundStyle(Color.togoSnow)
            }
        }
        // motion: state change — the system crossfades between entries in place; nothing moves.
        .transition(.opacity)
        .environment(\.colorScheme, .dark)
        .dynamicTypeSize(...LiveCopy.largestType)
    }
}

/// Expanded leading: the header. The agent's mark sits centred beside the session's title and
/// its state, "Needs you · Studio  +2 more", so the three read as one line of identity rather
/// than a mark stranded in the corner above them. It hugs the camera's leading edge and drops
/// below the camera when the title is too wide to fit beside it.
struct IslandExpandedLeading: View {
    let state: TogoActivityAttributes.ContentState
    let card: LiveCard
    var isStale = false

    private var status: LiveStatus { LiveStatus(state.status, stale: isStale) }

    var body: some View {
        HStack(alignment: .center, spacing: 10) {
            LiveMark(agent: card.agentID)
                .font(.title3)
                .foregroundStyle(Color.togoSnow)
            VStack(alignment: .leading, spacing: 1) {
                Text(verbatim: card.title)
                    .font(.headline)
                    .foregroundStyle(Color.togoSnow)
                    // trunc-ok: the Dynamic Island has a fixed width; the helper caps a session title at 64 characters and a tap opens the session
                    .lineLimit(1)
                    .privacySensitive()
                statusLine
            }
        }
        .padding(.leading, 4)
        .frame(maxWidth: .infinity, alignment: .leading)
        .environment(\.colorScheme, .dark)
        .dynamicTypeSize(...LiveCopy.largestType)
    }

    private var statusLine: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            LiveStatusText(status: status, font: .subheadline, reason: state.reason)
                .layoutPriority(2)
            if let step = card.step {
                Text(verbatim: step)
                    .font(.system(.footnote, design: .monospaced))
                    .foregroundStyle(Color.togoMute)
                    // trunc-ok: a tool or step name, capped at 48 characters by the helper
                    .lineLimit(1)
                    .privacySensitive()
            }
            if let machine = card.machine {
                Text(verbatim: "· \(machine)")
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                    // trunc-ok: the machine's name; it shows in full in the app
                    .lineLimit(1)
            }
            if let more = card.moreText, status.showsCount {
                Text(verbatim: more)
                    .font(.footnote)
                    .fontWeight(.medium)
                    // accent-ok: "+3 more" counts further requests waiting for the user.
                    .foregroundStyle(Color.togoAccentText)
                    // trunc-ok: "+3 more"
                    .lineLimit(1)
                    .layoutPriority(1)
            }
        }
    }
}

/// Expanded trailing: the clock at a fixed width, on the header's row against the camera's
/// trailing edge.
struct IslandExpandedTrailing: View {
    let state: TogoActivityAttributes.ContentState
    var isStale = false

    var body: some View {
        Group {
            if LiveStatus(state.status, stale: isStale).showsElapsed {
                LiveClock(since: state.since)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
                    // motion: state change — the system crossfades between entries in place; nothing moves.
                    .transition(.opacity)
            }
        }
        .padding(.trailing, 4)
        .frame(maxHeight: .infinity, alignment: .center)
        .environment(\.colorScheme, .dark)
        .dynamicTypeSize(...LiveCopy.largestType)
    }
}

/// Expanded bottom: the request, then Deny and Approve where a one-tap answer is allowed (the
/// relay mailbox path), else Open. The expanded island is at most 160 pt tall, so with a detail
/// the question gives way to it: the header already says "Needs you", and a question beside
/// a command would push the buttons out of the island.
struct IslandExpandedBottom: View {
    let state: TogoActivityAttributes.ContentState
    let card: LiveCard
    let hostId: String
    let link: URL?
    var isStale = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            if card.phase == .needsYou {
                if let ask = card.ask, card.detail == nil {
                    LiveAskText(ask: ask, alone: true)
                }
                if let detail = card.detail {
                    LiveDetailBlock(detail: detail, cornerRadius: 14, verticalPadding: 4)
                }
                buttons
            }
        }
        .environment(\.colorScheme, .dark)
        .dynamicTypeSize(...LiveCopy.largestType)
    }

    private var buttons: some View {
        LiveAnswerButtons(state: state, card: card, hostId: hostId, link: link)
            // The island's corners round in under the outer button ends.
            .padding(.horizontal, 8)
    }
}

/// Deny and Approve where a one-tap answer is allowed (the relay mailbox path), else Open when
/// there is a link. Shared by the expanded island and the lock screen card, which replaces the
/// notification while the activity runs.
struct LiveAnswerButtons: View {
    let state: TogoActivityAttributes.ContentState
    let card: LiveCard
    let hostId: String
    let link: URL?

    var body: some View {
        HStack(spacing: 8) {
            if let requestId = state.requestId, let answer = card.answer {
                Button(intent: DenyRequestIntent(hostId: hostId, requestId: requestId)) {
                    Text(LiveCopy.deny).frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered)
                .tint(Color.togoSnow)
                if answer == .approveDeny {
                    approve(requestId)
                }
            } else if let link {
                Link(destination: link) {
                    Text(LiveCopy.open).frame(maxWidth: .infinity)
                }
                .buttonStyle(.bordered)
                .tint(Color.togoSnow)
            }
        }
        .font(.subheadline.weight(.semibold))
        .controlSize(.small)
        // trunc-ok: one-word button labels
        .lineLimit(1)
        .environment(\.colorScheme, .dark)
    }

    /// Approve needs the device unlocked unless the user turned that off, as on notifications.
    @ViewBuilder private func approve(_ requestId: String) -> some View {
        let label = Text(LiveCopy.approve).frame(maxWidth: .infinity)
        Group {
            if SharedSettings.defaults?.bool(forKey: SharedSettings.approveWithoutUnlock) == true {
                Button(intent: ApproveUnlockedRequestIntent(hostId: hostId, requestId: requestId)) { label }
            } else {
                Button(intent: ApproveRequestIntent(hostId: hostId, requestId: requestId)) { label }
            }
        }
        .buttonStyle(.borderedProminent)
        .tint(Color.togoAccent)
        .foregroundStyle(Color.togoInk)
    }
}
