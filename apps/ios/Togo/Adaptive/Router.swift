import Foundation

/// Where a window is in the app's one navigation tree, as a value: what the stacks have pushed,
/// whether a session is up, the status display, and the user's sidebar choice. The store keeps
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
    /// The status display: the roster across the window.
    var monitorShown = false
    var sidebar = SidebarState()
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
