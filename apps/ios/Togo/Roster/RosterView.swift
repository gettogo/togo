import SwiftUI
import TogoKit
import UIKit

/// The home: the filter and tally row, then every machine's sessions, one line each, then Add
/// machine and the archive.
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

    @State private var killTarget: SessionRow?
    @State private var renameTarget: SessionRow?
    @State private var keys = SidebarKeyboard()
    @State private var searching = false
    /// The session whose mission control a long-press opened.
    @State private var missionTarget: RosterMissionTarget?
    /// Where that mission control handed off to, acted on once its sheet has gone.
    @State private var missionExit: (tab: TabID, exit: RosterMissionExit)?
    @FocusState private var listFocused: Bool
    /// The sidebar header's own search field, on regular width.
    @FocusState private var fieldFocused: Bool
    /// The detail column's dock carries the sidebar toggle (root.md), so the header does not.
    @Environment(\.sidebarToggleInDock) private var toggleInDock

    /// One body at every width: the bar's items and the search field's place change with
    /// `wide`, the view tree does not, so a fold or resize keeps the list's scroll, search and
    /// cursor. System bars throughout, so they take the safe area and the standard spacing, and
    /// an iPhone Duo stands them on their side beside its status bar.
    var body: some View {
        withSearch(screen.toolbar { toolbar })
            // Regular width: the sidebar column has no bar items left (the toggle is the dock's
            // or the header's, New session is each machine's "+"), and iOS set any search
            // placement in a row of its own under an empty band. So the column's bar goes and
            // its header is one pinned row with the search field, level with the detail's tab
            // rail; a bar, so the list scrolls under it behind the system's scroll edge effect.
            // Modifier values, not branches, so a fold keeps the list's scroll (P11). The
            // stack's title stays for compact, where the system Back reads "Sessions".

    }

    /// Nothing to search before there is a machine. A phone searches at the foot, as iOS puts
    /// search there. On regular width the system field would sit in the hidden bar's drawer and
    /// never show; the header's own field searches the same `query` instead.
    @ViewBuilder private func withSearch(_ view: some View) -> some View {
        if hasNoMachines {
            view
        } else {
            view.searchable(
                text: $store.query, isPresented: $searching,
                placement: wide ? .navigationBarDrawer(displayMode: .always) : .automatic,
                prompt: RosterCopy.searchPrompt
            )
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
            // No title on screen: the status row says what matters. It is still the stack's
            // title, so the system's Back over the workspace reads "Sessions".
            .navigationTitle(WorkspaceCopy.sessions)
            .toolbar(removing: .title)
            .navigationBarTitleDisplayMode(.inline)
            // The bar keeps the system's scroll edge effect, so the list fades under it rather
            // than showing through behind its buttons. The sidebar column has no bottom bar.
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
                // The filter and tally are the first row and scroll with the lines; nothing is
                // pinned above. Notices share its section, apart from the machines', so a notice
                // coming and going never lands in the same update as a line leaving a machine,
                // and no empty section adds a gap above the first machine.
                Section {
                    // Its 44-point target is laid out in 28, so the first machine follows close.
                    RosterFilterHeader(store: store, roster: roster, launchTarget: launchTarget)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.vertical, -8)
                        .listRowInsets(EdgeInsets(top: 4, leading: 20, bottom: 0, trailing: 20))
                        .listRowBackground(Color.togoInk)
                        .listRowSeparator(.hidden)
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
                }
                .listSectionSeparator(.hidden)
                ForEach(roster.groups) { group in
                    RosterSection(
                        group: group, first: group.id == roster.groups.first?.id,
                        etAvailable: store.etAvailable(group.id), now: now, zoom: zoom,
                        selected: selected, cursor: keyboardNavigates && listFocused ? keys.cursor : nil, draggable: linesDrag,
                        restoring: store.restoring, wakeNote: shown.wakeNotes[group.id], perform: perform
                    )
                }
                // What used to wait in a "more" menu, where the list ends.
                Section {
                    ForEach(RosterListEnd.rows(archived: store.controlCenter.archived.count), id: \.self) { row in
                        RosterListEndRow(row: row, store: store)
                            .foregroundStyle(Color.togoSnow)
                            .listRowBackground(Color.togoInk)
                            .listRowSeparator(.hidden)
                    }
                }
                .listSectionSeparator(.hidden)
            }
            .listStyle(.plain)
            // The gap under the filter row was the list's: its 44-point minimum row height
            // padding the short filter and heading rows, and the space it adds between
            // sections. Both are zero; each row sets its own height and the headings their
            // own space above (8 under the filter, 24 between machines).
            .listSectionSpacing(0)
            .environment(\.defaultMinListRowHeight, 0)
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
            // A machine the user tried to add again comes into view, once.
            .onChange(of: store.revealMachine, initial: true) { _, id in
                guard let id else { return }
                store.revealMachine = nil
                // motion: state change — the list moves to the machine the user was sent to
                withAnimation(Motion.animation(.paneFlow, reduce: reduceMotion)) {
                    scroller.scrollTo(SessionRotors.Target.machine(id), anchor: .top)
                }
            }
        }
    }

    // MARK: Toolbar

    /// Each control once per layout (roster.md, `RosterBar`). A phone, iPhone Duo folded and a
    /// narrow window: New session then Settings at the trailing end, search at the foot;
    /// folded, the Duo stands them in its vertical bar in that order, search last. The sidebar
    /// column (iPad, iPhone Duo open): no bar items, only the search field in the bar; New
    /// session is each machine's "+", Settings is the detail column's (root.md), and there is
    /// no bottom bar. The filter is the list's first row everywhere, so no bar carries it.
    @ToolbarContentBuilder private var toolbar: some ToolbarContent {
        let bar = RosterBar.layout(wide: wide, hasMachines: !hasNoMachines)
        // Each its own item, so the prominent New session keeps its own glass.
        if let first = bar.topTrailing.first {
            ToolbarItem(placement: .topBarTrailing) { item(first) }
        }
        if bar.topTrailing.count > 1 {
            ToolbarItemGroup(placement: .topBarTrailing) {
                ForEach(bar.topTrailing.dropFirst(), id: \.self) { item($0) }
            }
        }
        if bar.bottom.contains(.search) {
            DefaultToolbarItem(kind: .search, placement: .bottomBar)
        }
        // Regular width: the header is the column's own bar, so the system places it, with the
        // safe area and bar height the detail column's tab rail gets, on every device.
        if wide, !hasNoMachines || !toggleInDock {
            ToolbarItem(placement: .principal) {
                RosterSidebarHeader(store: store, search: !hasNoMachines, toggle: !toggleInDock, focused: $fieldFocused)
                    .frame(maxWidth: .infinity)
            }
            .sharedBackgroundVisibility(.hidden)
        }
    }

    @ViewBuilder private func item(_ item: RosterBarItem) -> some View {
        switch item {
        case .settings: RosterSettingsButton(store: store)
        case .newSession: RosterNewSessionButton(store: store)
        case .search: EmptyView()
        }
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
        Task { @MainActor in
            if wide { fieldFocused = true } else { searching = true }
        }
    }

    private func returnToTerminal() -> KeyPress.Result {
        guard wide else { return .ignored }
        listFocused = false
        guard let tab = store.focusedTab, let handle = terminals?.handle(for: tab.id) else { return .ignored }
        handle.engineView.view.focusFirstResponder()
        return .handled
    }
}

