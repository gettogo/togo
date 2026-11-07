import SwiftUI
import TogoKit

/// One machine in the roster: its name and count, a line for whatever keeps it from being live,
/// then one line per session and the sessions a restart ended.
struct RosterSection: View {
    let group: RosterModel.Group
    var etAvailable = false
    let now: Date
    var zoom: Namespace.ID?
    /// The selected line, drawn on the raised surface; nil on a phone.
    var selected: TabID?
    /// The line the keyboard cursor is on, beside a terminal.
    var cursor: TabID?
    /// Lines can be dragged into a pane, which only the wide roster sits beside.
    var draggable = false
    var restoring: Set<TabID> = []
    /// Whether a machine that is off can be woken from here; false until the build allows it.
    var wakeEnabled = WakeFeature.isEnabled
    /// What the last wake signal said.
    var wakeNote: String?
    let perform: (SessionCommand) -> Void

    private var machine: MachineSection { group.machine }

    var body: some View {
        Section {
            status
            ForEach(group.lines) { line in
                sessionLine(line)
            }
            if machine.reachability == .online {
                ForEach(machine.restorable) { session in
                    RestorableSessionRow(
                        session: session, busy: restoring.contains(TabID(hostID: machine.id, sessionID: session.id))
                    ) { action in
                        perform(.restore(machine.id, session, resume: action == .restoreAndContinue))
                    }
                    .listRowBackground(Color.togoInk)
                    .dismissesRestorable { perform(.dismissRestorable(machine.id, session)) }
                }
            }
        } header: {
            Group {
                if case .offline = machine.reachability {
                    heading.tutorialTip(.offline)
                } else {
                    heading
                }
            }
            .contextMenu { MachineMenuItems(machine: machine, perform: perform) }
            .id(SessionRotors.Target.machine(machine.id))
        }
    }

    private var heading: some View {
        RosterMachineHeading(
            name: machine.name, count: group.count,
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

    /// Lines of a machine Togo cannot reach right now keep their place, dimmed, so nothing the
    /// user was about to tap moves away.
    private var stale: Bool {
        switch machine.reachability {
        case .offline, .authNeeded, .failed, .keyMissing: true
        case .connecting, .waking, .online: false
        }
    }

    private var connecting: Bool {
        machine.reachability == .connecting || machine.reachability == .waking
    }

    // MARK: Machine status

    @ViewBuilder private var status: some View {
        switch machine.reachability {
        case .authNeeded:
            MachineMessage(ControlCenterCopy.signedOut(machine.name), action: ControlCenterCopy.signInAgain) {
                perform(.signIn(machine.id))
            }
        case .failed(let reason):
            MachineMessage(reason, action: ControlCenterCopy.tryAgain) { perform(.retry(machine.id)) }
        case .keyMissing:
            MachineMessage(ControlCenterCopy.keyMissing(device: DeviceName.model), action: ControlCenterCopy.edit) {
                perform(.edit(machine.id))
            }
        case .connecting, .waking:
            if machine.sessions.isEmpty {
                PawWalk(machine.reachability == .waking ? ControlCenterCopy.waking : ControlCenterCopy.connecting)
                    .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                    .listRowBackground(Color.togoInk)
            }
        case .offline:
            if let wakeNote {
                quiet(wakeNote)
            }
        case .online:
            switch machine.helper {
            case .missing:
                MachineMessage(ControlCenterCopy.plainSSH, action: ControlCenterCopy.install) {
                    perform(.installHelper(machine.id))
                }
            case .checking where machine.sessions.isEmpty:
                PawWalk(ControlCenterCopy.connecting)
                    .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                    .listRowBackground(Color.togoInk)
            case .present, .outdated, .checking:
                if case .outdated(let found) = machine.helper {
                    MachineMessage(ControlCenterCopy.helperOutdated(found), action: ControlCenterCopy.update) {
                        perform(.installHelper(machine.id))
                    }
                }
                if machine.sessions.isEmpty && machine.restorable.isEmpty {
                    Button { perform(.newSession(machine.id)) } label: {
                        Label(RosterCopy.newSession, systemImage: "plus")
                            .font(.subheadline)
                            .foregroundStyle(Color.togoSnow)
                            .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
                            .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .listRowBackground(Color.togoInk)
                    .accessibilityIdentifier("machine.newSession")
                }
            }
        }
    }

    private func quiet(_ text: String) -> some View {
        Text(text)
            .font(.subheadline)
            .foregroundStyle(Color.togoMute)
            .frame(maxWidth: .infinity, minHeight: 32, alignment: .leading)
            .listRowBackground(Color.togoInk)
    }

    // MARK: Lines

    private func sessionLine(_ line: RosterModel.Line) -> some View {
        let row = line.row
        let actions = SessionActions.actions(for: row, on: machine)
        let id = line.id
        return Button {
            perform(.open(row, nil))
        } label: {
            RosterLine(line: line, stale: stale, connecting: connecting, selected: selected == id)
        }
        .buttonStyle(.plain)
        .hoverEffect(.highlight)
        .listRowBackground(lineBackground(id))
        .listRowSeparator(selected == id ? .hidden : .automatic)
        .modifier(ZoomSource(id: id, namespace: zoom))
        .modifier(LineDrag(id: id, enabled: draggable))
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
        .contextMenu {
            SessionMenu(row: row, actions: actions, etAvailable: etAvailable, perform: perform) {
                RosterMissionMenuButton(line: line)
                if draggable {
                    OpenInWindowButton(tab: id)
                }
            }
        }
    }

    /// The selected line sits on the raised surface; the keyboard cursor draws a ring.
    private func lineBackground(_ id: TabID) -> some View {
        let shape = RoundedRectangle(cornerRadius: 14, style: .continuous)
        return ZStack {
            Color.togoInk
            if selected == id {
                shape.fill(Color.togoRaised).padding(.horizontal, 6)
            }
            if cursor == id {
                shape.strokeBorder(Color.togoSnow, lineWidth: 2).padding(.horizontal, 6)
            }
        }
    }
}

/// A machine's name in semibold with its session count in mono after it, and why it is not
/// live when it is not.
struct RosterMachineHeading: View {
    @Environment(\.dynamicTypeSize) private var typeSize
    let name: String
    let count: Int
    var caption: String?
    /// Set on an offline machine that can be woken.
    var onWake: (() -> Void)?

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(verbatim: name)
                    .font(.subheadline.weight(.semibold))
                    .foregroundStyle(Color.togoSnow)
                Text(verbatim: "\(count)")
                    .font(.system(.caption, design: .monospaced))
                    .monospacedDigit()
                    .foregroundStyle(Color.togoMute)
                if let caption {
                    Text(caption)
                        .font(.footnote)
                        .foregroundStyle(Color.togoMute)
                        .lineLimit(1)
                }
                Spacer(minLength: 0)
                if let onWake, !typeSize.isAccessibilitySize {
                    wakeButton(onWake)
                }
            }
            if let onWake, typeSize.isAccessibilitySize {
                wakeButton(onWake)
            }
        }
        .textCase(nil)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }

