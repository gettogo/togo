import SwiftUI
import TogoKit

/// The chat's one live indicator, at the foot for the whole turn: "Working · 1m 4s" with the
/// spinner and what the agent does now, or "Waiting for you · 12s" with the still beacon while
/// a request is open. One view with one identity, so going from waiting to working is a morph
/// in place, never a second line popping in.
///
/// - The clock is never animated (motion principles v2 §4). It is its own `Text`, last on its
///   line, so "9s" → "10s" pushes nothing; one `TimelineView` ticks from the origin of the
///   number on screen (`TurnClock.displayOrigin`), so the digit changes exactly on the tick.
/// - Waiting ↔ working changes the word and the glyph in place; the number cuts to the other
///   clock (the wait, or the paused working time) without rolling.
/// - The step is status text that changes many times a minute: instant, in a one-line slot
///   that is there all turn, so the line's height never changes while it runs.
struct LiveLine: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.loopsPaused) private var loopsPaused
    @ScaledMetric(relativeTo: .footnote) private var side: CGFloat = 20
    let state: WorkingState

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            LiveGlyph(waiting: state.waiting)
                .frame(width: side)
            VStack(alignment: .leading, spacing: 2) {
                label
                stepLine
            }
        }
        .foregroundStyle(Color.togoMute)
        .frame(minHeight: 44, alignment: .leading)
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(Text(verbatim: WorkingCopy.spoken(state, now: .now)))
    }

    /// The word, the separator, then the clock: the clock last, so its width never moves them.
    private var label: some View {
        HStack(spacing: 0) {
            Text(verbatim: WorkingCopy.word(state))
                .fontWeight(state.waiting ? .semibold : .regular)
                .foregroundStyle(state.waiting ? Color.togoAccentText : Color.togoMute)
                .contentTransition(reduceMotion ? .identity : .interpolate)
                // motion: state change — "Working" becomes "Waiting for you" in the beacon, in place; only the word moves.
                .motion(.respond, value: state.waiting)
            if WorkingCopy.seconds(state, now: .now) != nil {
                Text(verbatim: LiveLineLayout.separator)
                TimelineView(.clock(from: LiveLineLayout.origin(state), paused: loopsPaused)) { context in
                    Text(verbatim: WorkingCopy.clock(state, now: context.date))
                        .clockText()
                }
            }
        }
        .font(.system(.footnote, design: .monospaced))
    }

    /// What the agent does now, in a slot one line tall that is there all turn, so a step
    /// arriving or leaving never moves the chat. It changes instantly: it is status text.
    private var stepLine: some View {
        Text(verbatim: state.step ?? " ")
            .font(.footnote)
            // trunc-ok: one line so the live line never changes height; the full step is in the work line's steps and their details
            .lineLimit(1)
            .truncationMode(.middle)
            .opacity(state.step == nil ? 0 : 1)
            .contentTransition(.identity)
            .transaction { $0.animation = nil }
    }
}

/// The live line's layout rules, pure so they are tested without a view.
enum LiveLineLayout {
    static let separator = " · "

    /// The pieces of the line in order. The clock, when there is one, is always last.
    enum Segment: Equatable {
        case word(String)
        case separator
        case clock(String)
    }

    static func segments(_ state: WorkingState, now: Date) -> [Segment] {
        var out: [Segment] = [.word(WorkingCopy.word(state))]
        if WorkingCopy.seconds(state, now: now) != nil {
            out += [.separator, .clock(WorkingCopy.clock(state, now: now))]
        }
        return out
    }

    /// Where the one-second schedule starts: the origin of the number on screen, so each tick
    /// lands exactly when the shown second changes. Never `.now`, which re-anchors on render.
    static func origin(_ state: WorkingState) -> Date {
        state.clock.displayOrigin(waiting: state.waiting)
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
            } else {
                LiveSpinner()
            }
        }
        // motion: state change — the spinner gives way to the still beacon in the same fixed slot: the agent stopped for the user.
        .motion(.respond, value: waiting)
        .accessibilityHidden(true)
    }
}

/// A once-a-second clock that holds still while the view is hidden (`loopsPaused`): a chat
/// under the terminal does not redraw its timers.
struct ClockSchedule: TimelineSchedule {
    let origin: Date
    let paused: Bool

    func entries(from startDate: Date, mode: TimelineScheduleMode) -> AnyIterator<Date> {
        if paused {
            var given = false
            return AnyIterator {
                defer { given = true }
                return given ? nil : startDate
            }
        }
        return AnyIterator(PeriodicTimelineSchedule(from: origin, by: 1).entries(from: startDate, mode: mode))
    }
}

extension TimelineSchedule where Self == ClockSchedule {
    static func clock(from origin: Date, paused: Bool) -> ClockSchedule {
        ClockSchedule(origin: origin, paused: paused)
    }
}
