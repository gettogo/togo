import SwiftUI

/// The text fields an action revealed, and which one holds the keyboard. Revealing a field
/// (an "Other answer" row, a Set button over a secret, "With instructions…") focuses it, so
/// one tap is enough; hiding the focused field lets the keyboard go. Pure, so the rules are
/// tested without a view.
struct RevealedFields<ID: Hashable>: Equatable {
    private(set) var open: Set<ID> = []
    /// The field that should hold the keyboard now; nil lets it go.
    private(set) var focus: ID?

    init() {}

    func isOpen(_ id: ID) -> Bool { open.contains(id) }

    var isEmpty: Bool { open.isEmpty }

    /// Opens the field and hands it the keyboard. Revealing an open field focuses it again.
    mutating func reveal(_ id: ID) {
        open.insert(id)
        focus = id
    }

    mutating func hide(_ id: ID) {
        open.remove(id)
        if focus == id { focus = nil }
    }

    /// The keyboard moved by itself: the user tapped another field, or dismissed it.
    mutating func focusMoved(to id: ID?) {
        focus = id.flatMap { open.contains($0) ? $0 : nil }
    }
}

/// Brings a row of the surrounding conversation into view above the composer and keyboard,
/// by row id. The chat sets it; elsewhere a List or Form keeps a focused field visible itself.
struct RevealRowAction {
    let run: (String) -> Void
    func callAsFunction(_ row: String) { run(row) }
}

extension EnvironmentValues {
    @Entry var revealRow: RevealRowAction?
}

/// Focuses a field the moment it appears, when an action just revealed it, and brings its row
/// up above the keyboard. A field cannot take focus in the update that inserts it, so the
/// focus lands on the next turn of the run loop, once the field is in the hierarchy.
private struct FocusOnReveal<Value: Hashable>: ViewModifier {
    @Environment(\.revealRow) private var revealRow
    let focus: FocusState<Value?>.Binding
    let value: Value
    let revealed: Bool
    let row: String?

    func body(content: Content) -> some View {
        content
            .focused(focus, equals: value)
            .onAppear { if revealed { land() } }
            .onChange(of: revealed) { _, now in if now { land() } }
    }

    private func land() {
        Task { @MainActor in
            focus.wrappedValue = value
            if let row { revealRow?(row) }
        }
    }
}

private struct FocusFlagOnReveal: ViewModifier {
    @Environment(\.revealRow) private var revealRow
    let focus: FocusState<Bool>.Binding
    let revealed: Bool
    let row: String?

    func body(content: Content) -> some View {
        content
            .focused(focus)
            .onAppear { if revealed { land() } }
            .onChange(of: revealed) { _, now in if now { land() } }
    }

    private func land() {
        Task { @MainActor in
            focus.wrappedValue = true
            if let row { revealRow?(row) }
        }
    }
}

extension View {
    /// `.focused(focus, equals: value)` that takes the keyboard as soon as the field appears
    /// while `revealed` is true (an action just opened it), and scrolls `row` into view.
    func focusOnReveal<Value: Hashable>(
        _ focus: FocusState<Value?>.Binding, equals value: Value, revealed: Bool, row: String? = nil
    ) -> some View {
        modifier(FocusOnReveal(focus: focus, value: value, revealed: revealed, row: row))
    }

    /// `.focused(focus)` that takes the keyboard as soon as the field appears while `revealed`.
    func focusOnReveal(_ focus: FocusState<Bool>.Binding, revealed: Bool, row: String? = nil) -> some View {
        modifier(FocusFlagOnReveal(focus: focus, revealed: revealed, row: row))
    }
}

/// Focus asked for while a sheet, popover or dialog is still closing does not land: the
/// closing presentation keeps the keyboard. "With instructions…" and a palette command that
/// takes an argument both close one as they hand the composer the line, so the focus is asked
/// for again at these moments until it holds.
enum FocusRetry {
    static let delays: [Duration] = [.zero, .milliseconds(150), .milliseconds(350), .milliseconds(650)]

    /// Whether try `attempt` (0-based) asks for the focus: only while it has not landed.
    static func shouldRetry(attempt: Int, focused: Bool) -> Bool {
        !focused && attempt < delays.count
    }

    @MainActor static func land(isFocused: @escaping @MainActor () -> Bool, focus: @escaping @MainActor () -> Void) async {
        for (attempt, delay) in delays.enumerated() {
            if delay > .zero { try? await Task.sleep(for: delay) }
            guard !Task.isCancelled, shouldRetry(attempt: attempt, focused: isFocused()) else { return }
            focus()
        }
    }
}