    private func wakeButton(_ run: @escaping () -> Void) -> some View {
        Button(WakeCopy.button, action: run)
            .buttonStyle(.secondary(.fit))
            .fixedSize()
    }
}

/// A machine's symbol and name over its rows, for lists outside the roster such as the archive.
struct MachineHeading: View {
    @ScaledMetric(relativeTo: .headline) private var symbolWidth: CGFloat = 28
    let machine: MachineSection
    var caption: String?

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: machine.symbol.systemImage)
                .font(.headline)
                .foregroundStyle(Color.togoMute)
                .frame(width: symbolWidth)
                .accessibilityHidden(true)
            SectionHeading(machine.name, detail: caption)
        }
        .textCase(nil)
    }
}

/// What a machine's heading offers on a long press, whatever state the machine is in.
struct MachineMenuItems: View {
    let machine: MachineSection
    let perform: (SessionCommand) -> Void

    var body: some View {
        Button(SidebarCopy.newSessionHere, systemImage: "plus") { perform(.newSession(machine.id)) }
            .disabled(machine.reachability != .online)
        Button(SidebarCopy.installHelper, systemImage: "arrow.down.circle") { perform(.installHelper(machine.id)) }
            .disabled(machine.reachability != .online)
        Button(SidebarCopy.signInAgain, systemImage: "person.badge.key") { perform(.signIn(machine.id)) }
        Button(SidebarCopy.editMachine, systemImage: "pencil") { perform(.edit(machine.id)) }
    }
}

enum SidebarCopy {
    static let newSessionHere = String(localized: "sidebar.machine.newSession", defaultValue: "New session here")
    static let installHelper = String(localized: "sidebar.machine.installHelper", defaultValue: "Install or update helper")
    static let signInAgain = String(localized: "sidebar.machine.signIn", defaultValue: "Sign in again")
    static let editMachine = String(localized: "sidebar.machine.edit", defaultValue: "Edit machine")
}

/// A sentence about the machine with the one thing to do about it, as a trailing text button.
/// At accessibility sizes the button goes under the sentence.
private struct MachineMessage: View {
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    let text: String
    let action: String
    let run: () -> Void

    init(_ text: String, action: String, run: @escaping () -> Void) {
        self.text = text
        self.action = action
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
        .listRowBackground(Color.togoInk)
    }
}

/// Lets a line be dragged into a pane as its tab.
private struct LineDrag: ViewModifier {
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

/// Lets a line grow into its terminal when the presenter shares a namespace with the roster.
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
