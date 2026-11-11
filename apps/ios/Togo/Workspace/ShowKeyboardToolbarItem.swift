import SwiftUI
import TogoTerminal

/// Show keyboard in the bar while the terminal layer is up and nothing types into it: the way
/// to the keyboard that is always in sight. The terminal's own keys (with Hide keyboard) only
/// show while typing, so the first step needs a place of its own; a tap on the terminal does
/// the same. Gone again once the terminal types, by the software keyboard or a hardware one.
struct ShowKeyboardToolbarItem: ToolbarContent {
    let handle: TerminalHandle

    /// Nil unless the tab shows its terminal and the terminal is not typing. Holding focus is
    /// not typing: a terminal can keep focus while its keyboard went away.
    @MainActor init?(store: AppStore, terminals: TerminalPool, tab: TabID) {
        let watch = KeyboardWatch.shared
        let typing = watch.terminalTyping && (watch.dockedKeyboard || KeyboardPresence.shared.hardwareKeyboardConnected)
        guard store.layer(for: tab) == .terminal, !typing, let handle = terminals.handle(for: tab) else { return nil }
        self.handle = handle
    }

    var body: some ToolbarContent {
        ToolbarItem(placement: .primaryAction) {
            Button(WorkspaceCopy.showKeyboard, systemImage: "keyboard") { handle.raiseKeyboard() }
                .accessibilityIdentifier("workspace.showKeyboard")
        }
    }
}

extension WorkspaceCopy {
    static let showKeyboard = String(localized: "workspace.showKeyboard", defaultValue: "Show keyboard")
}
