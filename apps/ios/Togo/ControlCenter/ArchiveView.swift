import SwiftUI
import TogoKit

/// Sessions that finished more than a few hours ago, newest first, grouped by machine.
struct ArchiveView: View {
    let rows: [SessionRow]
    let machines: [MachineSection]
    let perform: (SessionCommand) -> Void

    var body: some View {
        List {
            ForEach(groups, id: \.machine.id) { group in
                Section {
                    ForEach(group.rows) { row in
                        Button {
                            perform(.open(row, nil))
                        } label: {
                            SessionRowView(row: row)
                        }
                        .buttonStyle(.plain)
                        .hoverEffect(.highlight)
                        .listRowBackground(Color.togoInk)
                        .contextMenu {
                            Button(SessionAction.open.title(for: row), systemImage: SessionAction.open.systemImage(for: row)) {
                                perform(.open(row, nil))
                            }
                            Button(
                                SessionAction.copyPath.title(for: row),
                                systemImage: SessionAction.copyPath.systemImage(for: row)
                            ) { perform(.copyPath(row)) }
                        }
                    }
                } header: {
                    MachineHeading(machine: group.machine)
                }
            }
        }
        .listStyle(.insetGrouped)
        .listRowSeparatorTint(Color.togoLine)
        .scrollContentBackground(.hidden)
        .background(Color.togoInk)
        .navigationTitle(ControlCenterCopy.archive)
        .navigationBarTitleDisplayMode(.inline)
    }

    /// Machines keep the control center's order; rows keep the archive's newest-first order.
    private var groups: [(machine: MachineSection, rows: [SessionRow])] {
        machines.compactMap { machine in
            let rows = rows.filter { $0.hostID == machine.id }
            return rows.isEmpty ? nil : (machine, rows)
        }
    }
}
