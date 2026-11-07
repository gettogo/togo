import SwiftUI

// Choreography: how content and its container move together (motion principles v2).
//
// - The container moves on `resize`, applied with `withAnimation` at the state change, so the
//   card, its siblings and the scroll position travel in one transaction. That movement is the
//   meaningful part: space opening where the new thing lands.
// - Content itself never moves to arrive. It `arrive`s (opacity only, no offset, no delay) or it
//   `unfold`s (inserted in place, uncovered by an edge that opens with its container). Content
//   leaving `conceal`s first, and only then does the container shrink.
// - Transitions take their timing from the transaction that inserts or removes the view: a
//   change made with no animation lands at once, so first loads, rows from "Earlier" and search
//   results never animate.
// - Every call site says why it moves: `// motion: <purpose> — <what the eye learns>`.

extension Motion {
    /// Whether a presented surface slides from its edge; under Reduce Motion it only fades.
    static func presentMoves(reduce: Bool) -> Bool { !reduce }

    /// Content arriving (a reply, a row, a section, a line): its opacity only, on the timing of
    /// the transaction that inserts it, with no delay and no offset. Removal conceals fast, so
    /// it is gone before the container shrinks. The same under Reduce Motion (opacity never
    /// moves anything).
    static func arrive(reduce: Bool) -> AnyTransition {
        .asymmetric(insertion: AnyTransition(Arriving()), removal: conceal(reduce: reduce))
    }

    /// Content uncovered by a growing container (More, a fold, command output, an editor): it
    /// is inserted at full opacity in its final place and never moves; its bottom edge opens
    /// downwards on the inserting transaction (the `resize` that moves its neighbours), so the
    /// growing edge uncovers it top to bottom. Removal conceals (opacity), then the container
    /// shrinks. Under Reduce Motion the content crossfades.
    static func unfold(reduce: Bool) -> AnyTransition {
        .asymmetric(
            insertion: reduce ? AnyTransition(Arriving()) : AnyTransition(Unfolding()),
            removal: conceal(reduce: reduce)
        )
    }

    /// Content leaving: a fast fade with no movement, so it is gone before its container
    /// shrinks.
    static func conceal(reduce: Bool) -> AnyTransition {
        AnyTransition(Concealing(animation: animation(.conceal, reduce: reduce)))
    }

    /// A swap of A for B in the same slot (loading → loaded, a card → its answered line): a
    /// crossfade in place. Nothing shifts: the two states sit in the same place.
    static func swap(reduce: Bool) -> AnyTransition {
        .opacity
    }

    /// A custom surface presenting from the edge it lives on (the prompt bar from the bottom),
    /// only when its appearance is news. Under Reduce Motion it fades.
    static func present(from edge: Edge, reduce: Bool) -> AnyTransition {
        presentMoves(reduce: reduce) ? .move(edge: edge).combined(with: .opacity) : .opacity
    }
}

/// Opacity on the inserting transaction's own timing; nothing moves.
private struct Arriving: Transition {
    func body(content: Content, phase: TransitionPhase) -> some View {
        content.opacity(phase.isIdentity ? 1 : 0)
    }
}

/// Content in its final place, uncovered from the top by an edge that opens with its container.
private struct Unfolding: Transition {
    func body(content: Content, phase: TransitionPhase) -> some View {
        content.mask(alignment: .top) {
            Rectangle().scaleEffect(x: 1, y: phase == .willAppear ? 0 : 1, anchor: .top)
        }
    }
}

/// Opacity on the `conceal` curve, run only when the change that removes the view is animated
/// at all.
private struct Concealing: Transition {
    let animation: Animation?

    func body(content: Content, phase: TransitionPhase) -> some View {
        content
            .opacity(phase.isIdentity ? 1 : 0)
            .transaction { transaction in
                guard transaction.animation != nil, !transaction.disablesAnimations else { return }
                transaction.animation = animation
            }
    }
}

// MARK: - Disclosure (§2.1, §2.2, §2.4)

