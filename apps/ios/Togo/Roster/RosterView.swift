import SwiftUI
import TogoKit
import UIKit

/// The home: a small status row with its tally, then every machine's sessions, one line each.
/// On a phone a tap zooms into the terminal; beside a terminal it shows the session there and
/// its line stays selected. There is no title: the status row says what the user needs to know,
/// and scrolls away with the lines.
///
/// Beside a terminal, the arrows move a cursor through the lines, Return opens the line under
/// it, ⌘⌫ asks to end that session and Escape hands focus back to the terminal.
struct RosterView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.machineTools) private var machineTools
    @Environment(\.supportsMultipleWindows) private var supportsMultipleWindows
    @Environment(\.hardwareKeyboard) private var hardwareKeyboard
    @Bindable var store: AppStore
    /// For Escape, which puts keyboard focus back in the focused pane's terminal.
    var terminals: TerminalPool?
    /// Lines are zoom sources in this namespace, so the terminal grows out of the tapped line.
    var zoom: Namespace.ID?
    /// Beside a terminal: lines show there and the focused one is drawn selected.
    var wide = false
    /// A fixed clock for previews and snapshots; nil follows the time of day.
    var now: Date?
    /// The launch mark shrinks into the headline's leading edge on a cold start.
    var launchTarget = false
    /// On a phone, whether the navigation bar stays for a host's own items there (the monitor's
    /// keep-awake switch, the tutorial's Done). Otherwise it goes: the roster's bar row is its
    /// search field with the menu beside it.
    var keepsNavigationBar = false
    /// The roster is the sidebar column beside the terminal, so its bar offers to hide it: the
    /// way to a full-width terminal by touch, as ⌃⌘S is by keyboard.
    var sidebar = false

    @State private var killTarget: SessionRow?
    @State private var renameTarget: SessionRow?
    @State private var keys = SidebarKeyboard()
    @State private var searching = false
    /// The session whose mission control a long-press opened.
    @State private var missionTarget: RosterMissionTarget?
    /// Where that mission control handed off to, acted on once its sheet has gone.
    @State private var missionExit: (tab: TabID, exit: RosterMissionExit)?
    @FocusState private var listFocused: Bool

    var body: some View {
        if wide {
            // As a sidebar column the field sits in the column's bar row, the menu trailing it.
            screen
                .toolbar { toolbar }
                .searchable(
                    text: $store.query, isPresented: $searching, placement: .toolbarPrincipal,
                    prompt: RosterCopy.searchPrompt
                )
        } else {
            // On a phone the system puts a searchable field in a row of its own under the bar,
            // leaving the menu alone above it; so the field and the menu share one row here.
            screen
                .safeAreaBar(edge: .top) {
                    if hasNoMachines {
                        // Nothing to search yet; the menu keeps its place for Settings.
                        HStack {
                            Spacer()
                            menu
                        }
                            .padding(.horizontal, 16)
                            .padding(.top, 2)
                    } else {
                        RosterSearchBar(query: $store.query, searching: $searching) { menu }
                    }
                }
                .toolbarVisibility(keepsNavigationBar ? .automatic : .hidden, for: .navigationBar)
        }
    }

    /// Everything but the bar row: ⌘K or a tap searches session names, agents, machines and
    /// folders, from the bar row on every width.
    private var screen: some View {
        content
            .background(Color.togoInk)
            // `initial`: a roster that comes on screen with the ⌘K that showed it, as the
            // sidebar does, still takes it.
            .onChange(of: store.sidebar.searchRequests, initial: true) { takeSearchRequest() }
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackgroundVisibility(.hidden, for: .navigationBar)
            .modifier(SessionDialogs(store: store, kill: $killTarget, rename: $renameTarget))
            .environment(\.openSessionMission, OpenSessionMission { missionTarget = RosterMissionTarget(tab: $0) })
            // Mission control beside nothing here: the side context's sheet on every width.
            .harnessPresent(item: $missionTarget, as: .sideContext, name: "roster.mission", onDismiss: leaveMission) { target in
                RosterMissionSheet(app: store, tab: target.tab) { exit in
                    missionExit = exit.map { (target.tab, $0) }
                    missionTarget = nil
                }
            }
    }

    private var hasNoMachines: Bool {
        store.services.hosts.isEmpty && store.controlCenter.machines.isEmpty
    }

    @ViewBuilder private var content: some View {
        if hasNoMachines {
            RosterNoMachines { store.showSheet(.addMachine) }
        } else {
            // Lines arriving and leaving glide on `resize`; nothing else in the list animates.
            IdentityKeyed(store.rosterShown, key: RosterShown.shape, quiet: RosterShown.quiet) { shown in
                TimelineView(.everyMinute) { context in
                    list(shown, now: now ?? context.date)
                }
            }
        }
    }

    // MARK: List

    private func list(_ shown: RosterShown, now: Date) -> some View {
        let roster = shown.model
        let selected = RosterModel.selection(focused: shownTab, wide: wide, in: roster)
        return ScrollViewReader { scroller in
            List {
                // The status is the first row and scrolls with the lines; nothing is pinned above.
                Section {
                    if RosterHeadline.shows(roster) {
                        RosterHeadline(roster: roster, launchTarget: launchTarget)
                            .listRowInsets(EdgeInsets(top: 4, leading: 20, bottom: 8, trailing: 20))
                            .listRowBackground(Color.togoInk)
                            .listRowSeparator(.hidden)
                    }
                }
                .listSectionSeparator(.hidden)
                // The notice has a section of its own that is always there, so its row coming
                // and going never lands in the same update as a line leaving a machine.
                Section {
                    if roster.deviceOffline {
                        Label(ControlCenterCopy.deviceOffline(DeviceName.model), systemImage: "wifi.slash")
                            .font(.subheadline)
                            .foregroundStyle(Color.togoSnow)
                            .listRowBackground(Color.togoInk)
                            .listRowSeparator(.hidden)
                            .accessibilityAddTraits(.isHeader)
                            .accessibilityIdentifier("roster.deviceOffline")
                    }
                    if let notice = shown.notice {
                        Label(notice.text, systemImage: notice.isError ? "exclamationmark.circle" : "info.circle")
                            .font(.subheadline)
                            .foregroundStyle(Color.togoSnow)
                            .listRowBackground(Color.togoInk)
                            .listRowSeparator(.hidden)
                    }
                    // A filter hides lines, those that need the user included; it says so
                    // where the lines start, with the way back.
                    if store.filter != .all {
                        RosterFilterRow(filter: store.filter) { store.filter = .all }
                            .listRowBackground(Color.togoInk)
                            .listRowSeparator(.hidden)
                    }
                }
                .listSectionSeparator(.hidden)
                ForEach(roster.groups) { group in
                    RosterSection(
                        group: group, etAvailable: store.etAvailable(group.id), now: now, zoom: zoom,
                        selected: selected, cursor: keyboardNavigates && listFocused ? keys.cursor : nil, draggable: linesDrag,
                        restoring: store.restoring, wakeNote: shown.wakeNotes[group.id], perform: perform
                    )
                }
            }
            .listStyle(.plain)
            .scrollDismissesKeyboard(.immediately)
            .overlay {
                if store.listNarrowed && roster.lines.isEmpty {
                    if store.query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                        ContentUnavailableView {
                            Label(ControlCenterCopy.filterEmpty(store.filter), systemImage: "line.3.horizontal.decrease")
                        } actions: {
                            Button(RosterCopy.showAll) { store.filter = .all }
                                .buttonStyle(.secondary(.fit))
                                .accessibilityIdentifier("roster.filterEmpty.showAll")
                        }
                    } else {
                        ContentUnavailableView.search(text: store.query)
                    }
                }
            }
            .listRowSeparatorTint(Color.togoLine)
            .listSectionSeparator(.hidden)
            .scrollContentBackground(.hidden)
            .background(Color.togoInk)
            .refreshable { await store.services.refresh() }
            .task(id: store.restoreCandidates) { await store.loadRestorable(tools: machineTools) }
            .sessionRotors(store.listMachines, requests: [])
            .focusable(keyboardNavigates)
            .focusEffectDisabled()
            .focused($listFocused)
            .onKeyPress(keys: [.upArrow, .downArrow]) { press in
                guard keyboardNavigates else { return .ignored }
                keys.move(press.key == .upArrow ? -1 : 1, through: roster.lines.map(\.id))
                if let cursor = keys.cursor { scroller.scrollTo(SessionRotors.Target.row(cursor)) }
                return .handled
            }
            .onKeyPress(.return) { openCursor(roster) }
            .onKeyPress(.delete, phases: .down) { press in
                press.modifiers.contains(.command) ? askToEndCursor(roster) : .ignored
            }
            .onKeyPress(.escape) { returnToTerminal() }
        }
    }

    // MARK: Toolbar

    /// One button: everything that is not a session lives in its menu, so the roster has no
    /// bottom bar of its own; the prompt bar sits there.
    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        if sidebar {
            ToolbarItem(placement: .topBarLeading) {
                Button(AppCommandCopy.hideSidebar, systemImage: "sidebar.leading") { store.toggleSidebar() }
                    .foregroundStyle(Color.togoSnow)
                    .accessibilityIdentifier("roster.hideSidebar")
            }
        }
        ToolbarItem(placement: .topBarTrailing) {
            Menu { menuItems } label: {
                Label(RosterCopy.more, systemImage: "line.3.horizontal")
            }
            .foregroundStyle(Color.togoSnow)
            .accessibilityIdentifier("roster.more")
        }
    }

    /// The same menu on a phone, as a glass circle trailing the search field.
    private var menu: some View {
        Menu { menuItems } label: { RosterMenuLabel() }
            .menuStyle(.button)
            .buttonStyle(.plain)
            .accessibilityIdentifier("roster.more")
    }

    @ViewBuilder private var menuItems: some View {
        Button(RosterCopy.newSession, systemImage: "plus") { store.startNewSession() }
            .disabled(store.services.hosts.isEmpty)
        Picker(selection: $store.filter) {
            ForEach(SessionFilter.allCases, id: \.self) { filter in
                Text(ControlCenterCopy.filterName(filter)).tag(filter)
            }
        } label: {
            Label(RosterCopy.show, systemImage: "line.3.horizontal.decrease")
        }
        .pickerStyle(.menu)
        Button(MonitorCopy.toggle, systemImage: "rectangle.grid.1x2") { store.toggleMonitor() }
        Divider()
        Button(RosterCopy.settings, systemImage: "gearshape") { store.showSettings() }
        Button(RosterCopy.addMachine, systemImage: "desktopcomputer") { store.showSheet(.addMachine) }
        Button(RosterCopy.archive, systemImage: "archivebox") { store.showArchive() }
            .disabled(store.controlCenter.archived.isEmpty)
    }

    // MARK: Derived state

    /// The session the terminal column shows, while one does.
    private var shownTab: TabID? {
        store.workspaceOnScreen ? store.focusedTab?.id : nil
    }

    /// A line drags into a pane or out to a window of its own, so only where one can take it.
    private var linesDrag: Bool {
        wide && (supportsMultipleWindows || store.panesFit || store.workspace.secondary != nil)
    }

    // MARK: Actions

    private func perform(_ command: SessionCommand) {
        store.run(
            command, tools: machineTools, open: { row, transport in open(row, transport: transport) },
            kill: { killTarget = $0 }, rename: { renameTarget = $0 }
        )
    }

    /// Beside a terminal the column flows to the session; on a phone the terminal zooms out of
    /// the line.
    private func open(_ row: SessionRow, transport: TransportPreference?) {
        // A line tapped on the monitor leaves it for the terminal.
        store.monitorShown = false
        guard wide else {
            store.open(hostID: row.hostID, sessionID: row.id, transport: transport)
            return
        }
        keys.cursor = TabID(hostID: row.hostID, sessionID: row.id)
        // motion: cause → effect — the tapped line's session flows into the pane beside it
        withAnimation(Motion.animation(.paneFlow, reduce: reduceMotion)) {
            store.showFromSidebar(row, transport: transport)
        }
    }

    /// After a roster mission sheet closes: the review for a diff or preview, or the session's
    /// chat.
    private func leaveMission() {
        guard let handoff = missionExit else { return }
        missionExit = nil
        let tab = handoff.tab
        switch handoff.exit {
        case .external(let target):
            store.followMissionTarget(target, from: tab)
        case .chat:
            guard let row = store.roster.lines.first(where: { $0.id == tab })?.row else { return }
            open(row, transport: nil)
        }
    }

    /// Arrow keys, Return and ⌘⌫ move through the lines wherever a hardware keyboard is
    /// attached, not only beside a terminal: a narrow iPad window or a phone with a keyboard too.
    private var keyboardNavigates: Bool {
        wide || hardwareKeyboard.hardwareKeyboardConnected
    }

    private func openCursor(_ roster: RosterModel) -> KeyPress.Result {
        guard keyboardNavigates, let row = keys.row(in: roster.lines.map(\.row)) else { return .ignored }
        open(row, transport: nil)
        return .handled
    }

    /// The same confirmation as the line's menu, and only where the menu offers it.
    private func askToEndCursor(_ roster: RosterModel) -> KeyPress.Result {
        guard keyboardNavigates, let row = keys.rowToEnd(in: roster.lines.map(\.row), machines: store.controlCenter.machines)
        else { return .ignored }
        killTarget = row
        return .handled
    }

    /// Puts the caret in the search field for a ⌘K no roster has taken yet. A turn later, so a
    /// roster that has just come on screen has its field in place first.
    private func takeSearchRequest() {
        guard store.sidebar.searchPending else { return }
        store.sidebar.searchPending = false
        Task { @MainActor in searching = true }
    }

    private func returnToTerminal() -> KeyPress.Result {
        guard wide else { return .ignored }
        listFocused = false
        guard let tab = store.focusedTab, let handle = terminals?.handle(for: tab.id) else { return .ignored }
        handle.engineView.view.focusFirstResponder()
        return .handled
    }
}

