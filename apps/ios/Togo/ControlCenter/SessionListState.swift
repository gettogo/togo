import SwiftUI
import TogoKit

/// What the session list shows, shared by the compact list and the sidebar so a filter or a
/// search reads the same in both.
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

    /// Requests are what "Needs you" means, so the other filters hide them.
    var listRequests: [RequestCard] {
        guard filter == .all || filter == .needsYou else { return [] }
        let needle = query.trimmingCharacters(in: .whitespacesAndNewlines)
        var seen = Set<String>()
        return controlCenter.requests.filter { card in
            needle.isEmpty || [card.title, card.agentName, card.bib ?? ""]
                .contains { $0.localizedStandardContains(needle) }
        }
        .filter { seen.insert($0.id).inserted }
    }

    /// Every row of the sidebar that carries an identity, in list order. The list keys its
    /// rows by these; a repeat, even across sections, makes its batch update inconsistent.
    var sidebarRowIDs: [SidebarRowID] {
        let narrowed = listNarrowed
        return [.board] + listRequests.map { .card($0.id) } + listMachines.flatMap { machine in
            let restorable = !narrowed && machine.reachability == .online ? machine.restorable : []
            return machine.sessions.map { SidebarRowID.session(TabID(hostID: machine.id, sessionID: $0.id)) }
                + restorable.map { SidebarRowID.restorable(TabID(hostID: machine.id, sessionID: $0.id)) }
        }
    }

    /// The sidebar's selection, only while its row is in the list: the focused tab can be a
    /// session the filter hides, an archived one or one the machine has not listed yet, and
    /// the list must not be asked to select a row it does not have.
    var listedSidebarSelection: SidebarItem? {
        switch sidebarSelection {
        case .board: .board
        case .session(let id)? where sidebarRowIDs.contains(.session(id)): .session(id)
        case .session, nil: nil
        }
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

    /// The archive row, under an unnarrowed list with finished sessions in it.
    var archiveShown: Bool {
        !listNarrowed && !controlCenter.archived.isEmpty
    }

    func showArchive() {
        path = [.archive]
    }

    func etAvailable(_ id: HostID) -> Bool {
        services.hosts.first { $0.id == id }?.etAvailable ?? false
    }
}

/// The identity of a sidebar row that has one.
enum SidebarRowID: Hashable, Sendable {
    case board
    case card(String)
    case session(TabID)
    case restorable(TabID)
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

/// Takes answered cards away once the list has shown their outcome. Every list that shows
/// cards carries it; in a regular window the sidebar is the only one on screen.
struct RequestCollapse: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let store: AppStore

    /// How long an answered card shows its outcome before it collapses.
    static let outcomeHold = Duration.milliseconds(400)

    func body(content: Content) -> some View {
        content.task(id: store.answeredCardIDs) {
            let ids = store.answeredCardIDs
            guard await store.holdAnswered(ids, hold: Self.outcomeHold) else { return }
            withAnimation(Motion.animation(.collapse, reduce: reduceMotion)) {
                store.dismissAnswered(ids)
            }
        }
    }
}

/// The filter, as segments or, at accessibility sizes where segments cannot grow, a menu.
struct SessionFilterPicker: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Bindable var store: AppStore

    var body: some View {
        let picker = Picker(ControlCenterCopy.filter, selection: $store.filter) {
            ForEach(SessionFilter.allCases, id: \.self) { filter in
                Text(ControlCenterCopy.filterName(filter)).tag(filter)
            }
        }
        if dynamicTypeSize.isAccessibilitySize {
            picker
                .pickerStyle(.menu)
                .foregroundStyle(Color.togoSnow)
                .listRowBackground(Color.togoInk)
        } else {
            picker
                .pickerStyle(.segmented)
                .listRowInsets(EdgeInsets())
                .listRowBackground(Color.clear)
        }
    }
}

/// The row that opens the archive of finished sessions.
struct ArchiveRow: View {
    let count: Int

    var body: some View {
        LabeledContent {
            Text(ControlCenterCopy.finishedSessions(count))
                .foregroundStyle(Color.togoMute)
        } label: {
            Text(ControlCenterCopy.archive)
                .foregroundStyle(Color.togoSnow)
        }
        .frame(minHeight: 44)
    }
}

/// The archive as a screen of its own: rows open in the workspace, beside the sidebar or in
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
