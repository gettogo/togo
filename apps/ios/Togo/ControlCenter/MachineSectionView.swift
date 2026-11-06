import SwiftUI
import TogoKit

/// One machine's sessions under its name, with a line for whatever keeps them from being live.
struct MachineSectionView: View {
    let machine: MachineSection
    var etAvailable = false
    let now: Date
    /// Search or a filter is narrowing the list, so an empty machine says nothing.
    var narrowed = false
    var zoom: Namespace.ID?
    /// Rows of the tabs on screen, marked in the sidebar.
    var highlighted: Set<TabID> = []
    /// The row the sidebar's keyboard cursor is on.
    var cursor: TabID?
    /// The sidebar's selected row, which the system marks; other visible tabs get a quieter fill.
    var selected: TabID?
    /// Rows can be dragged into a pane, which only the sidebar sits beside.
    var draggable = false
    var restoring: Set<TabID> = []
    /// Whether a machine that is off can be woken from here; false until the build allows it.
    var wakeEnabled = WakeFeature.isEnabled
    /// What the last wake signal said.
    var wakeNote: String?
    /// In the sidebar: rows take the list's native selection, the heading carries the machine's
    /// menu, and rows have no fill so the sidebar's glass shows.
    var sidebar = false
    let perform: (SessionCommand) -> Void

    /// The list's own row fill in the sidebar; the ink canvas in the compact list.
    private var rowFill: Color? { sidebar ? nil : Color.togoInk }

