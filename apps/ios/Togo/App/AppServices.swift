import Foundation
import TogoKit
import TogoTerminal
import UIKit

/// Every side effect the app performs. Views and the store see only this, so each screen
/// state can be built from values in tests and previews.
@MainActor protocol AppServices: AnyObject {
    var hosts: [HostRecordView] { get }
    /// Single consumer: `AppStore.run()`.
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
}

extension AppServices {
    /// Services without a live connection to a machine have nothing to reconnect.
    func refresh() async {}

    func hostRecord(_ id: HostID) -> HostRecord? { nil }

    var attachPrompts: AttachPrompts? { nil }

    var hostsLoaded: Bool { true }
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

    static func message(_ error: any Error) -> String {
        error.localizedDescription
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