/// One small row in one voice: "1 needs you" and the tally after it ("2 working  2 idle"),
/// all in the same mono at the same size on one baseline. "Needs you" stands out the way it
/// does on a line, by its status colour and weight, with the lit dot before it; with nothing
/// waiting only the tally shows.
struct RosterHeadline: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let roster: RosterModel
    /// Marks the leading edge, where the launch mark shrinks to on its way out.
    var launchTarget = false

    /// The one font for every count in the row.
    static let font = Font.system(.footnote, design: .monospaced)

    /// Nothing to say with no sessions and nothing waiting.
    static func shows(_ roster: RosterModel) -> Bool {
        roster.waiting > 0 || !roster.tallyText.isEmpty
    }

    var body: some View {
        // One line where it fits; at the largest text sizes the tally goes under the needs.
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .firstTextBaseline, spacing: 12) { parts }
            VStack(alignment: .leading, spacing: 4) { parts }
        }
        // motion: attention — the dot lights when something needs you, and goes out after
        .motion(.respond, value: roster.waiting)
        .background(alignment: .leading) {
            if launchTarget {
                Color.clear
                    .frame(width: 20, height: 20)
                    .launchTarget(fades: true)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
        .accessibilityIdentifier("roster.headline")
    }

    @ViewBuilder private var parts: some View {
        if roster.waiting > 0 {
            // Centred on the words; the row lines up on their baseline.
            HStack(alignment: .center, spacing: 8) {
                BeaconDot(side: 6, relativeTo: .footnote)
                Text(roster.headline)
                    .font(Self.font)
                    .fontWeight(.semibold)
                    .monospacedDigit()
                    .foregroundStyle(Color.togoAccentText)
                    // trunc-ok: the headline is a count and two words
                    .lineLimit(1)
                    .fixedSize()
                    // The needs count is an event: it may roll. Nothing else here does.
                    .countChange(Double(roster.waiting))
            }
            // motion: attention — something now needs you; the lit words appear in place
            .transition(.opacity)
        }
        if !roster.tallyText.isEmpty {
            // The session tally is a background value: it changes at once, never rolls.
            Text(verbatim: roster.tallyText)
                .font(Self.font)
                .monospacedDigit()
                .foregroundStyle(Color.togoMute)
                .fixedSize(horizontal: false, vertical: true)
                .transaction { $0.animation = nil }
        }
    }
}

/// The beacon lit: the one mark that says something waits on the user. Hidden from
/// VoiceOver; the words beside it carry the meaning.
struct BeaconDot: View {
    @ScaledMetric private var side: CGFloat

    /// `side` at the default text size, scaled with `relativeTo`.
    init(side: CGFloat = 8, relativeTo style: Font.TextStyle = .headline) {
        _side = ScaledMetric(wrappedValue: side, relativeTo: style)
    }

    var body: some View {
        Circle()
            // accent-ok: the lit dot says something needs the user.
            .fill(Color.togoAccent)
            .frame(width: side, height: side)
            // accent-ok: the lit beacon dot's halo.
            .background(Circle().fill(Color.togoAccent.opacity(0.14)).padding(-4))
            .accessibilityHidden(true)
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
#Preview("Roster") {
    NavigationStack {
        RosterView(store: AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter), now: Samples.now)
    }
}
#endif
