import Foundation
import TogoKit
import TogoTerminal
import UIKit

/// Every side effect the app performs. Views and the store see only this, so each screen
/// state can be built from values in tests and previews.
@MainActor protocol AppServices: AnyObject {
    var hosts: [HostRecordView] { get }
    /// A new stream on every call, one per window's store. It first yields the latest snapshot
    /// of each machine, so a window opened later starts where the others are.
    func snapshots() -> AsyncStream<HostViewSnapshot>
    /// Reconnects every machine now, including one that stopped retrying after a failed sign-in.
    func refresh() async
    func answer(hostID: HostID, requestID: String, decision: Decision) async throws
    func kill(hostID: HostID, sessionID: String) async throws
    func rename(hostID: HostID, sessionID: String, name: String) async throws
    func setMute(hostID: HostID, sessionID: String, muted: Bool) async throws
    func create(hostID: HostID, params: CreateSessionParams) async throws -> SessionRow
    func agents(hostID: HostID) async throws -> [AgentInfo]
    func recentDirs(hostID: HostID) async throws -> [String]
    func listDir(hostID: HostID, path: String) async throws -> ListDirResult
    /// `userStarted` is true when the user opened the tab or tapped Try again. Only such an
    /// attach may ask a sign-in question; automatic ones fail as signed out instead.
    func terminal(hostID: HostID, sessionID: String, transport: TransportPreference?, userStarted: Bool) -> TerminalHandle
    /// The last `lines` of a session's history with wrapped lines joined, as `togo capture` prints it.
    func capture(hostID: HostID, sessionID: String, lines: Int) async throws -> String
    func upload(hostID: HostID, sessionID: String, name: String, data: Data) async throws -> String
    /// Signs in to the machine once, asking `prompter` for a password or code, and has its
    /// monitor use that connection. Background connections never ask.
    func signIn(hostID: HostID, prompter: any ConnectionPrompter) async throws
    func saveHost(_ record: HostRecord) async throws
    func removeHost(_ id: HostID) async throws
    /// The stored record behind a machine, for editing it.
    func hostRecord(_ id: HostID) -> HostRecord?
    /// Questions from attaching terminals, if these services connect to real machines.
    var attachPrompts: AttachPrompts? { get }
    /// True once the stored machines have been read.
    var hostsLoaded: Bool { get }
    var settings: AppSettings { get set }
    /// Whether iOS lets Togo show notifications.
    var pushAuthorization: PushRegistrar.Authorization { get }
    /// Machines this iPhone receives pushes from.
    var pairedMachines: Set<HostID> { get }
    /// Paired machines whose helper last confirmed it sends command text.
    var confirmedCommands: Set<HostID> { get }
    /// Paired machines sent a command text choice they have not confirmed yet.
    var pendingCommands: Set<HostID> { get }
    /// Asks iOS for notification permission, from Settings.
    func turnOnNotifications() async
    /// Removed machines still to be told to stop pushing to this iPhone.
    var pendingRemovals: [PendingRemoval] { get }
    /// Gives up on telling a removed machine, and forgets what was kept to reach it.
    func forgetRemoval(_ id: String)
    /// Whether removing the machine takes this device's key off it.
    func removesKey(_ id: HostID) -> Bool
}

/// A removed machine waiting to unpair, by the helper's host id, or one that may still hold
/// this device's key, by `keyID(_:)`.
struct PendingRemoval: Identifiable, Equatable, Sendable {
    enum Kind: Equatable, Sendable {
        case unpair
        /// This device's key, whose `authorized_keys` line ends in `comment`, may still be on
        /// the machine. `manual`: the helper cannot take it off, so the user does.
        case key(comment: String, manual: Bool)
    }

    var id: String
    var name: String
    var kind: Kind = .unpair

    private static let keyPrefix = "key:"

    static func keyID(_ host: HostID) -> String { keyPrefix + host.uuidString }

    static func hostID(fromKeyID id: String) -> HostID? {
        id.hasPrefix(keyPrefix) ? HostID(uuidString: String(id.dropFirst(keyPrefix.count))) : nil
    }
}

extension AppServices {
    /// Services without a live connection to a machine have nothing to reconnect.
    func refresh() async {}

    func hostRecord(_ id: HostID) -> HostRecord? { nil }

    func removesKey(_ id: HostID) -> Bool { false }

    var attachPrompts: AttachPrompts? { nil }

    var hostsLoaded: Bool { true }

    var pushAuthorization: PushRegistrar.Authorization { .notDetermined }

    var pairedMachines: Set<HostID> { [] }

    var confirmedCommands: Set<HostID> { [] }

    var pendingCommands: Set<HostID> { [] }

    func turnOnNotifications() async {}

    var pendingRemovals: [PendingRemoval] { [] }

    func forgetRemoval(_ id: String) {}
}

/// A running terminal for one tab: the engine's view and the link phases that drive its chrome.
@MainActor final class TerminalHandle {
    let engineView: UIViewController
    let phases: AsyncStream<LinkPhase>
    /// Nil when the engine cannot search its scrollback.
    let search: TerminalSearch?
    /// Command-key shortcuts the engine sees while it has focus, such as ⌘1 to ⌘9.
    var onShortcut: (ShortcutAction) -> Void = { _ in }
    /// A two-finger swipe across the terminal: +1 for the next tab, -1 for the previous one.
    var onSwipe: (Int) -> Void = { _ in }
    /// A swipe in from the left edge, back to the sessions list.
    var onEdgeSwipe: () -> Void = {}
    /// The point size a pinch settled on.
    var onFontSize: (Double) -> Void = { _ in }
    /// The key bar's Compose key.
    var onCompose: () -> Void = {}
    /// The key bar's Dictate key.
    var onDictate: () -> Void = {}
    /// Hides the key bar's Compose and Dictate keys while zen mode keeps the compose bar away.
    var hidesComposeKeys = false {
        didSet { if hidesComposeKeys != oldValue { onHidesComposeKeys(hidesComposeKeys) } }
    }
    var onHidesComposeKeys: (Bool) -> Void = { _ in }
    /// An image pasted into the terminal, as the pasteboard encoded it.
    var onPasteImage: (Data) -> Void = { _ in }
    /// Text into the session as a paste, and the Enter key, for the compose bar.
    var paste: (String) -> Void = { _ in }
    var pressEnter: () -> Void = {}
    private let onStop: @MainActor () -> Void