/// One small row in one voice: the filter in force, then "1 needs you" and the tally after it
/// ("All · 2 working  2 idle"), and the chevron of the menu it opens, all in the same mono at
/// the same size on one baseline. "Needs you" stands out the way it does on a line, by its
/// status colour and weight, with the lit dot before it.
struct RosterHeadline: View {
    let roster: RosterModel
    var filter: SessionFilter = .all
    /// Marks the leading edge, where the launch mark shrinks to on its way out.
    var launchTarget = false

    /// The one font for every count in the row.
    static let font = Font.system(.footnote, design: .monospaced)

    /// What VoiceOver says for the row: "All, 1 needs you, 2 working  2 idle".
    static func spoken(roster: RosterModel, filter: SessionFilter) -> String {
        var parts = [ControlCenterCopy.filterName(filter)]
        if roster.waiting > 0 { parts.append(roster.headline) }
        if !roster.tallyText.isEmpty { parts.append(roster.tallyText) }
        return parts.joined(separator: ", ")
    }

    var body: some View {
        // One line where it fits; at the largest text sizes the tally goes under the filter.
        ViewThatFits(in: .horizontal) {
            HStack(alignment: .firstTextBaseline, spacing: 8) { parts }
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
        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("roster.headline")
    }

    @ViewBuilder private var parts: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Text(ControlCenterCopy.filterName(filter))
                .font(Self.font)
                .fontWeight(.semibold)
                .foregroundStyle(Color.togoSnow)
                // trunc-ok: a filter name is one or two words
                .lineLimit(1)
                .fixedSize()
            Image(systemName: "chevron.down")
                .font(.caption2.weight(.semibold))
                .foregroundStyle(Color.togoMute)
                .accessibilityHidden(true)
        }
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
