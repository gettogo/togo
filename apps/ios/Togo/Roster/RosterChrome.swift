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
            // The split view's own toggle sits beside New session. Settings is in the detail
            // column's bar (root.md), so the sidebar has no bottom bar.
            RosterBar(topTrailing: [.newSession])
        case (true, false):
            RosterBar()
        case (false, true):
            // A phone: New session, the prominent action, then Settings, search at the foot as
            // iOS puts it. Folded, a Duo stands them in its vertical bar in that order (D13).
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
        }
        .menuStyle(.button)
        .buttonStyle(.plain)
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
            }
            .accessibilityValue(Text(verbatim: "\(count)"))
            .accessibilityIdentifier("roster.archive")
        }
    }
}