    var body: some View {
        Section {
            if !narrowed {
                status
            }
            ForEach(machine.sessions) { row in
                sessionRow(row)
            }
            if !narrowed, machine.reachability == .online {
                ForEach(machine.restorable) { session in
                    RestorableSessionRow(
                        session: session, busy: restoring.contains(TabID(hostID: machine.id, sessionID: session.id))
                    ) { action in
                        perform(.restore(machine.id, session, resume: action == .restoreAndContinue))
                    }
                    .listRowBackground(rowFill)
                    .dismissesRestorable { perform(.dismissRestorable(machine.id, session)) }
                }
            }
        } header: {
            HStack(alignment: .center, spacing: 4) {
                Group {
                    if case .offline = machine.reachability {
                        heading.tutorialTip(.offline)
                    } else {
                        heading
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                if sidebar {
                    MachineNewSessionButton(machine: machine, perform: perform)
                    MachineMenu(machine: machine, perform: perform)
                }
            }
            .id(SessionRotors.Target.machine(machine.id))
        }
    }

    private var heading: some View {
        MachineHeading(
            machine: machine,
            caption: AwakeCopy.caption(
                reach: Self.caption(machine.reachability, helper: machine.helper, lastSeen: machine.lastSeen, now: now),
                awake: machine.awake
            ),
            onWake: WakeCopy.showsButton(for: machine, enabled: wakeEnabled) ? { perform(.wake(machine.id)) } : nil
        )
    }

    /// The words after a machine's name unless it is online with the helper.
    static func caption(_ reachability: Reachability, helper: HelperState, lastSeen: Date?, now: Date) -> String? {
        switch reachability {
        case .offline:
            return lastSeen.map { ControlCenterCopy.offlineSeen(ago($0, now: now)) } ?? ControlCenterCopy.offline
        case .waking:
            return ControlCenterCopy.waking
        case .connecting, .online, .authNeeded, .failed, .keyMissing:
            return helper == .missing ? ControlCenterCopy.noHelper : nil
        }
    }

    private static func ago(_ date: Date, now: Date) -> String {
        let minutes = Int(now.timeIntervalSince(date) / 60)
        switch minutes {
        case ..<1: return ControlCenterCopy.justNow
        case ..<60: return ControlCenterCopy.minutesAgo(minutes)
        case ..<(24 * 60): return ControlCenterCopy.hoursAgo(minutes / 60)
        default: return date.formatted(.dateTime.month(.abbreviated).day())
        }
    }

    /// Rows of a machine Togo cannot reach right now keep their place, dimmed, so nothing the
    /// user was about to tap moves away.
    private var stale: Bool {
        switch machine.reachability {
        case .offline, .authNeeded, .failed, .keyMissing: true
        case .connecting, .waking, .online: false
        }
    }

    // MARK: Machine status

    @ViewBuilder private var status: some View {
        switch machine.reachability {
        case .authNeeded:
            MachineMessage(ControlCenterCopy.signedOut(machine.name), action: ControlCenterCopy.signInAgain, fill: rowFill) {
                perform(.signIn(machine.id))
            }
        case .failed(let reason):
            MachineMessage(reason, action: ControlCenterCopy.tryAgain, fill: rowFill) { perform(.retry(machine.id)) }
        case .keyMissing:
            MachineMessage(ControlCenterCopy.keyMissing(device: DeviceName.model), action: ControlCenterCopy.edit, fill: rowFill) {
                perform(.edit(machine.id))
            }
        case .connecting, .waking:
            if machine.sessions.isEmpty {
                PawWalk(machine.reachability == .waking ? ControlCenterCopy.waking : ControlCenterCopy.connecting)
                    .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                    .listRowBackground(rowFill)
            }
        case .offline:
            if let wakeNote {
                Text(wakeNote)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .listRowBackground(rowFill)
            }
        case .online:
            switch machine.helper {
            case .missing:
                MachineMessage(ControlCenterCopy.plainSSH, action: ControlCenterCopy.install, fill: rowFill) {
                    perform(.installHelper(machine.id))
                }
            case .checking where machine.sessions.isEmpty:
                PawWalk(ControlCenterCopy.connecting)
                    .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                    .listRowBackground(rowFill)
            case .present, .outdated, .checking:
                if case .outdated(let found) = machine.helper {
                    MachineMessage(ControlCenterCopy.helperOutdated(found), action: ControlCenterCopy.update, fill: rowFill) {
                        perform(.installHelper(machine.id))
                    }
                }
                if machine.sessions.isEmpty && machine.restorable.isEmpty && sidebar {
                    // The heading's plus starts a session here; the row only says there is none.
                    Text(SidebarCopy.noSessions)
                        .font(.subheadline)
                        .foregroundStyle(Color.togoMute)
                        .frame(maxWidth: .infinity, minHeight: 32, alignment: .leading)
                        .accessibilityIdentifier("machine.noSessions")
                } else if machine.sessions.isEmpty && machine.restorable.isEmpty {
                    // One quiet row: the plus in the bar is the main way to start a session.
                    Button { perform(.newSession(machine.id)) } label: {
                        Label(ControlCenterCopy.newSession, systemImage: "plus")
                            .foregroundStyle(Color.togoMute)
                            .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .hoverEffect(.highlight)
                    .accessibilityIdentifier("machine.newSession")
                    .listRowBackground(rowFill)
                }
            }
        }
    }

    // MARK: Rows

    private func sessionRow(_ row: SessionRow) -> some View {
        let actions = SessionActions.actions(for: row, on: machine)
        let id = TabID(hostID: row.hostID, sessionID: row.id)
        return rowContent(row, id: id)
        .modifier(ZoomSource(id: id, namespace: zoom))
        .modifier(RowDrag(id: id, enabled: draggable))
        .id(SessionRotors.Target.row(id))
        .swipeActions(edge: .trailing) {
            if actions.allows(.kill) {
                Button(SessionAction.kill.title(for: row), systemImage: SessionAction.kill.systemImage(for: row),
                       role: .destructive) { perform(.kill(row)) }
            }
            if actions.allows(.mute) {
                Button(SessionAction.mute.title(for: row), systemImage: SessionAction.mute.systemImage(for: row)) {
                    perform(.toggleMute(row))
                }
            }
        }
        .swipeActions(edge: .leading) {
            if actions.allows(.runAgain) {
                Button(SessionAction.runAgain.title(for: row), systemImage: SessionAction.runAgain.systemImage(for: row)) {
                    perform(.runAgain(row))
                }
            }
            if actions.allows(.rename) {
                Button(SessionAction.rename.title(for: row), systemImage: SessionAction.rename.systemImage(for: row)) {
                    perform(.rename(row))
                }
            }
        }
        .contextMenu { SessionMenu(row: row, actions: actions, etAvailable: etAvailable, perform: perform) }
    }

    /// In the sidebar a row is a selectable item; the system draws its selection. The compact
    /// list's rows are buttons over the ink canvas, with the visible tabs filled.
    @ViewBuilder private func rowContent(_ row: SessionRow, id: TabID) -> some View {
        if sidebar {
            SessionRowView(row: row, stale: stale, shown: highlighted.contains(id))
                .tag(SidebarItem.session(id))
                .modifier(SidebarRowMarks(secondary: highlighted.contains(id) && id != selected, cursor: cursor == id))
        } else {
            Button {
                perform(.open(row, nil))
            } label: {
                SessionRowView(row: row, stale: stale, shown: highlighted.contains(id))
            }
            .buttonStyle(.plain)
            .hoverEffect(.highlight)
            .listRowBackground(rowBackground(id))
        }
    }

    /// The visible tabs' rows are filled; the keyboard cursor draws a ring.
    @ViewBuilder private func rowBackground(_ id: TabID) -> some View {
        let shape = RoundedRectangle(cornerRadius: 12, style: .continuous)
        ZStack {
            Color.togoInk
            if highlighted.contains(id) {
                shape.fill(Color.togoLine).padding(.horizontal, 6)
            }
            if cursor == id {
                shape.strokeBorder(Color.togoSnow, lineWidth: 2).padding(.horizontal, 6)
            }
        }
    }
}

/// A machine's symbol and name over its rows. The symbol sits in a fixed column so every
/// machine name starts at the same place.
struct MachineHeading: View {
    @ScaledMetric(relativeTo: .headline) private var symbolWidth: CGFloat = 28
    @Environment(\.dynamicTypeSize) private var typeSize
    let machine: MachineSection
    var caption: String?
    /// Set on an offline machine that can be woken.
    var onWake: (() -> Void)?

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Image(systemName: machine.symbol.systemImage)
                    .font(.headline)
                    .foregroundStyle(Color.togoMute)
                    .frame(width: symbolWidth)
                    .accessibilityHidden(true)
                SectionHeading(machine.name, detail: caption)
                if let onWake, !typeSize.isAccessibilitySize {
                    wakeButton(onWake)
                }
            }
            if let onWake, typeSize.isAccessibilitySize {
                wakeButton(onWake)
                    .padding(.leading, symbolWidth + 8)
            }
        }
        .textCase(nil)
    }

