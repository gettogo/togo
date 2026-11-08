import SwiftUI
import TogoPush

/// The status word. "Needs you" is the one state that takes the accent.
struct LiveStatusText: View {
    let status: LiveStatus
    var font: Font = .title3
    var reason: String?

    var body: some View {
        Text(status.word(reason: reason))
            .font(font)
            .fontWeight(status == .needsYou ? .semibold : .medium)
            .foregroundStyle(Self.color(status))
            // trunc-ok: a status word from a fixed short set
            .lineLimit(1)
            // Between updates the system morphs one word into the next.
            .contentTransition(.interpolate)
    }

    private static func color(_ status: LiveStatus) -> Color {
        switch status {
        case .needsYou: Color.togoAccentText
        case .expired, .notUpdated, .offline: Color.togoMute
        default: Color.togoSnow
        }
    }
}

/// The Live Activity on the lock screen and in the notification list. At a glance: which
/// session (the agent's mark and the session's title), the state, how long, how many wait,
/// and what the focused request is about, as a question and its command, file or URL. While
/// the agent works it shows the step it is on. The whole card opens the session.
struct LiveLockScreenView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let state: TogoActivityAttributes.ContentState
    let card: LiveCard
    var isStale = false

    private var status: LiveStatus { LiveStatus(state.status, stale: isStale) }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline, spacing: 12) {
                LiveTitle(card: card)
                Spacer(minLength: 0)
                if status.showsElapsed {
                    LiveClock(since: state.since)
                        .font(.subheadline)
                        .foregroundStyle(Color.togoMute)
                        // motion: state change — the system crossfades between entries in place; nothing moves.
                        .transition(.opacity)
                }
            }
            statusLine
            if card.phase == .needsYou {
                if let ask = card.ask {
                    LiveAskText(ask: ask, alone: card.detail == nil)
                }
                if let detail = card.detail {
                    LiveDetailBlock(detail: detail)
                        // motion: state change — the system crossfades between entries in place; nothing moves.
                        .transition(.opacity)
                }
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .dynamicTypeSize(...LiveCopy.largestType)
    }

    /// "Needs you · Studio      +2 more", or "Working  Bash".
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
                    // motion: state change — the system crossfades between entries in place; nothing moves.
                    .transition(.opacity)
            }
            if let machine = card.machine {
                Text(verbatim: "· \(machine)")
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                    // trunc-ok: the machine's name; it shows in full in the app
                    .lineLimit(1)
            }
            Spacer(minLength: 8)
            if let more = card.moreText, status.showsCount {
                Text(verbatim: more)
                    .font(.footnote)
                    .fontWeight(.medium)
                    // accent-ok: "+3 more" counts further requests waiting for the user.
                    .foregroundStyle(Color.togoAccentText)
                    // trunc-ok: "+3 more"
                    .lineLimit(1)
                    .contentTransition(reduceMotion ? .identity : .numericText(value: Double(card.more)))
                    // motion: state change — the system crossfades between entries in place; nothing moves.
                    .transition(.opacity)
            }
        }
    }
}

/// The Live Activity in the small family: the Apple Watch Smart Stack and CarPlay. Without it
/// the system builds that view from the island's expanded regions, which strands the mark in
/// the corner. One header row (the mark beside the agent's name, the state, the clock), then
/// the request or the session in up to three lines. No answer buttons: they do not work there,
/// and a tap opens the session.
struct LiveSmallView: View {
    let state: TogoActivityAttributes.ContentState
    let card: LiveCard
    var isStale = false

    private var status: LiveStatus { LiveStatus(state.status, stale: isStale) }

    /// The agent's name, else the title (which falls back to "Session").
    private var name: String { card.agentName.isEmpty ? card.title : card.agentName }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            header
            content
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .padding(10)
        .dynamicTypeSize(...LiveCopy.largestType)
    }

    /// "Ad Aider  Needs you      0:13".
    private var header: some View {
        HStack(alignment: .firstTextBaseline, spacing: 5) {
            HStack(alignment: .firstTextBaseline, spacing: 4) {
                LiveMark(agent: card.agentID)
                    .font(.caption)
                Text(verbatim: name)
                    .font(.caption)
                    .fontWeight(.semibold)
                    // trunc-ok: the small family is a fixed width; the state and clock come first and a tap opens the session
                    .lineLimit(1)
            }
            .foregroundStyle(Color.togoSnow)
            .accessibilityElement(children: .combine)
            .accessibilityLabel(name)
            LiveStatusText(status: status, font: .caption, reason: state.reason)
                .layoutPriority(2)
            Spacer(minLength: 4)
            if status.showsElapsed {
                LiveClock(since: state.since)
                    .font(.caption)
                    .foregroundStyle(Color.togoMute)
                    // motion: state change — the system crossfades between entries in place; nothing moves.
                    .transition(.opacity)
            }
        }
    }

    /// The request while the agent waits; else the session and, while it works, its step.
    @ViewBuilder private var content: some View {
        if card.phase == .needsYou, card.ask != nil || card.detail != nil {
            if let ask = card.ask {
                LiveAskText(ask: ask, alone: card.detail == nil)
            }
            if let detail = card.detail {
                LiveDetailBlock(detail: detail, lines: card.ask == nil ? 3 : 2, cornerRadius: 8)
                    // motion: state change — the system crossfades between entries in place; nothing moves.
                    .transition(.opacity)
            }
        } else {
            if card.title != name {
                Text(verbatim: card.title)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoSnow)
                    // trunc-ok: the small family is a fixed size; the helper caps a session title at 64 characters and a tap opens the session
                    .lineLimit(2)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .privacySensitive()
            }
            if let step = card.step {
                Text(verbatim: step)
                    .font(.system(.footnote, design: .monospaced))
                    .foregroundStyle(Color.togoMute)
                    // trunc-ok: a tool or step name, capped at 48 characters by the helper
                    .lineLimit(1)
                    .privacySensitive()
                    // motion: state change — the system crossfades between entries in place; nothing moves.
                    .transition(.opacity)
            }
        }
    }
}

#if DEBUG
#Preview("Small family, needs you") {
    let state = TogoActivityAttributes.ContentState(
        bib: "04", agent: "aider", status: "needs_you", waiting: 1,
        since: Int(Date().timeIntervalSince1970) - 13, requestId: "r_1", command: nil
    )
    let context = LiveContext(
        title: nil, step: nil, ask: "Login to OpenRouter or create a free account?", subject: nil, answer: nil
    )
    let card = LiveCard(
        agent: "aider", status: state.status, waiting: 1, requestId: "r_1", command: nil, context: context,
        machine: nil, stale: false
    )
    LiveSmallView(state: state, card: card)
        .frame(width: 184, height: 92)
        .background(Color.togoInk, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        .environment(\.colorScheme, .dark)
}

#Preview("Small family, working") {
    let state = TogoActivityAttributes.ContentState(
        bib: "04", agent: "claude", status: "working", waiting: 0,
        since: Int(Date().timeIntervalSince1970) - 1_830, requestId: nil, command: nil
    )
    let context = LiveContext(title: "Fix the login redirect", step: "Bash", ask: nil, subject: nil, answer: nil)
    let card = LiveCard(
        agent: "claude", status: state.status, waiting: 0, requestId: nil, command: nil, context: context,
        machine: nil, stale: false
    )
    LiveSmallView(state: state, card: card)
        .frame(width: 184, height: 92)
        .background(Color.togoInk, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        .environment(\.colorScheme, .dark)
}
#endif
