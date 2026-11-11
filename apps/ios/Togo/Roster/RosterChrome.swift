import SwiftUI

/// The roster's controls and where each sits, per layout (roster.md). Each is a symbol with a
/// title, so an iPhone Duo can stand it in its vertical bar and fall back to the title in the
/// overflow menu. Nothing hides in a "more" menu: the filter is the list's first row, Add
/// machine and Archive are rows at its end.

/// A roster bar item.
enum RosterBarItem: Hashable, Sendable {
    case settings, newSession, search
}

/// Which bar slot holds which roster item, for a layout. Each item appears once per layout;
/// the filter is never in a bar (it is the list's first row on every layout).
struct RosterBar: Equatable, Sendable {
    /// Leading to trailing; top to bottom in the folded Duo's vertical bar.
    var topTrailing: [RosterBarItem] = []
    var bottom: [RosterBarItem] = []

    /// `wide`: the roster is the sidebar column (regular width). Before a machine is added
    /// there is nothing to start or search, so only Settings is offered.
    static func layout(wide: Bool, hasMachines: Bool) -> RosterBar {
        switch (wide, hasMachines) {
        case (true, true):
            // Nothing in the sidebar's bar but its search field: New session is each machine's
            // "+" in the list (and ⌘T), Settings and the sidebar toggle are the detail column's
            // (root.md), so the sidebar has no bottom bar either.
            RosterBar()
        case (true, false):
            RosterBar()
        case (false, true):
            // A phone: New session, the prominent action that picks the machine in the sheet,
            // then Settings, search at the foot as iOS puts it. Each machine's "+" in the list
            // starts on that machine. Folded, a Duo stands them in its vertical bar (D13).
            RosterBar(topTrailing: [.newSession, .settings], bottom: [.search])
        case (false, false):
            RosterBar(topTrailing: [.settings])
        }
    }

    var all: [RosterBarItem] { topTrailing + bottom }
}

/// The rows after the last machine: Add machine always, Archive only while it holds sessions.
enum RosterListEnd: Hashable, Sendable {
    case addMachine
    case archive(Int)

    static func rows(archived: Int) -> [RosterListEnd] {
        archived > 0 ? [.addMachine, .archive(archived)] : [.addMachine]
    }
}

/// The one primary action: snow glass, ink glyph. Neutral, because the accent is kept for what
/// needs the user.
struct RosterNewSessionButton: View {
    let store: AppStore

    var body: some View {
        Button(RosterCopy.newSession, systemImage: "plus") { store.startNewSession() }
            .buttonStyle(.glassProminent)
            .tint(Color.togoSnow)
            .foregroundStyle(Color.togoInk)
            .disabled(store.services.hosts.isEmpty)
            .accessibilityIdentifier("roster.newSession")
    }
}

/// Settings, a tap away rather than inside a menu: in the roster's bar in a compact window, in
/// the detail column's bar beside the sidebar (root.md). Once per layout either way.
struct RosterSettingsButton: View {
    let store: AppStore

    var body: some View {
        Button(RosterCopy.settings, systemImage: "gearshape") { store.showSettings() }
            .foregroundStyle(Color.togoSnow)
            .accessibilityIdentifier("roster.settings")
    }
}

/// Settings as the detail column's last bar item. The rarest item there, so the first to go to
/// the overflow when the bar runs out of room (D14).
struct SettingsToolbarItem: ToolbarContent {
    let store: AppStore

    var body: some ToolbarContent {
        if #available(iOS 27.0, *) {
            item.visibilityPriority(.low)
        } else {
            item
        }
    }

    private var item: some ToolbarContent {
        ToolbarItem(placement: .primaryAction) { RosterSettingsButton(store: store) }
    }
}

/// The list's first row on every layout: which sessions it shows, then the tally ("All · 5
/// idle"), and the menu that changes it. "Needs you" stands out the way it does on a line, by
/// its status colour and weight, with the lit dot before it.
struct RosterFilterHeader: View {
    @Bindable var store: AppStore
    let roster: RosterModel
    /// Marks the leading edge, where the launch mark shrinks to on its way out.
    var launchTarget = false

    /// The headline's own height, as laid out.
    @State private var wordsHeight: CGFloat = 0

    /// The button's touch height.
    static let target: CGFloat = 44
    /// How much of the target the row gives back above and below, so the first machine follows
    /// close: only the blank part of it, never the words. One line keeps the row 28 points
    /// tall; a headline that wraps (a narrow sidebar, large text) takes the room it needs
    /// instead of running under the machine's heading.
    static func overlap(words: CGFloat) -> CGFloat {
        min(8, max(0, (target - words) / 2))
    }

