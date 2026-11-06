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
            .lineLimit(1)
    }

    private static func color(_ status: LiveStatus) -> Color {
        switch status {
        case .needsYou: Color.togoAccentText
        case .expired, .notUpdated, .offline: Color.togoMute
        default: Color.togoSnow
        }
    }
}

/// The Live Activity on the lock screen and in the notification list. Metadata by default; the
/// command shows only for machines that opted in to sending it. The whole card is one tap
/// target that opens the session in the app.
struct LiveLockScreenView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let state: TogoActivityAttributes.ContentState
    let machine: String
    var isStale = false

    private var status: LiveStatus { LiveStatus(state.status, stale: isStale) }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                identityLine
                statusLine
            }
            if let command = LiveCopy.command(state.command) {
                commandBlock(command)
            }
        }
        .padding(.horizontal, 18)
        .padding(.vertical, 16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .dynamicTypeSize(...LiveCopy.largestType)
        .accessibilityElement(children: .combine)
    }

    private var identityLine: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            if let bib = state.bib {
                BibText(bib)
            }
            Text(verbatim: LiveCopy.agentOnMachine(agent: state.agent, machine: machine))
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
                .lineLimit(1)
            Spacer(minLength: 12)
            if status.showsElapsed {
                LiveElapsed(since: state.since)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
                    .layoutPriority(1)
            }
        }
    }

    private var statusLine: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            LiveStatusText(status: status, reason: state.reason)
                .layoutPriority(2)
            Spacer(minLength: 12)
            if state.waiting > 1 && status.showsCount {
                Text(LiveCopy.waiting(state.waiting))
                    .font(.subheadline)
                    .foregroundStyle(Color.togoSnow)
                    .lineLimit(1)
                    .contentTransition(reduceMotion ? .identity : .numericText(value: Double(state.waiting)))
            }
        }
    }

    private func commandBlock(_ command: String) -> some View {
        Text(verbatim: command)
            .font(.system(.footnote, design: .monospaced))
            .foregroundStyle(Color.togoSnow)
            .lineLimit(2)
            .truncationMode(.middle)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
            .background(Color.togoRaised, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }
}
