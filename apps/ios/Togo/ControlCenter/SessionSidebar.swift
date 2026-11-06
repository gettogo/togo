import SwiftUI
import TogoKit
import UIKit

/// The sessions beside the workspace in a regular-width window: questions waiting on the user,
/// answerable in place, then every machine's sessions. Picking a row shows it in the focused
/// pane; the rows of both visible tabs stay highlighted.
///
/// From the keyboard, the arrows move a cursor through the rows, Return opens the row under
/// it, ⌘⌫ asks to end that session and Escape hands focus back to the terminal.
struct SessionSidebar: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.machineTools) private var machineTools
    @Environment(\.supportsMultipleWindows) private var supportsMultipleWindows
    @Bindable var store: AppStore
    /// For Escape, which puts keyboard focus back in the focused pane's terminal.
    var terminals: TerminalPool?
    /// The new-session sheet grows out of the plus button in this namespace.
    var zoom: Namespace.ID?
    /// A fixed clock for previews and snapshots; nil follows the time of day.
    var now: Date?

    @State private var killTarget: SessionRow?
    @State private var renameTarget: SessionRow?
    @State private var keys = SidebarKeyboard()
    @State private var searching = false
    @FocusState private var searchFocused: Bool
    @FocusState private var listFocused: Bool
    /// Stands in for `zoom` where no presenter shares one, as in previews.
    @Namespace private var ownZoom

    init(store: AppStore, terminals: TerminalPool? = nil, zoom: Namespace.ID? = nil, now: Date? = nil) {
        self.store = store
        self.terminals = terminals
        self.zoom = zoom
        self.now = now
    }

    var body: some View {
        // No fill of its own: the sidebar list draws the floating glass pane over the detail.
        TimelineView(.everyMinute) { context in
            list(now: now ?? context.date)
        }
        .navigationTitle(ControlCenterCopy.title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar { toolbar }
        .modifier(SessionDialogs(store: store, kill: $killTarget, rename: $renameTarget))
        .modifier(RequestCollapse(store: store))
        .onChange(of: store.sidebar.searchRequests) {
            keys.searchRequested()
            searching = true
            searchFocused = true
        }
        // The search field takes Escape itself and closes; after ⌘K that hands focus back to
        // the terminal, as Escape does from the list.
        .onChange(of: searching) { _, open in
            if !open, keys.searchClosed() { _ = returnToTerminal() }
        }
    }

    // MARK: List

    private func list(now: Date) -> some View {
        ScrollViewReader { scroller in
            List(selection: selection) {
                if let notice = store.notice {
                    Section {
                        Label(notice.text, systemImage: notice.isError ? "exclamationmark.circle" : "info.circle")
                            .font(.subheadline)
                            .foregroundStyle(Color.togoSnow)
                    }
                }
                Section {
                    Label(BoardCopy.title, systemImage: "square.grid.2x2")
                        .tag(SidebarItem.board)
                }
                requests
                ForEach(machines) { machine in
                    MachineSectionView(
                        machine: machine, etAvailable: etAvailable(machine.id), now: now, narrowed: narrowed,
                        highlighted: Set(store.visibleTabs), cursor: listFocused ? keys.cursor : nil,
                        selected: selectedTab, draggable: rowsDrag,
                        restoring: store.restoring, wakeNote: store.wakeNotes[machine.id], sidebar: true,
                        perform: perform
                    )
                }
                emptyResult
                if store.archiveShown {
                    Section {
                        Button { store.showArchive() } label: {
                            ArchiveRow(count: store.controlCenter.archived.count)
                        }
                        .buttonStyle(.plain)
                        .hoverEffect(.highlight)
                    }
                }
            }
            .listStyle(.sidebar)
            // The brand's raised surface, opaque, instead of the system's grey glass.
            .scrollContentBackground(.hidden)
            .background(Color.togoRaised)
            .containerBackground(Color.togoRaised, for: .navigation)
            .overlay(alignment: .trailing) {
                Rectangle().fill(Color.togoLine).frame(width: 1).ignoresSafeArea()
            }
            // A pull to refresh never frosts the rows under the bar.
            .scrollEdgeEffectStyle(.hard, for: .top)
            // The last rows scroll clear of the floating bottom bar.
            .contentMargins(.bottom, Self.bottomBarClearance, for: .scrollContent)
            .searchable(text: $store.query, isPresented: $searching, placement: .sidebar, prompt: ControlCenterCopy.searchPrompt)
            .searchFocused($searchFocused)
            .refreshable { await store.services.refresh() }
            .task(id: store.restoreCandidates) { await store.loadRestorable(tools: machineTools) }
            .focusable()
            .focusEffectDisabled()
            .focused($listFocused)
            .onKeyPress(keys: [.upArrow, .downArrow]) { press in
                keys.move(press.key == .upArrow ? -1 : 1, through: rows.map { TabID(hostID: $0.hostID, sessionID: $0.id) })
                if let cursor = keys.cursor { scroller.scrollTo(SessionRotors.Target.row(cursor)) }
                return .handled
            }
            .onKeyPress(.return) { openCursor() }
            .onKeyPress(.delete, phases: .down) { press in
                press.modifiers.contains(.command) ? askToEndCursor() : .ignored
            }
            .onKeyPress(.escape) { returnToTerminal() }
            .sessionRotors(machines, requests: visibleRequests)
            .motion(.dropIn, value: visibleRequests.map(\.id))
        }
    }

    @ViewBuilder private var requests: some View {
        let cards = visibleRequests
        if !cards.isEmpty {
            Section {
                ForEach(cards) { card in
                    RequestCardView(
                        card: card, machineName: store.machineName(for: card.hostID),
                        failure: store.answerFailures[card.id],
                        onOpen: card.sessionID.map { sessionID in
                            { show(TabID(hostID: card.hostID, sessionID: sessionID)) }
                        },
                        surface: .clear
                    ) { decision in
                        Task { await store.answer(card.id, decision) }
                    }
                    .listRowInsets(EdgeInsets(top: 6, leading: 0, bottom: 6, trailing: 0))
                    .listRowBackground(Color.clear)
                    .listRowSeparator(.hidden)
                    .id(SessionRotors.Target.card(card.id))
                }
            } header: {
                SectionHeading(ControlCenterCopy.waitingOnYou)
                    .textCase(nil)
            }
        }
    }

    @ViewBuilder private var emptyResult: some View {
        if narrowed && machines.isEmpty && visibleRequests.isEmpty {
            let query = store.query.trimmingCharacters(in: .whitespacesAndNewlines)
            // In a section of its own: a loose row between sections leaves the list to guess
            // its section, and a filter change that adds it while machines leave crashed it.
            Section {
                Text(query.isEmpty ? ControlCenterCopy.filterEmpty(store.filter) : ControlCenterCopy.noMatches(query))
                    .font(.body)
                    .foregroundStyle(Color.togoMute)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .listRowBackground(Color.clear)
            }
        }
    }

    /// Native selection: the board row, or the focused pane's session. Picking a row flows the
    /// detail to it.
    private var selection: Binding<SidebarItem?> {
        Binding {
            store.listedSidebarSelection
        } set: { item in
            guard let item, item != store.listedSidebarSelection else { return }
            if case .session(let id) = item { keys.cursor = id }
            withAnimation(Motion.animation(.paneFlow, reduce: reduceMotion)) {
                store.sidebarSelection = item
            }
        }
    }

    /// Two items at most beside the system's sidebar button, so nothing folds into an overflow
    /// menu; the global actions sit in the bottom bar. Symbol and title on every item: vertical
    /// bars show the symbol, overflow the title.
    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        ToolbarItem(placement: .topBarTrailing) {
            SidebarFilterMenu(store: store)
        }
        if !store.services.hosts.isEmpty {
            ToolbarItem(placement: .topBarTrailing) {
                Button(ControlCenterCopy.newSession, systemImage: "plus") { store.sheet = .newSession(nil) }
                    .buttonStyle(.glassProminent)
                    .tint(Color.togoSnow)
                    .foregroundStyle(Color.togoInk)
            }
            .matchedTransitionSource(id: ControlCenterView.newSessionSourceID, in: zoomNamespace)
        }
        // Only while the sidebar shows: hidden, its bar would float over the detail instead.
        if !store.sidebar.isHidden {
            ToolbarItem(placement: .bottomBar) {
                Button(ControlCenterCopy.settings, systemImage: "gearshape") { store.showSettings() }
            }
            ToolbarSpacer(.flexible, placement: .bottomBar)
            DefaultToolbarItem(kind: .search, placement: .bottomBar)
            ToolbarSpacer(.flexible, placement: .bottomBar)
            ToolbarItem(placement: .bottomBar) {
                Button { store.showSheet(.addMachine) } label: { AddMachineSymbol() }
                .accessibilityLabel(SidebarCopy.addMachine)
            }
        }
    }

    // MARK: Derived state

    /// The height of the floating bottom bar and its margin.
    static let bottomBarClearance: CGFloat = 72

    private var zoomNamespace: Namespace.ID { zoom ?? ownZoom }

    /// A row drags into a pane or out to a window of its own, so only where one can take it.
    private var rowsDrag: Bool {
        supportsMultipleWindows || store.panesFit || store.workspace.secondary != nil
    }

    private var selectedTab: TabID? {
        if case .session(let id)? = store.listedSidebarSelection { id } else { nil }
    }

    private var narrowed: Bool { store.listNarrowed }
    private var machines: [MachineSection] { store.listMachines }
    private var visibleRequests: [RequestCard] { store.listRequests }

    private var rows: [SessionRow] {
        machines.flatMap(\.sessions)
    }

    private func etAvailable(_ id: HostID) -> Bool {
        store.etAvailable(id)
    }

    // MARK: Actions

    private func perform(_ command: SessionCommand) {
        store.run(
            command, tools: machineTools, open: { row, transport in show(row, transport: transport) },
            kill: { killTarget = $0 }, rename: { renameTarget = $0 }
        )
    }

    private func show(_ row: SessionRow, transport: TransportPreference? = nil) {
        keys.cursor = TabID(hostID: row.hostID, sessionID: row.id)
        // The detail flows to the session while its row stays highlighted.
        withAnimation(Motion.animation(.paneFlow, reduce: reduceMotion)) {
            store.showFromSidebar(row, transport: transport)
        }
    }

    private func show(_ id: TabID) {
        keys.cursor = id
        withAnimation(Motion.animation(.paneFlow, reduce: reduceMotion)) {
            store.showSession(id)
        }
    }

    private func openCursor() -> KeyPress.Result {
        guard let row = keys.row(in: rows) else { return .ignored }
        show(row)
        return .handled
    }

    /// The same confirmation as the row menu, and only where the menu offers Kill.
    private func askToEndCursor() -> KeyPress.Result {
        guard let row = keys.rowToEnd(in: rows, machines: store.controlCenter.machines) else { return .ignored }
        killTarget = row
        return .handled
    }

    private func returnToTerminal() -> KeyPress.Result {
        searchFocused = false
        searching = false
        listFocused = false
        guard let tab = store.focusedTab, let handle = terminals?.handle(for: tab.id) else { return .ignored }
        handle.engineView.view.focusFirstResponder()
        return .handled
    }
}

