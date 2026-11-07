import SwiftUI
import TogoKit

/// The chat's one live indicator, at the foot for the whole turn: "Working · 1m 4s" with the
/// spinner and what the agent does now, or "Waiting for you · 12s" with the still beacon while
/// a request is open. One view with one identity, so going from waiting to working is a morph
/// in place, never a second line popping in.
///
/// - The clock is the turn's stopwatch (`TurnClock`): working time pauses during a wait and
///   resumes where it paused; the digits roll each second.
/// - The glyph sits in a fixed slot and crossfades; the words crossfade; the step has a fixed
///   one-line slot, so the line's height never changes while it runs.
struct LiveLine: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @ScaledMetric(relativeTo: .footnote) private var side: CGFloat = 20
    let state: WorkingState

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            LiveGlyph(waiting: state.waiting)
                .frame(width: side)
            VStack(alignment: .leading, spacing: 2) {
                TimelineView(.periodic(from: state.clock.start, by: 1)) { context in
                    label(at: context.date)
                }
                stepLine
            }
        }
        .foregroundStyle(Color.togoMute)
        .frame(minHeight: 44, alignment: .leading)
        .frame(maxWidth: .infinity, alignment: .leading)
        .motion(.respond, value: state.waiting)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: WorkingCopy.spoken(state, now: .now)))
    }

    private func label(at now: Date) -> some View {
        let seconds = WorkingCopy.seconds(state, now: now).map { $0.rounded(.down) }
        return HStack(spacing: 0) {
            Text(verbatim: WorkingCopy.word(state))
                .fontWeight(state.waiting ? .semibold : .regular)
                .foregroundStyle(state.waiting ? Color.togoAccentText : Color.togoMute)
                .contentTransition(reduceMotion ? .identity : .interpolate)
            if let seconds {
                Text(verbatim: " · ")
                Text(verbatim: ChatCopy.clock(seconds))
                    .rollingNumber(seconds)
            }
        }
        .font(.system(.footnote, design: .monospaced))
    }

    /// What the agent does now, in a slot one line tall that is there all turn, so a step
    /// arriving or leaving never moves the chat.
    private var stepLine: some View {
        Text(verbatim: state.step ?? " ")
            .font(.footnote)
            // trunc-ok: one line so the live line never changes height; the full step is in the work line's steps and their details
            .lineLimit(1)
            .truncationMode(.middle)
            .opacity(state.step == nil ? 0 : 1)
            .contentTransition(reduceMotion ? .identity : .interpolate)
            .motion(.respond, value: state.step)
    }
}

/// The live glyph in a fixed slot: the still beacon while waiting on the user, the braille
/// spinner while working. They crossfade; the slot never changes size.
private struct LiveGlyph: View {
    @ScaledMetric(relativeTo: .footnote) private var beacon: CGFloat = 7
    let waiting: Bool

    var body: some View {
        ZStack {
            // Holds the baseline the spinner sits on, so the slot never shifts between states.
            Text(verbatim: "⠿")
                .font(.system(.body, design: .monospaced).weight(.heavy))
                .hidden()
            if waiting {
                Circle()
                    .fill(Color.togoAccent)
                    .frame(width: beacon, height: beacon)
                    .padding(3)
                    .background(Circle().fill(Color.togoAccent.opacity(0.18)))
                    .transition(.opacity)
            } else {
                LiveSpinner()
                    .transition(.opacity)
            }
        }
        .accessibilityHidden(true)
    }
}
