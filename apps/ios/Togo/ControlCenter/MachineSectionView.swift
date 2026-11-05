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
    let perform: (SessionCommand) -> Void

    var body: some View {
        Section {
            if !narrowed {
                status
            }
            ForEach(machine.sessions) { row in
                sessionRow(row)
            }
        } header: {
            if case .offline = machine.reachability {
                heading.tutorialTip(.offline)
            } else {
                heading
            }
        }
    }

    private var heading: some View {
        MachineHeading(machine: machine, caption: Self.caption(
            machine.reachability, helper: machine.helper, lastSeen: machine.lastSeen, now: now
        ))
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
            EmptyView()
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
                if machine.sessions.isEmpty {
                    MachineMessage(ControlCenterCopy.noSessions, action: ControlCenterCopy.newSession) {
                        perform(.newSession(machine.id))
                    }
                }
            }
        }
    }

    // MARK: Rows

    private func sessionRow(_ row: SessionRow) -> some View {
        let actions = SessionActions.actions(for: row, on: machine)
        return Button {
            perform(.open(row, nil))
        } label: {
            SessionRowView(row: row, stale: stale)
        }
        .buttonStyle(.plain)
        .modifier(ZoomSource(id: TabID(hostID: row.hostID, sessionID: row.id), namespace: zoom))
        .listRowBackground(Color.togoInk)
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
            if actions.allows(.rename) {
                Button(SessionAction.rename.title(for: row), systemImage: SessionAction.rename.systemImage(for: row)) {
                    perform(.rename(row))
                }
            }
        }
        .contextMenu { menu(for: row, actions) }
    }

    @ViewBuilder private func menu(for row: SessionRow, _ actions: SessionActionSet) -> some View {
        ForEach(actions.available, id: \.self) { action in
            switch action {
            case .connectWith:
                ConnectWithMenu(etAvailable: etAvailable) { perform(.open(row, $0)) }
            case .kill:
                reviewItems(for: row, actions)
                Divider()
                menuButton(action, row, role: .destructive) { perform(.kill(row)) }
                    .disabled(actions.disabled.contains(action))
            case .open, .rename, .mute, .copyPath:
                menuButton(action, row) { perform(command(action, row)) }
                    .disabled(actions.disabled.contains(action))
            case .changes, .files, .ports:
                EmptyView()
            }
        }
        if !actions.available.contains(.kill) {
            reviewItems(for: row, actions)
        }
        if let note = actions.note {
            Divider()
            Text(note)
        }
    }

    /// Changes, Files and Ports, after the row's own actions and before Kill when there is one.
    @ViewBuilder private func reviewItems(for row: SessionRow, _ actions: SessionActionSet) -> some View {
        if !actions.review.isEmpty {
            Divider()
            ForEach(actions.review, id: \.self) { action in
                menuButton(action, row) { perform(command(action, row)) }
                    .disabled(actions.disabled.contains(action))
            }
            if let note = actions.reviewNote {
                Text(note)
            }
        }
    }

    private func menuButton(
        _ action: SessionAction, _ row: SessionRow, role: ButtonRole? = nil, run: @escaping () -> Void
    ) -> some View {
        Button(action.title(for: row), systemImage: action.systemImage(for: row), role: role, action: run)
    }

    private func command(_ action: SessionAction, _ row: SessionRow) -> SessionCommand {
        switch action {
        case .open, .connectWith: .open(row, nil)
        case .rename: .rename(row)
        case .mute: .toggleMute(row)
        case .copyPath: .copyPath(row)
        case .kill: .kill(row)
        case .changes: .review(row, .changes)
        case .files: .review(row, .files)
        case .ports: .review(row, .ports)
        }
    }
}

/// A machine's symbol and name over its rows. The symbol sits in a fixed column so every
/// machine name starts at the same place.
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

/// A sentence about the machine with the one thing to do about it.
private struct MachineMessage: View {
    let text: String
    let action: String
    let run: () -> Void

    init(_ text: String, action: String, run: @escaping () -> Void) {
        self.text = text
        self.action = action
        self.run = run
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(text)
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button(action, action: run)
                .buttonStyle(.primary(.fit))
                .padding(.vertical, 4)
        }
        .padding(.top, 6)
        .listRowBackground(Color.togoInk)
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
