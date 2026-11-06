import UIKit

public enum ShortcutAction: Hashable, Sendable {
    /// Sessions 1 to 9, in the order the session list shows them.
    case session(Int)
    case newSession, controlCenter, detach, paste
    /// ⌃⌘S: shows or hides the session sidebar.
    case toggleSidebar
    /// ⌘\: opens the next tab beside the focused one, or closes the second pane.
    case splitToggle
    /// ⌥⌘← and ⌥⌘→: moves keyboard focus between the panes.
    case focusLeft, focusRight
    /// ⇧⌘[ and ⇧⌘]: the tab before or after the focused one.
    case previousTab, nextTab
    /// ⌃⌘B: shows or hides the board of every session.
    case toggleBoard
    /// ⌃⌘F: hides or shows the workspace's chrome.
    case toggleZen

    @MainActor public init?(command: UIKeyCommand) {
        guard let raw = command.propertyList as? String else { return nil }
        self.init(raw: raw)
    }

    var raw: String {
        switch self {
        case .session(let n): "session.\(n)"
        case .newSession: "newSession"
        case .controlCenter: "controlCenter"
        case .detach: "detach"
        case .paste: "paste"
        case .toggleSidebar: "toggleSidebar"
        case .splitToggle: "splitToggle"
        case .focusLeft: "focusLeft"
        case .focusRight: "focusRight"
        case .previousTab: "previousTab"
        case .nextTab: "nextTab"
        case .toggleBoard: "toggleBoard"
        case .toggleZen: "toggleZen"
        }
    }

    private static let named: [String: ShortcutAction] = Dictionary(
        uniqueKeysWithValues: [
            ShortcutAction.newSession, .controlCenter, .detach, .paste, .toggleSidebar, .splitToggle,
            .focusLeft, .focusRight, .previousTab, .nextTab, .toggleBoard, .toggleZen,
        ].map { ($0.raw, $0) }
    )

    init?(raw: String) {
        if let action = Self.named[raw] {
            self = action
            return
        }
        guard raw.hasPrefix("session."), let n = Int(raw.dropFirst("session.".count)), (1...9).contains(n) else {
            return nil
        }
        self = .session(n)
    }
}

/// Command-key shortcuts only. Ctrl and Option chords without Command belong to the remote
/// program, so no `UIKeyCommand` here ever claims one.
@MainActor public enum HardwareShortcuts {
    public static func commands(action: Selector) -> [UIKeyCommand] {
        let sessions = (1...9).map { n in
            command("Session \(n)", input: "\(n)", .session(n), action)
        }
        return sessions + [
            command("New Session", input: "t", .newSession, action),
            command("Control Center", input: "k", .controlCenter, action),
            command("Detach", input: "w", .detach, action),
            command("Paste", input: "v", .paste, action),
            command("Show or Hide Sidebar", input: "s", modifiers: [.command, .control], .toggleSidebar, action),
            command("Open Beside or Close Pane", input: "\\", .splitToggle, action),
            command("Focus Left Pane", input: UIKeyCommand.inputLeftArrow, modifiers: [.command, .alternate], .focusLeft, action),
            command("Focus Right Pane", input: UIKeyCommand.inputRightArrow, modifiers: [.command, .alternate], .focusRight, action),
            command("Previous Tab", input: "[", modifiers: [.command, .shift], .previousTab, action),
            command("Next Tab", input: "]", modifiers: [.command, .shift], .nextTab, action),
            command("Board", input: "b", modifiers: [.command, .control], .toggleBoard, action),
            command("Zen mode", input: "f", modifiers: [.command, .control], .toggleZen, action),
        ]
    }

    private static func command(
        _ title: String, input: String, modifiers: UIKeyModifierFlags = .command, _ shortcut: ShortcutAction,
        _ action: Selector
    ) -> UIKeyCommand {
        let command = UIKeyCommand(
            title: title, action: action, input: input, modifierFlags: modifiers,
            propertyList: shortcut.raw)
        // Otherwise a focused text view can take ⌘V or ⌘W before the app sees them.
        command.wantsPriorityOverSystemBehavior = true
        return command
    }
}
