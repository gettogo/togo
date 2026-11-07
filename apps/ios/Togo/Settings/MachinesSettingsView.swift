import SwiftUI
import UIKit

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

/// What the machines page needs to offer importing from an SSH config.
struct MachineImport {
    var make: () -> ImportHostsModel
    /// Opens the add-machine form filled in from one imported host.
    var openInForm: (MachineDraft) -> Void
    var finished: (Int) -> Void
}

struct MachinesSettingsView: View {
    let machines: [SettingsMachine]
    let add: () -> Void
    let edit: (HostID) -> Void
    let remove: (HostID) -> Void
    /// Removed machines still to be told to stop pushing to this iPhone.
    var removing: [PendingRemoval] = []
    var forgetRemoval: (String) -> Void = { _ in }
    var machineImport: MachineImport?
    /// The remove question names the kind of device whose key goes.
    var isPad = UIDevice.current.userInterfaceIdiom == .pad
    @State private var removal = MachineRemoval()
    @State private var importModel: ImportHostsModel?
    @State private var draftToOpen: MachineDraft?

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
        .toolbar {
            if let machineImport {
                ToolbarItem(placement: .primaryAction) {
                    Button(settingsText("import.hosts.title", "Import from SSH config"), systemImage: "square.and.arrow.down") {
                        importModel = machineImport.make()
                    }
                }
            }
        }
        .sheet(item: $importModel, onDismiss: {
            if let draft = draftToOpen {
                draftToOpen = nil
                machineImport?.openInForm(draft)
            }
        }) { model in
            NavigationStack {
                ImportHostsView(
                    model: model,
                    openInForm: { host in
                        draftToOpen = MachineDraft(imported: host)
                        importModel = nil
                    },
                    finish: { machineImport?.finished($0) }
                )
            }
            .togoControls()
            .togoSheet(.form)
        }
        .confirmationDialog(
            settingsText("settings.machines.remove.title", "Remove \(removal.pending?.name ?? "")?"),
            isPresented: Binding { removal.pending != nil } set: { if !$0 { removal.cancel() } },
            titleVisibility: .visible,
            presenting: removal.pending
        ) { machine in
            Button(settingsText("settings.machines.remove.confirm", "Remove \(machine.name)"), role: .destructive) {
                removal.confirm(remove)
            }
        } message: { machine in
            Text(machine.removesKey
                ? (isPad
                    ? settingsText(
                        "settings.machines.remove.key.message.ipad",
                        "Togo removes this iPad's key from \(machine.name) so it can no longer sign in, then forgets the machine."
                    )
                    : settingsText(
                        "settings.machines.remove.key.message.iphone",
                        "Togo removes this iPhone's key from \(machine.name) so it can no longer sign in, then forgets the machine."
                    ))
                : settingsText(
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
                    // No destructive role: it would take the row away before the question is
                    // answered, and the list would count one row short from then on.
                    .swipeActions {
                        Button(settingsText("settings.machines.remove", "Remove")) {
                            removal.request(machine)
                        }
                        .tint(.red)
                    }
                    .contextMenu {
                        if case .outdated = machine.helper {
                            // Editing connects again and walks through installing the new helper.
                            Button(settingsText("settings.machines.updateHelper", "Update helper"), systemImage: "arrow.down.circle") {
                                edit(machine.id)
                            }
                        }
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
            let unpairing = removing.filter { $0.kind == .unpair }
            if !unpairing.isEmpty {
                PendingRemovalsSection(removals: unpairing, forget: forgetRemoval)
            }
            ForEach(removing.filter { $0.kind != .unpair }) { removal in
                PendingKeySection(removal: removal, forget: forgetRemoval)
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

#if DEBUG
extension MachinesSettingsView {
    /// The page with the remove question already up, for snapshots.
    init(machines: [SettingsMachine], isPad: Bool, asking machine: SettingsMachine) {
        self.init(machines: machines, add: {}, edit: { _ in }, remove: { _ in }, isPad: isPad)
        var removal = MachineRemoval()
        removal.request(machine)
        _removal = State(initialValue: removal)
    }
}
#endif

extension PendingRemoval {
    /// What the user can do about a key still on a removed machine; nil for an unpair.
    var footer: String? {
        guard case .key(let comment, let manual) = kind else { return nil }
        return manual
            ? settingsText(
                "settings.machines.removeKey.manual",
                "Remove the line ending in \(comment) from ~/.ssh/authorized_keys on \(name)."
            )
            : settingsText(
                "settings.machines.removeKey.later", "If \(name) can't be reached, run togo devices revoke on it later."
            )
    }
}

/// A removed machine that may still hold this device's key, with what to do about it below.
private struct PendingKeySection: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let removal: PendingRemoval
    let forget: (String) -> Void

    var body: some View {
        Section {
            let layout = dynamicTypeSize.isAccessibilitySize
                ? AnyLayout(VStackLayout(alignment: .leading, spacing: 8))
                : AnyLayout(HStackLayout(alignment: .center, spacing: 12))
            layout {
                VStack(alignment: .leading, spacing: 2) {
                    Text(settingsText("settings.machines.removeKey", "Remove key"))
                        .foregroundStyle(Color.togoSnow)
                    Text(removal.name)
                        .font(.subheadline)
                        .foregroundStyle(Color.togoMute)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityElement(children: .combine)
                Button(settingsText("settings.machines.forgetNow", "Forget now")) { forget(removal.id) }
                    .font(.subheadline)
                    .buttonStyle(.borderless)
            }
            .padding(.vertical, 4)
            .frame(minHeight: 44)
        } footer: {
            if let footer = removal.footer {
                Text(footer)
            }
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
