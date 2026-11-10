import SwiftUI
import TogoKit
import TogoTerminal

/// The terminal theme's background and the appearance chrome needs over it. What is drawn
/// around a terminal (the gaps beside and under the grid, the space behind the top bar, the
/// panes' base) takes the theme's background rather than the app's, so the terminal reads as
/// one full-bleed surface; the bar and the rows above the terminal take the theme's appearance,
/// so a dark terminal in light mode gets dark chrome from the system, not hard-coded colours.
struct TerminalBackdrop: Equatable {
    var background: Color
    var scheme: ColorScheme

    init(theme: TerminalTheme) {
        background = Color(terminalHex: theme.background)
        scheme = theme.isDark ? .dark : .light
    }

    /// The theme the terminal shows for `settings` under the system appearance `system`: the
    /// chosen theme, or for "Match System Appearance" the one for that appearance.
    static func resolve(_ settings: AppSettings, system: ColorScheme) -> TerminalBackdrop {
        TerminalBackdrop(theme: ThemeChoice.current(settings).theme(for: system))
    }

    /// The appearance the top bar and the status bar render in over `backdrop`: always a scheme,
    /// never nil. A nil `toolbarColorScheme` does not undo an earlier dark one, so after the
    /// terminal gave way to the chat the title and status bar stayed white on the light canvas.
    static func barScheme(_ backdrop: TerminalBackdrop?, system: ColorScheme) -> ColorScheme {
        backdrop?.scheme ?? system
    }
}

extension AppStore {
    /// `id`'s pane shows its terminal: neither the chat nor the loader covers it.
    func showsTerminal(_ id: TabID?) -> Bool {
        id.map { layer(for: $0) == .terminal } ?? false
    }

    /// The backdrop behind the top bar and the rows under it, or nil where app chrome meets the
    /// bar: a chat, the loader, or two panes under their own headers.
    func barBackdrop(system: ColorScheme) -> TerminalBackdrop? {
        guard !panesVisible, showsTerminal(focusedTab?.id) else { return nil }
        return .resolve(services.settings, system: system)
    }
}

/// The workspace's canvas and its top bar's appearance: the terminal theme's while the one pane
/// on screen shows its terminal, the app's otherwise. The bar keeps its system material and
/// scroll edge effect; only the appearance it renders in follows the canvas under it, both ways,
/// on the horizontal bar, a Duo's vertical bar and the detail column beside the sidebar.
struct WorkspaceBackdrop: ViewModifier {
    @Environment(\.colorScheme) private var system
    let store: AppStore

    func body(content: Content) -> some View {
        let backdrop = store.barBackdrop(system: system)
        content
            .background(backdrop?.background ?? Color.togoInk)
            .toolbarColorScheme(TerminalBackdrop.barScheme(backdrop, system: system), for: .navigationBar)
    }
}

/// The rows between the top bar and the terminal (the tab strip, the find field) sit on the
/// workspace's canvas, so they take its appearance: semantic colours stay readable on a dark
/// terminal in light mode and the other way round.
struct WorkspaceRowsAppearance: ViewModifier {
    @Environment(\.colorScheme) private var system
    let store: AppStore

    func body(content: Content) -> some View {
        content.environment(\.colorScheme, TerminalBackdrop.barScheme(store.barBackdrop(system: system), system: system))
    }
}
