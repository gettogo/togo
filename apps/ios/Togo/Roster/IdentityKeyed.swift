import SwiftUI

// Lists whose rows arrive and leave while the screen is open (motion spec §2.5): the change of
// identity moves in one `resize` transaction, so the rows that stay, the card around them and
// the scroll position travel together, while the text inside a row keeps updating in place with
// no animation at all. Used by mission control, the roster, the review lists and discovery.

/// Whether a change from `old` to `new` moves the layout's identity (rows or sections arriving
/// or leaving), judged by `key`. Pure, so the rule is testable without views.
/// A change of `quiet` (a search query, a filter) lands at once even when it changes identity:
/// results are never animated (§2.5, §7).
enum IdentityMotion {
    static func changesIdentity<Value, Key: Equatable>(
        from old: Value, to new: Value, key: (Value) -> Key, quiet: (Value) -> AnyHashable = { _ in 0 }
    ) -> Bool {
        quiet(old) == quiet(new) && key(old) != key(new)
    }
}

/// Shows `value` through a mirror that follows it: a change of `key(value)` lands in a `resize`
/// transaction (inserted rows reveal, removed ones conceal, everything else glides); any other
/// change lands at once. The first render never animates.
struct IdentityKeyed<Value: Equatable, Key: Equatable, Content: View>: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let value: Value
    let key: (Value) -> Key
    let quiet: (Value) -> AnyHashable
    let content: (Value) -> Content
    @State private var shown: Value

    init(
        _ value: Value, key: @escaping (Value) -> Key, quiet: @escaping (Value) -> AnyHashable = { _ in 0 },
        @ViewBuilder content: @escaping (Value) -> Content
    ) {
        self.value = value
        self.key = key
        self.quiet = quiet
        self.content = content
        _shown = State(initialValue: value)
    }

    var body: some View {
        content(shown)
            .onChange(of: value) { _, new in
                if IdentityMotion.changesIdentity(from: shown, to: new, key: key, quiet: quiet) {
                    // motion: preventing a jump — rows arriving or leaving open and close space; the rest glide
                    withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { shown = new }
                } else {
                    shown = new
                }
            }
    }
}

// MARK: - Phase swaps (§2.3)

/// The case of an enum value without its payload: "failed" for `.failed("…")`, so a swap keys on
/// the state's kind and not on every word inside it.
enum CaseName {
    static func of(_ value: Any) -> String {
        let mirror = Mirror(reflecting: value)
        if mirror.displayStyle == .enum, let label = mirror.children.first?.label { return label }
        return String(describing: value)
    }
}

extension View {
    /// Content that is replaced whole when `phase` changes case, such as a sheet or a screen
    /// going loading → loaded → failed in a fixed frame: the old content and the new crossfade
    /// in place on `respond`, with no shift. A phase that lasted under 250 ms is replaced at once,
    /// so a fast load never flickers a spinner. A change inside one case (new text, a new value)
    /// updates in place.
    func phaseSwap(_ phase: Any) -> some View {
        modifier(PhaseSwap(key: CaseName.of(phase)))
    }
}

/// When a phase swap crossfades: only when the phase it replaces was on screen long enough to be
/// read. Pure, so the rule is testable without views.
enum PhaseSwapTiming {
    /// A phase shown for less than this is replaced at once.
    static let minimumShown: TimeInterval = 0.25

    static func crossfades(shownFor elapsed: TimeInterval) -> Bool {
        elapsed >= minimumShown
    }
}

/// When the current phase began; a reference, so the transaction closure can read and move it in
/// the same update that changes the phase.
private final class PhaseStart {
    var key: String
    var at: Date

    init(key: String, at: Date) {
        self.key = key
        self.at = at
    }

    /// Records a change to `key` at `now`; returns whether the swap crossfades. A repeat call
    /// for the same key in one update answers the same way.
    func change(to key: String, now: Date) -> Bool? {
        guard key != self.key else { return nil }
        let crossfades = PhaseSwapTiming.crossfades(shownFor: now.timeIntervalSince(at))
        self.key = key
        at = now
        return crossfades
    }
}

private struct PhaseSwap: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let key: String
    @State private var start: PhaseStart

    init(key: String) {
        self.key = key
        _start = State(initialValue: PhaseStart(key: key, at: .now))
    }

    func body(content: Content) -> some View {
        ZStack {
            // motion: state change — the old phase and the new crossfade in the same place
            content
                .id(key)
                .transition(Motion.swap(reduce: reduceMotion))
        }
        .transaction(value: key) { transaction in
            guard let crossfades = start.change(to: key, now: .now), !transaction.disablesAnimations else { return }
            let quick = !crossfades
            // motion: state change — loading became loaded (or failed) in the same place
            transaction.animation = quick ? nil : Motion.animation(.respond, reduce: reduceMotion)
        }
    }
}

// MARK: - Lines that come and go in forms (§2.1, §2.5)

/// A model value as drawn, for content inside a system `List` or `Form` (an error line, a
/// result, a loading row) whose arrival must be an explicit transaction. Until the value first
/// changes it reads through to the live one, so the first render never waits or animates.
struct LiveMirror<Value: Equatable>: Equatable {
    fileprivate var held: [Value] = []

    func read(_ live: Value) -> Value { held.first ?? live }
}

extension View {
    /// Keeps `mirror` following `live`: each change lands in one `resize` transaction, so a
    /// line appearing or leaving moves the rows around it with it, and inserted rows reveal.
    func mirrors<Value: Equatable>(_ live: Value, into mirror: Binding<LiveMirror<Value>>) -> some View {
        modifier(MirrorsLive(live: live, mirror: mirror))
    }
}

private struct MirrorsLive<Value: Equatable>: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let live: Value
    @Binding var mirror: LiveMirror<Value>

    func body(content: Content) -> some View {
        content.onChange(of: live) { _, new in
            // motion: preventing a jump — a line arriving or leaving moves the rows around it with it
            withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { mirror.held = [new] }
        }
    }
}

extension View {
    /// Identity-keyed insert/remove for a `Form` section or `List` whose rows come from a model
    /// (spec §2.5): when `rows` (which rows and lines exist, never their words) changes, that
    /// update runs in one `resize` transaction, so the rows that stay, the section's footer and
    /// the scroll position move with the ones arriving or leaving. Any other update keeps its own
    /// transaction (usually none).
    func resizesOnRows(_ rows: some Equatable) -> some View {
        modifier(ResizesOnRows(rows: rows))
    }
}

private struct ResizesOnRows<Rows: Equatable>: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let rows: Rows

    func body(content: Content) -> some View {
        // identity-keyed insert/remove
        content.transaction(value: rows) { transaction in
            guard !transaction.disablesAnimations else { return }
            transaction.animation = Motion.animation(.resize, reduce: reduceMotion)
        }
    }
}