    private func wakeButton(_ run: @escaping () -> Void) -> some View {
        Button(WakeCopy.button, action: run)
            .buttonStyle(.secondary(.fit))
            .fixedSize()
    }
}

/// A sentence about the machine with the one thing to do about it, as a trailing text button.
/// At accessibility sizes the button goes under the sentence.
private struct MachineMessage: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let text: String
    let action: String
    let fill: Color?
    let run: () -> Void

    init(_ text: String, action: String, fill: Color?, run: @escaping () -> Void) {
        self.text = text
        self.action = action
        self.fill = fill
        self.run = run
    }

    var body: some View {
        let layout = dynamicTypeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: 4))
            : AnyLayout(HStackLayout(alignment: .firstTextBaseline, spacing: 12))
        layout {
            Text(text)
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button(action, action: run)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
                .buttonStyle(.borderless)
                .frame(minHeight: 44)
        }
        .listRowBackground(fill)
    }
}

/// In the sidebar, the second pane's row gets a quiet fill and the keyboard cursor a ring;
/// the focused pane's row has the system's selection.
private struct SidebarRowMarks: ViewModifier {
    let secondary: Bool
    let cursor: Bool

    func body(content: Content) -> some View {
        if secondary || cursor {
            let shape = RoundedRectangle(cornerRadius: 12, style: .continuous)
            content.listRowBackground(
                ZStack {
                    if secondary { shape.fill(Color.togoLine) }
                    if cursor { shape.strokeBorder(Color.togoSnow, lineWidth: 2) }
                }
                .padding(.horizontal, 6)
            )
        } else {
            content
        }
    }
}

/// Lets a row be dragged into a pane as its tab.
private struct RowDrag: ViewModifier {
    let id: TabID
    let enabled: Bool

    func body(content: Content) -> some View {
        if enabled {
            content.draggable(id)
        } else {
            content
        }
    }
}

/// Lets a row grow into its terminal when the presenter shares a namespace with the list.
struct ZoomSource: ViewModifier {
    let id: AnyHashable
    let namespace: Namespace.ID?

    func body(content: Content) -> some View {
        if let namespace {
            content.matchedTransitionSource(id: id, in: namespace)
        } else {
            content
        }
    }
}
