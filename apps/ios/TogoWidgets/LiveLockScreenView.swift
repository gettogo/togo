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
