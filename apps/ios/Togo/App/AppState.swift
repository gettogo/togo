import CoreGraphics
import Foundation
import TogoKit

typealias HostID = UUID

enum HostSymbol: String, Codable, CaseIterable, Sendable {
    case laptop, desktop, server, mini, cloud, raspberry

    var systemImage: String {
        switch self {
        case .laptop: "laptopcomputer"
        case .desktop: "desktopcomputer"
        case .server: "server.rack"
        case .mini: "macmini"
        case .cloud: "cloud"
        case .raspberry: "cpu"
        }
    }
}

/// `live` means the helper advertises the events capability, so rows update without polling.
enum HelperState: Equatable, Sendable {
    case checking
    case present(version: String, live: Bool)
    case missing
    case outdated(found: String)
}

enum Reachability: Equatable, Sendable {
    case connecting, waking, online
    case offline(since: Date, reason: String)
    case authNeeded
    case failed(String)
    /// The machine signs in with a key that is not on this device.
    case keyMissing
}

struct SessionRow: Identifiable, Equatable, Sendable {
    var id: String
    var hostID: HostID
    var bib: String?
    var name: String
    var agent: String
    var agentName: String
    var status: SessionStatus
    var activity: String?
    var cwd: String
    var muted: Bool
    var managed: Bool
    var meta: SessionMeta
    var statusSince: Date
    /// How the agent stopped while `status` is `ended` or `failed`.
    var exit: SessionExit? = nil
    /// Recent output volume, 0...1 oldest first, up to 16 values; empty when unknown.
    var activityHistory: [Double] = []
}

extension SessionStatus {
    /// The agent is no longer running: it finished its turn and the session ended, or it
    /// exited, crashed or stopped on a limit. These rows archive and count as done.
    var isFinished: Bool {
        switch self {
        case .done, .exited, .ended, .failed: true
        case .working, .needsYou, .idle, .unknown: false
        }
    }
}

struct RequestCard: Identifiable, Equatable, Sendable {
    var id: String
    var hostID: HostID
    var sessionID: String?
    var bib: String?
    var agentName: String
    var title: String
    var detail: String?
    var options: [ApprovalOption]
    var allowText: Bool
    var phase: Phase

    enum Phase: Equatable, Sendable {
        case open
        case sending(Decision)
        case done(Outcome)
    }

    enum Outcome: Equatable, Sendable {
        case approved, denied, answeredElsewhere, expired
    }
}

struct MachineSection: Identifiable, Equatable, Sendable {
    var id: HostID
    var name: String
    var symbol: HostSymbol
    var reachability: Reachability
    var helper: HelperState
    var sessions: [SessionRow]
    var lastSeen: Date?
    /// What the machine's helper offers, such as `upload.v1`. Empty without a helper.
    var capabilities: Set<String> = []
    /// Sessions a restart of the machine ended, offered back under its live ones. Loaded once
    /// per helper connection when the helper advertises `restore.v1`.
    var restorable: [RestorableSession] = []
    /// Whether the machine is being kept awake; nil without `awake.v1`.
    var awake: AwakeStatus?
    /// Where to send a wake packet for this machine, if it ever reported one.
    var wake: WakeTarget?
}

enum SessionFilter: String, CaseIterable, Sendable {
    case all, needsYou, working, done

    func admits(_ status: SessionStatus) -> Bool {
        switch self {
        case .all: true
        case .needsYou: status == .needsYou
        case .working: status == .working
        case .done: status.isFinished
        }
    }
}

struct ControlCenterState: Equatable, Sendable {
    var requests: [RequestCard] = []
    var machines: [MachineSection] = []
    var archived: [SessionRow] = []
    var loaded = false
}

/// A tab's identity. Helper and tmux session ids are unique only on their own machine, so a
/// tab is always named by both.
struct TabID: Hashable, Sendable {
    var hostID: HostID
    var sessionID: String
}

struct WorkspaceTab: Identifiable, Equatable, Sendable {
    var id: TabID { TabID(hostID: hostID, sessionID: sessionID) }
    var hostID: HostID
    var sessionID: String
    var bib: String?
    var title: String
    var link: LinkPhase
    /// Set when the user picked a transport for this tab; nil follows the machine's preference.
    var transport: TransportPreference? = nil
    /// The agent's name as last listed, so the header still names it while the machine is away.
    var agent: String? = nil
}

enum LinkPhase: Equatable, Sendable {
    case waking(String)
    case connecting
    case live(transport: String, note: String?)
    case roaming
    case reconnecting(Int)
    case failed(String)
    /// The machine asked a question to finish signing in and it was not answered.
    case signedOut
}

struct WorkspaceState: Equatable, Sendable {
    var tabs: [WorkspaceTab] = []
    var selected: TabID?
    /// "04 ended", shown in the workspace when the session on screen went away underneath it.
    var ended: String?
    /// The tab beside the selected one. It stays here while the window is too narrow to show it.
    var secondary: TabID?
    /// The pane that takes keyboard shortcuts, find, compose and dictation.
    var focused: Pane = .primary
    /// The primary pane's share of the width when the panes sit side by side.
    var primaryFraction: CGFloat = 0.5
}

enum AppRoute: Hashable, Sendable {
    case settings
    /// Finished sessions, from the list or the sidebar.
    case archive
    case settingsPage(SettingsPage)
}

enum AppSheet: Identifiable, Hashable, Sendable {
    case addMachine
    /// The add-machine form, already filled in.
    case addMachineFilled(MachineDraft)
    case editMachine(HostID)
    case newSession(HostID?)
    case signIn(HostID)
    case review(ReviewSubject, ReviewTab)
    /// Settings beside the sidebar, where pushing it would cover the session in the detail.
    case settings

    var id: Self { self }
}

enum SettingsPage: Hashable, Sendable {
    case terminal, keyboard, shortcuts, gestures, dictation, machines, notifications, sync, privacy, diagnostics, about
}

/// One machine's complete view at `at`. Applying it replaces everything the store knew about
/// that machine, so a missed event can never leave a ghost row behind.
struct HostViewSnapshot: Equatable, Sendable {
    var hostID: HostID
    var reachability: Reachability
    var helper: HelperState
    var sessions: [SessionRow]
    var requests: [RequestCard]
    var at: Date
    var capabilities: Set<String> = []
    var awake: AwakeStatus?
    var wake: WakeTarget?
    /// Requests the machine closed, with what their cards should say as they go.
    var closed: [String: RequestCard.Outcome] = [:]
}

/// Inline banner text. There are no toasts: a notice stays until the next action replaces it.
struct Notice: Equatable, Sendable {
    var text: String
    var isError: Bool
}

struct HostRecordView: Identifiable, Equatable, Sendable {
    var id: HostID
    var name: String
    var symbol: HostSymbol
    var address: String
    var etAvailable = false
}
