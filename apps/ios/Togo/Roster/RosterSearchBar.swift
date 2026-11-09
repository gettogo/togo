import SwiftUI

/// The phone's bar row: the search field, shortened, with the menu trailing it on the same row,
/// so the menu never takes a row of its own above the field and the lines start right under
/// it. Both are glass, like the bar's own controls, and both are at least 44 pt tall. ⌘K (and
/// anything else that sets `searching`) puts the caret in the field.
struct RosterSearchBar<Trailing: View>: View {
    @Binding var query: String
    @Binding var searching: Bool
    var prompt = RosterCopy.searchPrompt
    @ViewBuilder var trailing: () -> Trailing

    @FocusState private var focused: Bool
    @ScaledMetric(relativeTo: .body) private var height: CGFloat = 44

    var body: some View {
        HStack(spacing: 10) {
            field
            trailing()
        }
        .padding(.horizontal, 16)
        .padding(.top, 2)
        .padding(.bottom, 6)
        .onChange(of: searching, initial: true) { _, new in focused = new }
        .onChange(of: focused) { _, new in searching = new }
    }

    private var field: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .foregroundStyle(Color.togoMute)
                .accessibilityHidden(true)
            TextField(text: $query, prompt: Text(prompt).foregroundStyle(Color.togoMute)) {
                Text(prompt)
            }
            .focused($focused)
            .foregroundStyle(Color.togoSnow)
            .textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .submitLabel(.search)
            .onKeyPress(.escape) {
                query = ""
                focused = false
                return .handled
            }
            .accessibilityIdentifier("roster.search")
            if !query.isEmpty {
                Button {
                    query = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .foregroundStyle(Color.togoMute)
                        .frame(minWidth: height, minHeight: height)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityLabel(RosterCopy.clearSearch)
            }
        }
        .font(.body)
        .padding(.leading, 14)
        // The clear button is a full target at the capsule's end, its glyph centred on the curve.
        .padding(.trailing, query.isEmpty ? 14 : 0)
        .frame(minHeight: height)
        .contentShape(.capsule)
        .onTapGesture { focused = true }
        .glassEffect(.regular.interactive(), in: .capsule)
    }
}

/// The roster menu's glass button for `RosterSearchBar`: a 44 pt circle, named for VoiceOver.
struct RosterMenuLabel: View {
    @ScaledMetric(relativeTo: .body) private var side: CGFloat = 44

    var body: some View {
        Label(RosterCopy.more, systemImage: "line.3.horizontal")
            .labelStyle(.iconOnly)
            .font(.body.weight(.medium))
            .foregroundStyle(Color.togoSnow)
            .frame(width: side, height: side)
            .contentShape(.circle)
            .glassEffect(.regular.interactive(), in: .circle)
    }
}