    init(
        engineView: UIViewController, phases: AsyncStream<LinkPhase>, search: TerminalSearch? = nil,
        onStop: @escaping @MainActor () -> Void
    ) {
        self.engineView = engineView
        self.phases = phases
        self.search = search
        self.onStop = onStop
    }

    func stop() {
        onStop()
    }
}

/// Find in a terminal's scrollback. `find` answers whether anything matched.
struct TerminalSearch {
    let find: @MainActor (_ text: String, _ backwards: Bool) -> Bool
    let clear: @MainActor () -> Void
}

enum ServiceErrors {
    static func isAlreadyAnswered(_ error: any Error) -> Bool {
        if case .conflict? = error as? HelperError {
            return true
        }
        return false
    }

    /// What the person reads for a failure. Errors TogoKit and the app word themselves say
    /// "machine" here; anything else, such as a POSIX or URL error, gets a plain fallback.
    static func message(_ error: any Error) -> String {
        if let text = ServiceErrorCopy.known(error) { return text }
        if error is any LocalizedError { return error.localizedDescription }
        return ServiceErrorCopy.fallback
    }
}

enum ServiceErrorCopy {
    static let fallback = String(localized: "error.fallback", defaultValue: "Something went wrong. Try again.")

    static func known(_ error: any Error) -> String? {
        switch error {
        case let error as HelperError: helper(error)
        case let error as SSHError: ssh(error)
        case let error as MoshBootstrapError: mosh(error)
        default: nil
        }
    }

    private static func helper(_ error: HelperError) -> String? {
        switch error {
        case .timeout:
            String(localized: "error.helper.timeout", defaultValue: "The Togo helper on the machine didn't answer in time.")
        case .closed:
            String(localized: "error.helper.closed", defaultValue: "The connection to the Togo helper on the machine closed.")
        case .notReady:
            String(localized: "error.helper.notReady", defaultValue: "The Togo helper on the machine isn't ready yet.")
        case .unsupported:
            String(
                localized: "error.helper.unsupported",
                defaultValue: "The Togo helper on the machine is too old for this. Update it there."
            )
        case .malformedResponse:
            String(
                localized: "error.helper.malformed",
                defaultValue: "The Togo helper on the machine sent a reply that couldn't be read."
            )
        default: nil
        }
    }

    private static func ssh(_ error: SSHError) -> String? {
        switch error {
        case .hostKeyRejected:
            String(localized: "error.ssh.keyRejected", defaultValue: "The machine's key wasn't trusted, so the connection was closed.")
        case .authFailed(let tried) where tried.isEmpty:
            String(localized: "error.ssh.authFailed", defaultValue: "The machine didn't accept the login.")
        case .authFailed:
            String(localized: "error.ssh.authFailedTried", defaultValue: "The machine didn't accept the login. Check your key or password.")
        case .timeout:
            String(localized: "error.ssh.timeout", defaultValue: "The machine took too long to answer.")
        case .channelClosed(let status) where status != nil:
            String(localized: "error.ssh.commandStopped", defaultValue: "The command on the machine stopped.")
        default: nil
        }
    }

    private static func mosh(_ error: MoshBootstrapError) -> String {
        switch error {
        case .serverMissing:
            String(
                localized: "error.mosh.missing",
                defaultValue: "Mosh isn't installed on the machine. Install it there, or connect with SSH."
            )
        case .noConnectLine:
            String(
                localized: "error.mosh.noStart",
                defaultValue: "Mosh didn't start on the machine. A missing UTF-8 locale is a common cause."
            )
        case .malformedKey:
            String(localized: "error.mosh.badKey", defaultValue: "Mosh sent a session key that couldn't be read.")
        }
    }
}

// TogoKit stores the key bar and imported themes as bytes so it does not depend on the
// terminal. Settings outlive app versions and arrive from other devices through sync, so bytes
// that no longer decode read as the default, and one bad theme is dropped rather than all.
extension AppSettings {
    var keyBarConfig: KeyBarConfig {
        get { keyBar.flatMap { try? JSONDecoder().decode(KeyBarConfig.self, from: $0) } ?? .standard }
        set { keyBar = try? Self.encoder.encode(newValue) }
    }

    var customThemes: [TerminalTheme] {
        get {
            guard let importedThemes,
                  let list = try? JSONDecoder().decode([Lenient<TerminalTheme>].self, from: importedThemes) else { return [] }
            return list.compactMap(\.value)
        }
        set { importedThemes = newValue.isEmpty ? nil : try? Self.encoder.encode(newValue) }
    }

    /// Sorted keys, so the same value always encodes to the same bytes and compares equal.
    private static var encoder: JSONEncoder {
        let encoder = JSONEncoder()
        encoder.outputFormatting = .sortedKeys
        return encoder
    }
}

/// An array element that decodes to nil instead of failing its array.
private struct Lenient<Value: Decodable>: Decodable {
    var value: Value?

    init(from decoder: any Decoder) throws {
        value = try? Value(from: decoder)
    }
}
