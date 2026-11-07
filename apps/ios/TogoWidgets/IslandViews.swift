import SwiftUI
import TogoPush

// The Dynamic Island is always black, so every region resolves the palette in dark.

/// The bib on the island: brighter than in a list, since it stands alone on black.
private struct IslandBib: View {
    let bib: String
    var style: Font.TextStyle = .subheadline

    var body: some View {
        Text(verbatim: bib)
            .font(.system(style, design: .monospaced))
            .fontWeight(.medium)
            .monospacedDigit()
            .foregroundStyle(Color.togoSnow)
            // trunc-ok: a bib is two or three digits
            .lineLimit(1)
            .fixedSize()
    }
}

/// Shown where a bib would be when the session has none.
private struct IslandMark: View {
    var body: some View {
        Image(systemName: "apple.terminal")
            .font(.subheadline)
            .foregroundStyle(Color.togoMute)
            .accessibilityHidden(true)
    }
}

struct IslandCompactLeading: View {
    let state: TogoActivityAttributes.ContentState
    var isStale = false

    var body: some View {
        Group {
            if let bib = state.bib { IslandBib(bib: bib) } else { IslandMark() }
        }
        .environment(\.colorScheme, .dark)
        .dynamicTypeSize(...LiveCopy.largestType)
    }
}

/// One word for the state, or how many requests wait when there is more than one.
struct IslandCompactTrailing: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let state: TogoActivityAttributes.ContentState
    var isStale = false

    var body: some View {
        Group {
            if state.waiting > 1 && LiveStatus(state.status, stale: isStale).showsCompactCount {
                Text(LiveCopy.waiting(state.waiting))
                    .font(.subheadline)
                    .fontWeight(.semibold)
                    .foregroundStyle(Color.togoAccent)
                    .contentTransition(reduceMotion ? .identity : .numericText(value: Double(state.waiting)))
            } else {
                LiveStatusText(status: LiveStatus(state.status, stale: isStale), font: .subheadline, reason: state.reason)
            }
        }
        // trunc-ok: the compact island holds a status word or a count
        .lineLimit(1)
        .environment(\.colorScheme, .dark)
        .dynamicTypeSize(...LiveCopy.largestType)
    }
}

/// The smallest form, shared with another app's activity: the count when something waits,
/// otherwise the bib.
struct IslandMinimal: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let state: TogoActivityAttributes.ContentState
    var isStale = false

    var body: some View {
        Group {
            if state.waiting > 0 && LiveStatus(state.status, stale: isStale) == .needsYou {
                Text(verbatim: "\(state.waiting)")
                    .font(.subheadline)
                    .fontWeight(.semibold)
                    .monospacedDigit()
                    .foregroundStyle(Color.togoAccent)
                    .contentTransition(reduceMotion ? .identity : .numericText(value: Double(state.waiting)))
                    .accessibilityLabel(LiveCopy.waiting(state.waiting))
            } else if let bib = state.bib {
                IslandBib(bib: bib, style: .caption)
            } else {
                IslandMark()
            }
        }
        .environment(\.colorScheme, .dark)
        .dynamicTypeSize(...LiveCopy.largestType)
    }
}

struct IslandExpandedLeading: View {
    let state: TogoActivityAttributes.ContentState
    var isStale = false

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            if let bib = state.bib { IslandBib(bib: bib) }
            Text(verbatim: state.agent)
                .font(.subheadline)
                .foregroundStyle(Color.togoSnow)
                // trunc-ok: the Dynamic Island has a fixed width; the agent name is on the lock screen and in the app
                .lineLimit(1)
        }
        .padding(.leading, 4)
        .environment(\.colorScheme, .dark)
        .dynamicTypeSize(...LiveCopy.largestType)
    }
}

struct IslandExpandedTrailing: View {
    let state: TogoActivityAttributes.ContentState
    var isStale = false

    var body: some View {
        Group {
            if LiveStatus(state.status, stale: isStale).showsElapsed {
                LiveElapsed(since: state.since)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
            }
        }
        .padding(.trailing, 4)
        .environment(\.colorScheme, .dark)
        .dynamicTypeSize(...LiveCopy.largestType)
    }
}

/// "Needs you · Studio", with the count when more than one request waits.
struct IslandExpandedCenter: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let state: TogoActivityAttributes.ContentState
    var isStale = false
    let machine: String

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            LiveStatusText(status: LiveStatus(state.status, stale: isStale), reason: state.reason)
            Text(verbatim: "· \(machine)")
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
                // trunc-ok: the Dynamic Island has a fixed width; the machine name is on the lock screen and in the app
                .lineLimit(1)
            Spacer(minLength: 8)
            if state.waiting > 1 && LiveStatus(state.status, stale: isStale).showsCount {
                Text(LiveCopy.waiting(state.waiting))
                    .font(.subheadline)
                    .foregroundStyle(Color.togoSnow)
                    // trunc-ok: "3 waiting"
                    .lineLimit(1)
                    .contentTransition(reduceMotion ? .identity : .numericText(value: Double(state.waiting)))
            }
        }
        .padding(.horizontal, 4)
        .frame(maxWidth: .infinity, alignment: .leading)
        .environment(\.colorScheme, .dark)
        .dynamicTypeSize(...LiveCopy.largestType)
    }
}

/// The command, for machines that opted in to sending it; empty otherwise.
struct IslandExpandedBottom: View {
    let state: TogoActivityAttributes.ContentState
    var isStale = false

    var body: some View {
        Group {
            if let command = LiveCopy.command(state.command) {
                Text(verbatim: command)
                    .font(.system(.footnote, design: .monospaced))
                    .foregroundStyle(Color.togoSnow)
                    // trunc-ok: the Live Activity has a fixed height: two lines of the command, middle-cut so its start and end show; tapping opens the request with the whole command
                    .lineLimit(2)
                    .truncationMode(.middle)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 12)
                    .padding(.vertical, 10)
                    .background(Color.togoRaised, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            }
        }
        .padding(.top, 4)
        .environment(\.colorScheme, .dark)
        .dynamicTypeSize(...LiveCopy.largestType)
    }
}