extension AppStore {
    /// A row picked in the sidebar fills the focused pane. With one pane on screen that is the
    /// primary, so the workspace shows one large terminal unless the user opened a second.
    func showFromSidebar(_ row: SessionRow, transport: TransportPreference? = nil) {
        boardShown = false
        // Whatever the detail had pushed would otherwise stay on top of the session.
        if workspaceInline { path = [] }
        let id = TabID(hostID: row.hostID, sessionID: row.id)
        guard panesVisible, workspace.secondary != nil, workspace.focused == .secondary else {
            open(hostID: row.hostID, sessionID: row.id, transport: transport)
            return
        }
        if !workspace.tabs.contains(where: { $0.id == id }) || transport != nil {
            let primary = workspace.selected
            let secondary = workspace.secondary
            open(hostID: row.hostID, sessionID: row.id, transport: transport)
            workspace.selected = primary
            workspace.secondary = secondary
        }
        place(id, in: .secondary)
        workspaceOpen = true
    }
}

extension UIView {
    /// Gives keyboard focus to the first view under this one that takes it.
    @discardableResult func focusFirstResponder() -> Bool {
        if canBecomeFirstResponder { return becomeFirstResponder() }
        return subviews.contains { $0.focusFirstResponder() }
    }
}

#if DEBUG
#Preview("Sidebar") {
    NavigationStack {
        SessionSidebar(store: AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter), now: Samples.now)
    }
}
#endif
