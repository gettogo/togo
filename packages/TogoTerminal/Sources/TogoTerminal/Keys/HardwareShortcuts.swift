import UIKit

public enum ShortcutAction: Hashable, Sendable {
    /// Sessions 1 to 9, in the order the session list shows them.
    case session(Int)
    case newSession, controlCenter, detach, paste

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
        }
    }

    init?(raw: String) {
        switch raw {
        case "newSession": self = .newSession
        case "controlCenter": self = .controlCenter
        case "detach": self = .detach
        case "paste": self = .paste
        default:
            guard raw.hasPrefix("session."), let n = Int(raw.dropFirst("session.".count)), (1...9).contains(n) else {
                return nil
            }
            self = .session(n)
        }
    }
}

/// Command-key shortcuts only. Ctrl and Option chords belong to the remote program, so no
/// `UIKeyCommand` here ever claims one.
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
        ]
    }

    private static func command(_ title: String, input: String, _ shortcut: ShortcutAction, _ action: Selector) -> UIKeyCommand {
        let command = UIKeyCommand(
            title: title, action: action, input: input, modifierFlags: .command,
            propertyList: shortcut.raw)
        // Otherwise a focused text view can take ⌘V or ⌘W before the app sees them.
        command.wantsPriorityOverSystemBehavior = true
        return command
    }
}
