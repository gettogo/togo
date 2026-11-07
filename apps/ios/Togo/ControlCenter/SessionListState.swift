import SwiftUI
import TogoKit

/// What the roster lists, whatever the window's size.
extension AppStore {
    /// Search or a filter is narrowing the list.
    var listNarrowed: Bool {
        !query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || filter != .all
    }

    /// Machines in the order the user added them. One that has not reported yet shows as
    /// connecting, so the list has its final shape from the first frame.
    var listMachines: [MachineSection] {
        let hosts = services.hosts
        var sections = visibleMachines
        if !listNarrowed {
            for host in hosts where !sections.contains(where: { $0.id == host.id }) {
                sections.append(MachineSection(
                    id: host.id, name: host.name, symbol: host.symbol, reachability: .connecting,
                    helper: .checking, sessions: [], lastSeen: nil
                ))
            }
        }
        let order = Dictionary(hosts.enumerated().map { ($1.id, $0) }, uniquingKeysWith: { first, _ in first })
        var seen = Set<HostID>()
        return sections
            .filter { seen.insert($0.id).inserted }
            .map { section in
                // The heading reads the machine's record, so a rename shows at once.
                var section = section.listed
                if let host = hosts.first(where: { $0.id == section.id }) {
                    section.name = host.name
                    section.symbol = host.symbol
                }
                return section
            }
            .sorted { (order[$0.id] ?? .max) < (order[$1.id] ?? .max) }
    }

    var answeredCardIDs: Set<String> {
        Set(controlCenter.requests.filter { card in
            if case .done = card.phase { true } else { false }
        }.map(\.id))
    }

    /// Answered cards show their outcome for `hold`, then leave.
    func collapseAnswered(_ ids: Set<String>, hold: Duration) async {
        if await holdAnswered(ids, hold: hold) { dismissAnswered(ids) }
    }

    /// Waits out the outcome; false when there is nothing to collapse or the wait was cancelled.
    func holdAnswered(_ ids: Set<String>, hold: Duration) async -> Bool {
        guard !ids.isEmpty else { return false }
        try? await Task.sleep(for: hold)
        return !Task.isCancelled
    }

    func showArchive() {
        path = [.archive]
    }

    func etAvailable(_ id: HostID) -> Bool {
        services.hosts.first { $0.id == id }?.etAvailable ?? false
    }
}

extension MachineSection {
    /// Each session once, and a restorable session only while no live row has its id (a
    /// restore can land in a snapshot before the restorable list drops it).
    var listed: MachineSection {
        var section = self
        var seen = Set<String>()
        section.sessions = sessions.filter { seen.insert($0.id).inserted }
        section.restorable = restorable.filter { seen.insert($0.id).inserted }
        return section
    }
}

/// Takes answered requests away once the bar has shown their outcome. The roster carries it,
/// so it runs whichever window size shows the bar.
struct RequestCollapse: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let store: AppStore

    /// How long an answered card shows its outcome before it collapses.
    static let outcomeHold = Duration.milliseconds(900)

    func body(content: Content) -> some View {
        content.task(id: store.answeredCardIDs) {
            let ids = store.answeredCardIDs
            guard await store.holdAnswered(ids, hold: Self.outcomeHold) else { return }
            // The hold is a model-level wait; the visual change is a choreography: the bar
            // conceals the answered card before the next stage rises (`PromptBar.move`), and
            // everything else that moves with it (a roster line's word, the bar's height)
            // travels on `resize`.
            // motion: preventing a jump — the answered card leaves and the rows close the gap on one transaction.
            withAnimation(Motion.animation(.resize, reduce: reduceMotion)) {
                store.dismissAnswered(ids)
            }
        }
    }
}

/// The archive as a screen of its own: rows open in the workspace, beside the roster or in
/// the cover.
struct ArchiveScreen: View {
    @Environment(\.machineTools) private var machineTools
    let store: AppStore

    var body: some View {
        ArchiveView(rows: store.controlCenter.archived, machines: store.listMachines) { command in
            if case .open(let row, let transport) = command, store.workspaceInline {
                store.showFromSidebar(row, transport: transport)
            } else {
                store.run(command, tools: machineTools, open: { row, transport in
                    store.open(hostID: row.hostID, sessionID: row.id, transport: transport)
                }, kill: { _ in }, rename: { _ in })
            }
        }
    }
}
