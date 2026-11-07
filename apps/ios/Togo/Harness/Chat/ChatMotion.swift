import SwiftUI
import TogoKit

/// How the chat's live line follows the store (motion principles v2 §3, §4).
enum LiveFollow {
    enum Change: Equatable {
        /// The line's contents changed while it stays (a tick, a step, waiting ↔ working), or
        /// the chat has not loaded yet: no animation at all, so the store's arrival
        /// transaction never reaches the clock.
        case cut
        /// A turn started: the line is simply there (the spinner starting is the signal) while
        /// the rows above make room on `resize`.
        case appear
        /// The turn ended: the line leaves in the same `resize` transaction that turns the
        /// work line into "Worked … ✓".
        case end
    }

    static func change(from old: WorkingState?, to new: WorkingState?, loaded: Bool) -> Change {
        guard loaded else { return .cut }
        switch (old, new) {
        case (nil, .some): return .appear
        case (.some, nil): return .end
        default: return .cut
        }
    }
}

extension LiveFollow.Change {
    /// Whether the change lands on `resize` (the rows make room); a cut has no animation.
    var animates: Bool { self != .cut }
}

/// Which answer the person tapped on a request, so the answered line can come out of it
/// (cause → effect): "Approve" travels into "Approved". Answered anywhere else, nothing was
/// tapped here and the card collapses in place instead.
struct AnswerTap<Answer: Equatable>: Equatable {
    private(set) var answer: Answer?

    init(_ answer: Answer? = nil) {
        self.answer = answer
    }

    /// The person tapped `answer`; a later tap replaces an earlier one (after a failure).
    mutating func record(_ answer: Answer) {
        self.answer = answer
    }

    mutating func clear() {
        answer = nil
    }

    /// Whether `answer`'s control is where the answered line comes from.
    func isSource(_ answer: Answer) -> Bool {
        self.answer == answer
    }

    /// How the card settles into its answered line.
    func settle(answeredHere: Bool) -> AnswerSettle {
        answer != nil && answeredHere ? .travel : .collapse
    }
}

enum AnswerSettle: Equatable {
    /// The person's own answer: one `resize` transaction; the card conceals around the tapped
    /// control while the answered line moves out of it (matched geometry). No bounce: the
    /// tap's haptic and the travel already confirm it.
    case travel
    /// Answered elsewhere, or expired: the card conceals, the row shrinks to its line in
    /// place, and the check bounces once, since nothing else tells the person.
    case collapse

    /// The answered line's check bounces once only when nothing on this screen caused it.
    var bounces: Bool { self == .collapse }
}

/// The matched-geometry id shared by a request's tapped answer and its answered line.
enum AnswerMatch {
    static func id(_ itemID: String) -> String { "\(itemID).answer" }
}

extension View {
    /// The tapped answer's control, or the answered line it becomes: both carry the same id in
    /// one namespace, so the line comes out of the control. Off for every other control.
    @ViewBuilder func answerSource(_ id: String, in namespace: Namespace.ID, active: Bool) -> some View {
        if active {
            // motion: cause → effect — the tapped answer becomes the answered line, in the place the person touched.
            matchedGeometryEffect(id: id, in: namespace, properties: .position, anchor: .leading)
        } else {
            self
        }
    }
}
