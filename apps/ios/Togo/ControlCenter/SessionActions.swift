import Foundation
import TogoKit

/// What the user can do to one session row, in menu order.
enum SessionAction: Hashable, CaseIterable, Sendable {
    case open, connectWith, rename, mute, copyPath, kill

    /// Killing ends the agent and its screen for good, so it is the one action that asks first.
    var needsConfirmation: Bool { self == .kill }

    func title(for row: SessionRow) -> String {
        switch self {
        case .open: ControlCenterCopy.open
        case .connectWith: ControlCenterCopy.connectWith
        case .rename: ControlCenterCopy.rename
        case .mute: row.muted ? ControlCenterCopy.unmute : ControlCenterCopy.mute
        case .copyPath: ControlCenterCopy.copyPath
        case .kill: ControlCenterCopy.kill
        }
    }

    func systemImage(for row: SessionRow) -> String {
        switch self {
        case .open: "terminal"
        case .connectWith: "network"
        case .rename: "pencil"
        case .mute: row.muted ? "bell" : "bell.slash"
        case .copyPath: "doc.on.doc"
        case .kill: "xmark.circle"
        }
    }
}

/// The actions a row offers. `disabled` ones stay in the menu so the user sees why they are
/// unavailable, which `note` says in words.
struct SessionActionSet: Equatable, Sendable {
    var available: [SessionAction]
    var disabled: Set<SessionAction>
    var note: String?

    func allows(_ action: SessionAction) -> Bool {
        available.contains(action) && !disabled.contains(action)
    }
}

/// Everything a row, card or machine section can ask the control center to do.
enum SessionCommand {
    case open(SessionRow, TransportPreference?)
    case rename(SessionRow)
    case toggleMute(SessionRow)
    case copyPath(SessionRow)
    case kill(SessionRow)
    case newSession(HostID)
    case retry(HostID)
    case signIn(HostID)
    case installHelper(HostID)
    case edit(HostID)
}

enum SessionActions {
    /// Without the helper Togo can still kill and rename the tmux sessions it started itself,
    /// which it names with this prefix.
    static let plainSessionPrefix = "togo-"

    private static let changesSession: Set<SessionAction> = [.rename, .mute, .kill]

    static func actions(for row: SessionRow, on machine: MachineSection? = nil) -> SessionActionSet {
        let plain = machine?.helper == .missing
        let changeable: [SessionAction] = if row.managed && !plain {
            [.rename, .mute, .kill]
        } else if plain && row.name.hasPrefix(plainSessionPrefix) {
            [.rename, .kill]
        } else {
            []
        }
        let available = SessionAction.allCases.filter { !changesSession.contains($0) || changeable.contains($0) }

        if changeable.isEmpty {
            // The helper refuses these with -32007 for sessions it did not start.
            return SessionActionSet(available: available, disabled: [], note: ControlCenterCopy.startedOutside)
        }
        if let machine, let reason = unreachableReason(machine) {
            return SessionActionSet(available: available, disabled: Set(changeable), note: reason)
        }
        return SessionActionSet(available: available, disabled: [], note: nil)
    }

    /// Opening still works while a machine is away, because the terminal retries on its own;
    /// changes go through the helper and would fail.
    private static func unreachableReason(_ machine: MachineSection) -> String? {
        switch machine.reachability {
        case .offline: ControlCenterCopy.machineOffline(machine.name)
        case .authNeeded, .failed, .keyMissing: ControlCenterCopy.machineNotConnected(machine.name)
        case .connecting, .waking, .online: nil
        }
    }

    static func killConfirmation(for row: SessionRow) -> (title: String, message: String) {
        (ControlCenterCopy.killTitle(label(row)), ControlCenterCopy.killMessage)
    }

    /// How a session is named in a sentence: "04 Claude Code", or its name without a bib.
    static func label(_ row: SessionRow) -> String {
        guard let bib = row.bib else { return row.name.isEmpty ? row.agentName : row.name }
        return "\(bib) \(row.agentName)"
    }
}
