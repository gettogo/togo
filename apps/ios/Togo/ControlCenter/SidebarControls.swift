import SwiftUI

/// The session filter as a toolbar menu beside the sidebar, in place of a row in the list. The
/// symbol fills while the list is narrowed to some sessions.
struct SidebarFilterMenu: View {
    @Bindable var store: AppStore

    var body: some View {
        Menu {
            Picker(ControlCenterCopy.filter, selection: $store.filter) {
                ForEach(SessionFilter.allCases, id: \.self) { filter in
                    Text(ControlCenterCopy.filterName(filter)).tag(filter)
                }
            }
            .pickerStyle(.inline)
        } label: {
            Label(
                SidebarCopy.filter,
                systemImage: store.filter == .all ? "line.3.horizontal.decrease" : "line.3.horizontal.decrease.circle.fill"
            )
        }
        .accessibilityValue(ControlCenterCopy.filterName(store.filter))
    }
}

/// What a machine's menu in the sidebar offers, whatever state the machine is in.
struct MachineMenu: View {
    let machine: MachineSection
    let perform: (SessionCommand) -> Void

    var body: some View {
        Menu {
            Button(SidebarCopy.newSessionHere, systemImage: "plus") { perform(.newSession(machine.id)) }
                .disabled(machine.reachability != .online)
            Button(SidebarCopy.installHelper, systemImage: "arrow.down.circle") { perform(.installHelper(machine.id)) }
                .disabled(machine.reachability != .online)
            Button(SidebarCopy.signInAgain, systemImage: "person.badge.key") { perform(.signIn(machine.id)) }
            Button(SidebarCopy.editMachine, systemImage: "pencil") { perform(.edit(machine.id)) }
        } label: {
            Label(SidebarCopy.machineMenu(machine.name), systemImage: "ellipsis.circle")
                .labelStyle(.iconOnly)
                .font(.body)
                .foregroundStyle(Color.togoMute)
                .frame(minWidth: 44, minHeight: 44)
                .contentShape(Rectangle())
        }
    }
}

/// Starts a session on this machine, beside the machine's menu in its heading.
struct MachineNewSessionButton: View {
    let machine: MachineSection
    let perform: (SessionCommand) -> Void

    var body: some View {
        Button { perform(.newSession(machine.id)) } label: {
            Image(systemName: "plus")
                .font(.body)
                .foregroundStyle(Color.togoMute)
                .frame(minWidth: 44, minHeight: 44)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .hoverEffect(.highlight)
        .disabled(machine.reachability != .online)
        .accessibilityLabel(SidebarCopy.newSessionOn(machine.name))
        .accessibilityIdentifier("machine.newSession")
    }
}

/// A computer with a plus, so adding a machine never reads as starting a session.
struct AddMachineSymbol: View {
    var body: some View {
        Image(systemName: "desktopcomputer")
            .padding(.trailing, 4)
            .overlay(alignment: .bottomTrailing) {
                Image(systemName: "plus.circle.fill")
                    .font(.system(size: 10, weight: .bold))
                    .symbolRenderingMode(.palette)
                    .foregroundStyle(Color.togoInk, Color.togoSnow)
                    .offset(x: 5, y: 4)
            }
            .accessibilityHidden(true)
    }
}

enum SidebarCopy {
    static let noSessions = String(localized: "sidebar.machine.noSessions", defaultValue: "No sessions")
    static func newSessionOn(_ name: String) -> String {
        String(localized: "sidebar.machine.newSessionOn", defaultValue: "New session on \(name)")
    }
    static let filter = String(localized: "sidebar.filter", defaultValue: "Filter")
    static let addMachine = String(localized: "sidebar.addMachine", defaultValue: "Add machine")
    static let newSessionHere = String(localized: "sidebar.machine.newSession", defaultValue: "New session here")
    static let installHelper = String(localized: "sidebar.machine.installHelper", defaultValue: "Install or update helper")
    static let signInAgain = String(localized: "sidebar.machine.signIn", defaultValue: "Sign in again")
    static let editMachine = String(localized: "sidebar.machine.edit", defaultValue: "Edit machine")
    static func machineMenu(_ name: String) -> String {
        String(localized: "sidebar.machine.menu", defaultValue: "More for \(name)")
    }
}
