import SwiftUI

/// Removing a machine always goes through a question first. `confirm` acts on the machine the
/// user was asked about, once, so a stale dialog can never remove a second time.
struct MachineRemoval: Equatable {
    private(set) var pending: SettingsMachine?

    mutating func request(_ machine: SettingsMachine) {
        pending = machine
    }

    mutating func confirm(_ remove: (HostID) -> Void) {
        guard let machine = pending else { return }
        pending = nil
        remove(machine.id)
    }

    mutating func cancel() {
        pending = nil
    }
}

struct MachinesSettingsView: View {
    let machines: [SettingsMachine]
    let add: () -> Void
    let edit: (HostID) -> Void
    let remove: (HostID) -> Void
    @State private var removal = MachineRemoval()

    var body: some View {
        Group {
            if machines.isEmpty {
                StateView(
                    kind: .empty,
                    title: settingsText("settings.machines.empty.title", "No machines yet"),
                    message: settingsText("settings.machines.empty.message", "Add a Mac or Linux machine to manage it here."),
                    action: .init(settingsText("settings.machines.add", "Add a machine"), run: add)
                )
                .background(Color.togoInk)
            } else {
                list
            }
        }
        .navigationTitle(settingsText("settings.machines.title", "Machines"))
        .navigationBarTitleDisplayMode(.inline)
        .confirmationDialog(
            settingsText("settings.machines.remove.title", "Remove \(removal.pending?.name ?? "")?"),
            isPresented: Binding { removal.pending != nil } set: { if !$0 { removal.cancel() } },
            titleVisibility: .visible,
            presenting: removal.pending
        ) { machine in
            Button(settingsText("settings.machines.remove.confirm", "Remove \(machine.name)"), role: .destructive) {
                removal.confirm(remove)
            }
        } message: { _ in
            Text(settingsText(
                "settings.machines.remove.message",
                "Its sessions keep running on the machine. You can add it again at any time."
            ))
        }
    }

    private var list: some View {
        List {
            Section {
                ForEach(machines) { machine in
                    Button {
                        edit(machine.id)
                    } label: {
                        MachineRow(machine: machine)
                    }
                    .swipeActions {
                        Button(settingsText("settings.machines.remove", "Remove"), role: .destructive) {
                            removal.request(machine)
                        }
                    }
                    .contextMenu {
                        Button(settingsText("settings.machines.edit", "Edit"), systemImage: "pencil") { edit(machine.id) }
                        Button(settingsText("settings.machines.remove", "Remove"), systemImage: "trash", role: .destructive) {
                            removal.request(machine)
                        }
                    }
                    .accessibilityAction(named: settingsText("settings.machines.remove", "Remove")) {
                        removal.request(machine)
                    }
                }
                Button(action: add) {
                    Label(settingsText("settings.machines.add", "Add a machine"), systemImage: "plus")
                }
                .frame(minHeight: 44)
            } footer: {
                Text(settingsText("settings.machines.footer", "Tap a machine to edit it. Swipe left to remove it."))
            }
            .groupedRows()
        }
        .groupedList()
    }
}

private struct MachineRow: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let machine: SettingsMachine

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 14) {
            if !dynamicTypeSize.isAccessibilitySize {
                Image(systemName: machine.symbol.systemImage)
                    .foregroundStyle(Color.togoMute)
                    .frame(width: 28)
                    .accessibilityHidden(true)
            }
            VStack(alignment: .leading, spacing: 2) {
                Text(machine.name)
                    .foregroundStyle(Color.togoSnow)
                Text(machine.address)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Text(machine.helper.settingsDescription)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, 4)
        .frame(minHeight: 44)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
        .accessibilityHint(settingsText("settings.machines.row.hint", "Opens the machine's details"))
    }
}
