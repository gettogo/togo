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
    /// Removed machines still to be told to stop pushing to this iPhone.
    var removing: [PendingRemoval] = []
    var forgetRemoval: (String) -> Void = { _ in }
    @State private var removal = MachineRemoval()

    var body: some View {
        Group {
            if machines.isEmpty && removing.isEmpty {
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
            if !removing.isEmpty {
                PendingRemovalsSection(removals: removing, forget: forgetRemoval)
            }
        }
        .groupedList()
    }
}

/// Removed machines that have not yet been told to stop sending notifications to this iPhone.
private struct PendingRemovalsSection: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let removals: [PendingRemoval]
    let forget: (String) -> Void

    var body: some View {
        Section {
            ForEach(removals) { removal in
                // At accessibility sizes the button goes under the text so neither breaks mid-word.
                let layout = dynamicTypeSize.isAccessibilitySize
                    ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8))
                    : AnyLayout(HStackLayout(alignment: .firstTextBaseline, spacing: 12))
                layout {
                    Text(settingsText(
                        "settings.machines.removing", "Removing \(removal.name). Finishes next time it's reachable."
                    ))
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .fixedSize(horizontal: false, vertical: true)
                    Button(settingsText("settings.machines.forgetNow", "Forget now")) { forget(removal.id) }
                        .font(.subheadline)
                        .buttonStyle(.borderless)
                }
                .padding(.vertical, 4)
                .frame(minHeight: 44)
            }
        } footer: {
            Text(settingsText(
                "settings.machines.forgetNow.footer",
                "After Forget now, notifications may continue until you run togo devices revoke on that machine."
            ))
        }
        .groupedRows()
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
