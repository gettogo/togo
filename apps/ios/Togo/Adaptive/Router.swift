import Foundation

/// Where a window is in the app's one navigation tree, as a value: what the stacks have pushed,
/// whether a session is up, and the user's sidebar choice. The store keeps
/// it across a fold, unfold or resize (the scene stays); the window saves it with its tabs
/// (`WindowRestoration`) so a relaunch comes back to the same place.
///
/// Foundation only, so any target can compile it. The routing calls that change it live on
/// `AppStore` (`ShellRouting.swift`, `AppCommands.swift`).
struct Router: Codable, Equatable, Sendable {
    /// Pushed over the roster in a compact window, over the detail column in a regular one.
    var path: [AppRoute] = []
    /// A session is on screen: the cover in a compact window, the detail column in a regular one.
    var workspaceOpen = false
    var sidebar = SidebarState()

    init(path: [AppRoute] = [], workspaceOpen: Bool = false, sidebar: SidebarState = SidebarState()) {
        self.path = path
        self.workspaceOpen = workspaceOpen
        self.sidebar = sidebar
    }

    private enum CodingKeys: String, CodingKey {
        case path, workspaceOpen, sidebar
    }

    /// Saved by any version, so no saved value may stop a launch: a key it no longer knows (the
    /// removed status display's `monitorShown`) is ignored, a route that no longer decodes drops
    /// out of the path, and a field that fails takes its default.
    init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = (try? c.decodeIfPresent([Lenient<AppRoute>].self, forKey: .path))?.compactMap(\.value) ?? []
        workspaceOpen = (try? c.decodeIfPresent(Bool.self, forKey: .workspaceOpen)) ?? false
        sidebar = (try? c.decodeIfPresent(SidebarState.self, forKey: .sidebar)) ?? SidebarState()
    }
}

/// One element of a saved list, nil when it no longer decodes, so one stale value costs only
/// itself.
struct Lenient<Value: Decodable>: Decodable {
    let value: Value?

    init(from decoder: any Decoder) throws {
        value = try? Value(from: decoder)
    }
}

enum AppRoute: Hashable, Codable, Sendable {
    case settings
    /// Finished sessions, from the roster's overflow.
    case archive
    case settingsPage(SettingsPage)
}

enum SettingsPage: Hashable, Codable, Sendable {
    case terminal, keyboard, shortcuts, gestures, dictation, machines, notifications, sync, privacy, diagnostics, about
}

/// The sidebar column of a regular-width window. Only the user hides it (the split view's
/// toggle or ⌃⌘S); the app never hides it to make room. Only `hidden` is saved.
struct SidebarState: Codable, Equatable, Sendable {
    /// The user's choice; nil until they made one, which shows the sidebar.
    var hidden: Bool?
    /// Bumped by ⌘K.
    var searchRequests = 0
    /// A ⌘K no roster has taken yet. A roster that comes on screen after it (the sidebar
    /// shown by the same ⌘K) takes it when it appears, so the request is never lost.
    var searchPending = false

    var isHidden: Bool { hidden ?? false }

    mutating func toggle() {
        hidden = !isHidden
    }

    private enum CodingKeys: String, CodingKey {
        case hidden
    }
}
