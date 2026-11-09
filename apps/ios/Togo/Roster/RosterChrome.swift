import SwiftUI

/// The roster's bar items, shared by the phone's bars, the sidebar column's bars and the empty
/// detail beside it. Each is a symbol with a title, so an iPhone Duo can stand it in its vertical
/// bar and fall back to the title in the overflow menu.

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

/// Settings, a tap away rather than inside the menu.
struct RosterSettingsButton: View {
    let store: AppStore

    var body: some View {
        Button(RosterCopy.settings, systemImage: "gearshape") { store.showSettings() }
            .foregroundStyle(Color.togoSnow)
            .accessibilityIdentifier("roster.settings")
    }
}

/// Which sessions the list shows. The glyph fills while a filter hides some, as the row at the
/// top of the list says too.
struct RosterFilterMenu: View {
    @Bindable var store: AppStore

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
            Label(
                RosterCopy.show,
                systemImage: store.filter == .all ? "line.3.horizontal.decrease" : "line.3.horizontal.decrease.circle.fill"
            )
        }
        .foregroundStyle(Color.togoSnow)
        .accessibilityValue(ControlCenterCopy.filterName(store.filter))
        .accessibilityIdentifier("roster.filter")
    }
}

/// The rarer things: the status display, adding a machine, the archive. On iOS 27 they are the
/// items of the bar's system overflow menu.
struct RosterRareActions: View {
    let store: AppStore

    var body: some View {
        Button { store.toggleMonitor() } label: {
            Label {
                Text(MonitorCopy.menuTitle)
                Text(MonitorCopy.menuSubtitle)
            } icon: {
                Image(systemName: MonitorCopy.symbol)
            }
        }
        Divider()
        Button(RosterCopy.addMachine, systemImage: "desktopcomputer") { store.showSheet(.addMachine) }
        Button(RosterCopy.archive, systemImage: "archivebox") { store.showArchive() }
            .disabled(store.controlCenter.archived.isEmpty)
    }
}

/// Before iOS 27, which has no system overflow menu for a bar: the same items in a menu that
/// looks like one.
struct RosterMoreMenu: View {
    let store: AppStore

    var body: some View {
        Menu {
            RosterRareActions(store: store)
        } label: {
            Label(RosterCopy.more, systemImage: "ellipsis")
        }
        .foregroundStyle(Color.togoSnow)
        .accessibilityIdentifier("roster.more")
    }
}
