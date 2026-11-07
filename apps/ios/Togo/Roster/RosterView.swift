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
    @FocusState private var listFocused: Bool

    var body: some View {
        content
            .background(Color.togoInk)
            .toolbar { toolbar }
            // Pulled down, or ⌘K: a search over session names, agents, machines and folders.
            .searchable(
                text: $store.query, isPresented: $searching,
                placement: .navigationBarDrawer(displayMode: .automatic), prompt: RosterCopy.searchPrompt
            )
            .onChange(of: store.sidebar.searchRequests) { searching = true }
            .navigationBarTitleDisplayMode(.inline)
            .toolbarBackgroundVisibility(.hidden, for: .navigationBar)
            .modifier(SessionDialogs(store: store, kill: $killTarget, rename: $renameTarget))
    }

    private var hasNoMachines: Bool {
        store.services.hosts.isEmpty && store.controlCenter.machines.isEmpty
    }

    @ViewBuilder private var content: some View {
        if hasNoMachines {
            RosterNoMachines { store.showSheet(.addMachine) }
        } else {
            TimelineView(.everyMinute) { context in
                list(now: now ?? context.date)
            }
        }
    }

    // MARK: List

    private func list(now: Date) -> some View {
        let roster = store.roster
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
                    if let notice = store.notice {
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
                        group: group, etAvailable: store.etAvailable(group.id), now: now, zoom: zoom,
                        selected: selected, cursor: wide && listFocused ? keys.cursor : nil, draggable: linesDrag,
                        restoring: store.restoring, wakeNote: store.wakeNotes[group.id], perform: perform
                    )
                }
            }
            .listStyle(.plain)
            .overlay {
                if store.listNarrowed && roster.lines.isEmpty {
                    if store.query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                        ContentUnavailableView(ControlCenterCopy.filterEmpty(store.filter), systemImage: "line.3.horizontal.decrease")
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
            .focusable(wide)
            .focusEffectDisabled()
            .focused($listFocused)
            .onKeyPress(keys: [.upArrow, .downArrow]) { press in
                guard wide else { return .ignored }
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
        ToolbarItem(placement: .topBarTrailing) {
            Menu {
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
            } label: {
                Label(RosterCopy.more, systemImage: "line.3.horizontal")
            }
            .foregroundStyle(Color.togoSnow)
            .accessibilityIdentifier("roster.more")
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
        // A line tapped on the monitor leaves it for the terminal.
        store.monitorShown = false
        guard wide else {
            store.open(hostID: row.hostID, sessionID: row.id, transport: transport)
            return
        }
        keys.cursor = TabID(hostID: row.hostID, sessionID: row.id)
        withAnimation(Motion.animation(.paneFlow, reduce: reduceMotion)) {
            store.showFromSidebar(row, transport: transport)
        }
    }

    private func openCursor(_ roster: RosterModel) -> KeyPress.Result {
        guard wide, let row = keys.row(in: roster.lines.map(\.row)) else { return .ignored }
        open(row, transport: nil)
        return .handled
    }

    /// The same confirmation as the line's menu, and only where the menu offers it.
    private func askToEndCursor(_ roster: RosterModel) -> KeyPress.Result {
        guard wide, let row = keys.rowToEnd(in: roster.lines.map(\.row), machines: store.controlCenter.machines)
        else { return .ignored }
        killTarget = row
        return .handled
    }

    private func returnToTerminal() -> KeyPress.Result {
        guard wide else { return .ignored }
        listFocused = false
        guard let tab = store.focusedTab, let handle = terminals?.handle(for: tab.id) else { return .ignored }
        handle.engineView.view.focusFirstResponder()
        return .handled
    }
}

/// One small row: "1 needs you" with its beacon and the tally after it in mono, or with
/// nothing waiting only the tally ("1 idle").
struct RosterHeadline: View {
    let roster: RosterModel
    /// Marks the leading edge, where the launch mark shrinks to on its way out.
    var launchTarget = false

    /// Nothing to say with no sessions and nothing waiting.
    static func shows(_ roster: RosterModel) -> Bool {
        roster.waiting > 0 || !roster.tallyText.isEmpty
    }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            if roster.waiting > 0 {
                HStack(alignment: .center, spacing: 8) {
                    BeaconDot()
                    Text(roster.headline)
                        .font(.headline)
                        .monospacedDigit()
                        .foregroundStyle(Color.togoSnow)
                        .lineLimit(1)
                        .fixedSize()
                }
            }
            if !roster.tallyText.isEmpty {
                Text(verbatim: roster.tallyText)
                    .font(.system(.footnote, design: .monospaced))
                    .monospacedDigit()
                    .foregroundStyle(Color.togoMute)
                    .lineLimit(1)
                    .truncationMode(.tail)
            }
        }
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
}

/// The beacon lit: the one mark that says something waits on the user. Hidden from
/// VoiceOver; the words beside it carry the meaning.
struct BeaconDot: View {
    @ScaledMetric(relativeTo: .headline) private var side: CGFloat = 8

    var body: some View {
        Circle()
            .fill(Color.togoAccent)
            .frame(width: side, height: side)
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
