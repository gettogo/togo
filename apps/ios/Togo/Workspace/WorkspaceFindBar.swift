import SwiftUI

/// The find row above the terminal: a field, "No matches" when the last search missed, previous,
/// next and Done. The row itself reveals and conceals with the rows above the terminal.
struct WorkspaceFindBar: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// The focused terminal's search; nil when it has none.
    let search: TerminalSearch?
    /// Whether the field should have the keyboard, as the workspace asks.
    @Binding var focused: Bool
    let onDone: () -> Void

    @State private var text = ""
    @State private var missed = false
    @FocusState private var fieldFocused: Bool

    var body: some View {
        HStack(spacing: 4) {
            TextField(WorkspaceCopy.findPrompt, text: $text)
                .font(.body)
                .foregroundStyle(Color.togoSnow)
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .submitLabel(.search)
                // The row is inserted in the same update that asks for the keyboard: focus lands
                // on the next turn of the run loop, so one tap on Find is enough.
                .focusOnReveal($fieldFocused, revealed: focused)
                .onSubmit { find(backwards: false) }
                .onChange(of: text) { setMissed(false) }
            if missed {
                Text(WorkspaceCopy.noMatches)
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                    // trunc-ok: the fixed words "No matches"
                    .lineLimit(1)
            }
            Button(WorkspaceCopy.findPrevious, systemImage: "chevron.up") { find(backwards: true) }
                .labelStyle(.iconOnly)
                .frame(minWidth: 44, minHeight: 44)
            Button(WorkspaceCopy.findNext, systemImage: "chevron.down") { find(backwards: false) }
                .labelStyle(.iconOnly)
                .frame(minWidth: 44, minHeight: 44)
            Button(WorkspaceCopy.findDone, action: onDone)
                .font(.body.weight(.semibold))
                .frame(minHeight: 44)
        }
        .foregroundStyle(Color.togoSnow)
        .padding(.horizontal, 16)
        .onChange(of: fieldFocused) { _, on in focused = on }
    }

    private func find(backwards: Bool) {
        guard !text.isEmpty, let search else { return }
        setMissed(!search.find(text, backwards))
    }

    /// "No matches" comes and goes in place, on `respond`.
    private func setMissed(_ value: Bool) {
        guard missed != value else { return }
        // motion: state change — "No matches" comes and goes in place.
        withAnimation(Motion.animation(.respond, reduce: reduceMotion)) { missed = value }
    }
}