/// Two-phase disclosure state: `open` drives the layout (whether the content is inserted),
/// `shown` drives the content's opacity. Opening sets both in one `resize` transaction; closing
/// conceals first and shrinks after. `generation` lets a reversal cancel a pending shrink.
struct Disclosure: Equatable, Sendable {
    var open = false
    var shown = false
    var generation = 0

    init(open: Bool = false) {
        self.open = open
        shown = open
    }

    /// Open as far as the person can tell: a closing disclosure already reads as closed.
    var isOpen: Bool { open && shown }

    enum Step: Equatable, Sendable {
        /// Insert and show in one `resize` transaction.
        case open
        /// Conceal now; shrink when the conceal completes, if `generation` still matches.
        case close(generation: Int)
    }

    /// Starts a toggle. A disclosure that is closing (concealed, not yet shrunk) reopens.
    mutating func begin() -> Step {
        generation += 1
        if isOpen { return .close(generation: generation) }
        return .open
    }

    /// The state after `begin` returned `.open`.
    mutating func applyOpen() {
        open = true
        shown = true
    }

    /// The state after `begin` returned `.close`: the content conceals, the layout stays.
    mutating func applyConceal() {
        shown = false
    }

    /// Shrinks after the conceal, unless the disclosure was toggled again meanwhile. Returns
    /// whether it shrank.
    @discardableResult
    mutating func finishClose(generation expected: Int) -> Bool {
        guard generation == expected, !shown else { return false }
        open = false
        return true
    }
}

extension Motion {
    /// Opens or closes a disclosure the spec's way: open in one `resize` transaction (content
    /// arrives with it); close by concealing the content, then shrinking on the same
    /// `resize` spring. A toggle during the conceal reverses it, and the stale shrink is dropped.
    @MainActor static func toggle(_ disclosure: Binding<Disclosure>, reduce: Bool) {
        switch disclosure.wrappedValue.begin() {
        case .open:
            withAnimation(animation(.resize, reduce: reduce)) { disclosure.wrappedValue.applyOpen() }
        case let .close(generation):
            withAnimation(animation(.conceal, reduce: reduce), completionCriteria: .logicallyComplete) {
                disclosure.wrappedValue.applyConceal()
            } completion: {
                withAnimation(animation(.resize, reduce: reduce)) {
                    _ = disclosure.wrappedValue.finishClose(generation: generation)
                }
            }
        }
    }

    /// Lands a disclosure in `open` without animation (restoring state, first render).
    @MainActor static func set(_ disclosure: Binding<Disclosure>, open: Bool) {
        disclosure.wrappedValue.generation += 1
        disclosure.wrappedValue.open = open
        disclosure.wrappedValue.shown = open
    }
}

extension View {
    /// Applies the disclosure to content inserted while `disclosure.open`: its opacity follows
    /// `disclosure.shown`; it arrives with its opacity only and conceals before the shrink.
    /// `index` is ignored (no stagger). For content the person uncovered (More, a fold), prefer
    /// `unfolds(_:)`.
    func disclosed(_ disclosure: Disclosure, index: Int = 0) -> some View {
        modifier(DisclosedModifier(shown: disclosure.shown, unfolds: false))
    }

    /// Applies the disclosure to content uncovered by its container: inserted in place at full
    /// opacity, so the container's growing edge uncovers it; on close it conceals, then the
    /// container shrinks. Under Reduce Motion it crossfades.
    func unfolds(_ disclosure: Disclosure) -> some View {
        modifier(DisclosedModifier(shown: disclosure.shown, unfolds: true))
    }
}

private struct DisclosedModifier: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let shown: Bool
    let unfolds: Bool

    func body(content: Content) -> some View {
        content
            .opacity(shown ? 1 : 0)
            .transition(unfolds ? Motion.unfold(reduce: reduceMotion) : Motion.arrive(reduce: reduceMotion))
    }
}

// MARK: - Mirrored A → B (§2.3)

/// A value shown on screen that follows a model value through the §2.3 choreography: the
/// outgoing content conceals, then the value changes on `resize` while the incoming content
/// arrives. Keep it in `@State` and update it from `.onChange` of the model value.
struct Mirrored<Value: Equatable>: Equatable {
    var value: Value
    /// Whether the current content is visible (false during the conceal).
    var shown = true
    var generation = 0

