import SwiftUI

// Choreography: how content and its container move together (the motion spec, §1.3 and §2).
//
// - The container moves on `resize`, applied with `withAnimation` at the state change, so the
//   card, its siblings and the scroll offset travel in one transaction.
// - Content entering a grown container `reveal`s a beat later; content leaving `conceal`s first,
//   and only then does the container shrink.
// - Transitions take their timing from the transaction that inserts or removes the view: a
//   change made with no animation lands at once, so rows on first render, from "Earlier" or
//   from a search never animate.

extension Motion {
    /// The reveal starts this long after the resize, so the container is already moving when
    /// the content begins to show.
    static let revealDelay = 0.06
    /// A user-caused reveal of a few items steps by this much per item...
    static let staggerStep = 0.03
    /// ...and never by more than this in total.
    static let staggerCap = 0.15

    /// How far revealed content rises as it fades in; none under Reduce Motion.
    static func revealRise(reduce: Bool) -> CGFloat { reduce ? 0 : 6 }
    /// How far a swapped pair exchanges vertically; none under Reduce Motion.
    static func swapShift(reduce: Bool) -> CGFloat { reduce ? 0 : 4 }
    /// Whether a presented surface slides from its edge; under Reduce Motion it only fades.
    static func presentMoves(reduce: Bool) -> Bool { !reduce }

    /// The extra delay for the `index`th item of a user-caused reveal: 30 ms per item, at most
    /// 150 ms.
    static func staggerDelay(_ index: Int) -> Double {
        min(Double(max(index, 0)) * staggerStep, staggerCap)
    }

    /// The curve content enters on: `reveal`, a beat after the resize starts (plus its stagger).
    /// Under Reduce Motion a plain short fade with no delay.
    static func revealAnimation(reduce: Bool, index: Int = 0) -> Animation? {
        guard let base = animation(.reveal, reduce: reduce) else { return nil }
        return reduce ? base : base.delay(revealDelay + staggerDelay(index))
    }

    /// Content entering a grown container: a fade and a 6 pt rise, a beat after the resize.
    /// Under Reduce Motion a fade only.
    static func reveal(reduce: Bool, index: Int = 0) -> AnyTransition {
        AnyTransition(Choreographed(
            rise: revealRise(reduce: reduce), animation: revealAnimation(reduce: reduce, index: index)
        ))
    }

    /// Content leaving: a fast fade with no movement, so it is gone before its container
    /// shrinks.
    static func conceal(reduce: Bool) -> AnyTransition {
        AnyTransition(Choreographed(rise: 0, animation: animation(.conceal, reduce: reduce)))
    }

    /// Insert reveals, remove conceals (fast, no movement), so a removal never lingers outside
    /// a container that is already shrinking.
    static func revealConceal(reduce: Bool, index: Int = 0) -> AnyTransition {
        .asymmetric(insertion: reveal(reduce: reduce, index: index), removal: conceal(reduce: reduce))
    }

    /// A swap of A for B in the same slot (Waiting → Working, a card → its answered line): a
    /// crossfade with a 4 pt vertical exchange. Under Reduce Motion a crossfade.
    static func swap(reduce: Bool) -> AnyTransition {
        let shift = swapShift(reduce: reduce)
        return .asymmetric(
            insertion: .opacity.combined(with: .offset(y: shift)),
            removal: .opacity.combined(with: .offset(y: -shift))
        )
    }

    /// A custom surface presenting from an edge (a toast from the top, a bar from the bottom).
    /// Under Reduce Motion it fades.
    static func present(from edge: Edge, reduce: Bool) -> AnyTransition {
        presentMoves(reduce: reduce) ? .move(edge: edge).combined(with: .opacity) : .opacity
    }
}

/// Opacity plus a small rise, run on the transition's own role only when the change that
/// inserts or removes the view is animated at all.
private struct Choreographed: Transition {
    let rise: CGFloat
    let animation: Animation?

    func body(content: Content, phase: TransitionPhase) -> some View {
        content
            .opacity(phase.isIdentity ? 1 : 0)
            .offset(y: phase == .willAppear ? rise : 0)
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
    /// reveals a beat later); close by concealing the content, then shrinking on the same
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
    /// `disclosure.shown`, and it reveals and conceals with the spec's transitions.
    func disclosed(_ disclosure: Disclosure, index: Int = 0) -> some View {
        modifier(DisclosedModifier(shown: disclosure.shown, index: index))
    }
}

private struct DisclosedModifier: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let shown: Bool
    let index: Int

    func body(content: Content) -> some View {
        content
            .opacity(shown ? 1 : 0)
            .transition(Motion.revealConceal(reduce: reduceMotion, index: index))
    }
}

// MARK: - Mirrored A → B (§2.3)

/// A value shown on screen that follows a model value through the §2.3 choreography: the
/// outgoing content conceals, then the value changes on `resize` while the incoming content
/// reveals. Keep it in `@State` and update it from `.onChange` of the model value.
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
    /// transaction so the container takes the new size while the new content reveals. A newer
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
    /// The content of a mirrored slot: it fades with `mirrored.shown` and reveals or conceals
    /// as the branch changes.
    func settling(_ mirrored: Mirrored<some Equatable>) -> some View {
        modifier(DisclosedModifier(shown: mirrored.shown, index: 0))
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

    /// A number that rolls when it changes (clocks, counts, percentages), with monospaced
    /// digits; under Reduce Motion it changes in place.
    func rollingNumber(_ value: Double) -> some View {
        modifier(RollingNumber(value: value))
    }
}

private struct RollingNumber: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let value: Double

    func body(content: Content) -> some View {
        content
            .monospacedDigit()
            .contentTransition(reduceMotion ? .identity : .numericText(value: value))
            .animation(reduceMotion ? nil : Motion.animation(.respond, reduce: false), value: value)
    }
}

extension EnvironmentValues {
    /// Set by the one self-moving thing on a screen (a needs card's drift); other ambient loops
    /// below it hold still (spec §4, one loop per screen).
    @Entry var ambientTaken = false
}

// MARK: - A user-caused stagger

extension View {
    /// Steps content in as it appears: a fade and a 6 pt rise, `index` × 30 ms after the
    /// container starts to grow (at most 150 ms). For a reveal of a few items the person caused,
    /// such as the More list; inactive, it shows at once. Under Reduce Motion a plain fade.
    func revealOnAppear(index: Int, active: Bool = true) -> some View {
        modifier(RevealOnAppear(index: index, active: active))
    }
}

private struct RevealOnAppear: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let index: Int
    let active: Bool
    @State private var appeared = false

    func body(content: Content) -> some View {
        let shows = appeared || !active
        content
            .opacity(shows ? 1 : 0)
            .offset(y: shows ? 0 : Motion.revealRise(reduce: reduceMotion))
            .onAppear {
                guard active, !appeared else { return }
                withAnimation(Motion.revealAnimation(reduce: reduceMotion, index: index)) { appeared = true }
            }
    }
}

/// Raised by a view running the screen's one ambient loop (a needs card's drift). A screen
/// reads it with `onPreferenceChange` and passes `ambientTaken` to loops elsewhere on it, such
/// as the mission pill's beacon, so only one thing moves by itself (spec §4).
struct AmbientClaim: PreferenceKey {
    static let defaultValue = false
    static func reduce(value: inout Bool, nextValue: () -> Bool) { value = value || nextValue() }
}
