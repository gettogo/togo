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
    /// with a small vertical exchange on `respond` (a crossfade under Reduce Motion), so nothing
    /// pops. A change inside one case (new text, a new value) updates in place.
    func phaseSwap(_ phase: Any) -> some View {
        modifier(PhaseSwap(key: CaseName.of(phase)))
    }
}

private struct PhaseSwap: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let key: String

    func body(content: Content) -> some View {
        ZStack {
            content
                .id(key)
                .transition(Motion.swap(reduce: reduceMotion))
        }
        .motion(.respond, value: key)
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