    init(_ value: Value) {
        self.value = value
    }
}

extension Motion {
    /// Moves a mirrored value to `target`: conceal (0.12 s), then the value lands in a `resize`
    /// transaction so the container takes the new size while the new content arrives. A newer
    /// target arriving mid-way wins. With `small` (the two states differ by under 4 pt) it is
    /// one `respond` transaction instead, for a `swap` transition.
    @MainActor static func settle<Value: Equatable>(
        _ mirrored: Binding<Mirrored<Value>>, to target: Value, small: Bool = false, reduce: Bool
    ) {
        guard mirrored.wrappedValue.value != target else { return }
        mirrored.wrappedValue.generation += 1
        let generation = mirrored.wrappedValue.generation
        if small {
            withAnimation(animation(.respond, reduce: reduce)) {
                mirrored.wrappedValue.value = target
                mirrored.wrappedValue.shown = true
            }
            return
        }
        withAnimation(animation(.conceal, reduce: reduce), completionCriteria: .logicallyComplete) {
            mirrored.wrappedValue.shown = false
        } completion: {
            guard mirrored.wrappedValue.generation == generation else { return }
            withAnimation(animation(.resize, reduce: reduce)) {
                mirrored.wrappedValue.value = target
                mirrored.wrappedValue.shown = true
            }
        }
    }
}

extension View {
    /// The content of a mirrored slot: it fades with `mirrored.shown` and arrives or conceals
    /// as the branch changes.
    func settling(_ mirrored: Mirrored<some Equatable>) -> some View {
        modifier(DisclosedModifier(shown: mirrored.shown, unfolds: false))
    }
}

// MARK: - Slots, numbers, the ambient budget

extension View {
    /// A fixed-size accessory slot (a spinner, a check, a glyph) whose content crossfades on
    /// `respond` when `key` changes, so nothing around it reflows.
    func slot(width: CGFloat? = nil, height: CGFloat? = nil, alignment: Alignment = .center,
              key: some Equatable) -> some View {
        frame(width: width, height: height, alignment: alignment)
            .motion(.respond, value: key)
    }

    /// A clock or any value that changes every second or so (timers, streamed percentages):
    /// monospaced digits that simply change. Never animated: no content transition, and any
    /// transaction inherited from a store update is stripped, so nothing ghosts or slides.
    func clockText() -> some View {
        monospacedDigit()
            .contentTransition(.identity)
            .transaction { $0.animation = nil }
    }

    /// An event-like count the person cares about that changes at most every few seconds (the
    /// needs count, checklist done, the queued count): the digits roll on `respond`, downwards
    /// when `countsDown`, or in the direction the value moved when `countsDown` is nil. Never
    /// inside a `TimelineView`. Under Reduce Motion it changes in place.
    func countChange(_ value: Double, countsDown: Bool? = nil) -> some View {
        modifier(CountChange(value: value, countsDown: countsDown))
    }
}

private struct CountChange: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let value: Double
    let countsDown: Bool?

    func body(content: Content) -> some View {
        content
            .monospacedDigit()
            .contentTransition(reduceMotion ? .identity : transition)
            .animation(reduceMotion ? nil : Motion.animation(.respond, reduce: false), value: value)
    }

    private var transition: ContentTransition {
        guard let countsDown else { return .numericText(value: value) }
        return .numericText(countsDown: countsDown)
    }
}

extension EnvironmentValues {
    /// Set by the one self-moving thing on a screen (a needs card's drift); other ambient loops
    /// below it hold still (spec §4, one loop per screen).
    @Entry var ambientTaken = false
}

/// Raised by a view running the screen's one ambient loop (a needs card's drift). A screen
/// reads it with `onPreferenceChange` and passes `ambientTaken` to loops elsewhere on it, such
/// as the mission pill's beacon, so only one thing moves by itself (spec §4).
struct AmbientClaim: PreferenceKey {
    static let defaultValue = false
    static func reduce(value: inout Bool, nextValue: () -> Bool) { value = value || nextValue() }
}