    var body: some View {
        Menu {
            Picker(selection: $store.filter) {
                ForEach(SessionFilter.allCases, id: \.self) { filter in
                    Text(ControlCenterCopy.filterName(filter)).tag(filter)
                }
            } label: {
                Text(RosterCopy.show)
            }
            .pickerStyle(.inline)
        } label: {
            RosterHeadline(roster: roster, filter: store.filter, launchTarget: launchTarget)
                .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { wordsHeight = $0 }
                .frame(minHeight: Self.target)
                .contentShape(.rect)
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
        .padding(.vertical, -Self.overlap(words: wordsHeight))
        .accessibilityLabel(RosterCopy.show)
        .accessibilityValue(RosterHeadline.spoken(roster: roster, filter: store.filter))
        .accessibilityIdentifier("roster.filter")
    }
}

/// Add machine and Archive, after the last machine, as plain rows.
struct RosterListEndRow: View {
    let row: RosterListEnd
    let store: AppStore

    var body: some View {
        switch row {
        case .addMachine:
            Button { store.showSheet(.addMachine) } label: {
                Label(RosterCopy.addMachine, systemImage: "plus.circle")
                    .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                    .contentShape(.rect)
            }
            .accessibilityIdentifier("roster.addMachine")
        case .archive(let count):
            Button { store.showArchive() } label: {
                Label {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Text(RosterCopy.archive)
                        Text(verbatim: "\(count)")
                            .font(.system(.caption, design: .monospaced))
                            .monospacedDigit()
                            .foregroundStyle(Color.togoMute)
                    }
                } icon: {
                    Image(systemName: "archivebox")
                }
                .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                .contentShape(.rect)
            }
            .accessibilityValue(Text(verbatim: "\(count)"))
            .accessibilityIdentifier("roster.archive")
        }
    }
}

/// The sidebar column's header on regular width, in place of its navigation bar (roster.md):
/// the search field, pinned, its 44-point row level with the detail column's tab rail. Where
/// the split view's toggle would have stood in that bar (a horizontal detail bar: iPad, the
/// upright Duo), "Hide Sidebar" leads the row; beside a vertical bar the dock has it.
struct RosterSidebarHeader: View {
    @Bindable var store: AppStore
    var search: Bool
    var toggle: Bool
    var focused: FocusState<Bool>.Binding
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        HStack(spacing: 8) {
            if toggle {
                Button(AppCommandCopy.hideSidebar, systemImage: "sidebar.leading") {
                    // motion: cause → effect — the column slides out toward the edge it lives on.
                    withAnimation(Motion.animation(.sidebar, reduce: reduceMotion)) { store.toggleSidebar() }
                }
                .labelStyle(.iconOnly)
                .buttonStyle(.glass)
                .buttonBorderShape(.circle)
                .frame(minWidth: 44, minHeight: 44)
                .accessibilityIdentifier("sidebar.hide")
            }
            if search {
                RosterSearchField(query: $store.query, focused: focused)
            } else {
                Spacer(minLength: 0)
            }
        }
        .padding(.horizontal, 4)
    }
}

/// A search field drawn as the system's: magnifier, "Search", a clear button once there is
/// text, on the system's search fill. The same `query` the phone's `.searchable` binds, so the
/// list narrows the same way. ⌘K focuses it (RosterView); Esc clears it, then leaves it.
struct RosterSearchField: View {
    @Binding var query: String
    var focused: FocusState<Bool>.Binding

    var body: some View {
        HStack(spacing: 6) {
            Image(systemName: "magnifyingglass")
                .foregroundStyle(.secondary)
                .accessibilityHidden(true)
            TextField(RosterCopy.searchPrompt, text: $query)
                .focused(focused)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .submitLabel(.search)
                .accessibilityLabel(RosterCopy.searchPrompt)
                .accessibilityAddTraits(.isSearchField)
                .accessibilityIdentifier("roster.search")
            if !query.isEmpty {
                Button(RosterCopy.clearSearch, systemImage: "xmark.circle.fill") { query = "" }
                    .labelStyle(.iconOnly)
                    .buttonStyle(.borderless)
                    .foregroundStyle(.secondary)
                    .frame(minWidth: 28, minHeight: 44)
                    .contentShape(.rect)
            }
        }
        .font(.body)
        .padding(.horizontal, 12)
        .frame(minHeight: 36)
        .background(.fill.tertiary, in: .capsule)
        .frame(maxWidth: .infinity, minHeight: 44)
        .contentShape(.rect)
        .onTapGesture { focused.wrappedValue = true }
        .onKeyPress(.escape) {
            if query.isEmpty {
                focused.wrappedValue = false
            } else {
                query = ""
            }
            return .handled
        }
    }
}
