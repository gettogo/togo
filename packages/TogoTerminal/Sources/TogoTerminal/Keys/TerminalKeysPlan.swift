import Foundation

/// A key the waiting prompt wants, as the row shows it: an answer word, or ↑ ↓ ⏎ for a menu.
public struct PromptKey: Hashable, Sendable, Identifiable {
    public enum Kind: Hashable, Sendable {
        /// Answers the request; the host decides what it sends.
        case answer
        /// Shows the whole request.
        case more
        /// A terminal key, for menus the agent draws itself.
        case stroke(KeyStroke)
    }

    public var id: String
    public var label: String
    public var symbol: String?
    /// The first option: drawn filled.
    public var filled: Bool
    public var kind: Kind

    public init(id: String, label: String, symbol: String? = nil, filled: Bool = false, kind: Kind) {
        self.id = id
        self.label = label
        self.symbol = symbol
        self.filled = filled
        self.kind = kind
    }
}

/// What the host knows that decides where the keys go. Read from the system, never the device:
/// the bar's edge, the size class, the keyboard guide and the focus.
public struct TerminalKeysSituation: Equatable, Sendable {
    /// The system draws the pane's bar vertically (an iPhone Duo).
    public var verticalBar = false
    /// Compact height: an iPhone on its side.
    public var compactHeight = false
    public var hardwareKeyboard = false
    /// A software keyboard is up and the terminal holds it.
    public var typing = false
    /// The session's "Keep keys on screen".
    public var keep = false
    public var promptKeys: [PromptKey] = []
    public var shortcuts: [KeyBarItem] = []
    public var config = TerminalKeysConfig()
    /// The agent's Mode key exists; without one the column and row leave it out.
    public var hasMode = true

    public init() {}
}

/// What to show, derived from a situation. The column stands beside the terminal only where
/// no vertical bar can hold the controller; the row above the keyboard is drawn only while it
/// has something (a software keyboard's Hide key counts); the controller goes to the bar where
/// there is one.
public struct TerminalKeysPlan: Equatable, Sendable {
    public struct Column: Equatable, Sendable {
        /// The keys above the controller.
        public var keys: [KeyRef]
    }

    public enum RowItem: Hashable, Sendable {
        case key(KeyRef)
        case prompt(PromptKey)
        case shortcut(KeyBarItem)
    }

    public var column: Column?
    public var row: [RowItem]
    /// The controller is the bar's own capsule.
    public var controllerInBar: Bool

    public var isEmpty: Bool { column == nil && row.isEmpty && !controllerInBar }

    public static func plan(_ s: TerminalKeysSituation) -> TerminalKeysPlan {
        let fixed = KeyRef.column.filter { s.hasMode || $0 != .mode }
        var column: Column?
        if !s.verticalBar {
            let keep = s.keep && s.config.showColumn == .alsoOnDemand
            if s.hardwareKeyboard {
                if keep { column = Column(keys: []) }
            } else if s.typing || keep {
                column = Column(keys: s.compactHeight ? KeyRef.shortColumn : fixed)
            }
        }
        var row: [RowItem] = []
        if s.verticalBar, s.typing, !s.hardwareKeyboard {
            row += fixed.map(RowItem.key)
        }
        if s.config.promptKeys {
            row += s.promptKeys.map(RowItem.prompt)
        }
        row += s.shortcuts.map(RowItem.shortcut)
        // A software keyboard always has its way down at the row's trailing end, apart from
        // the keys; it is reason enough to draw the row.
        if s.typing, !s.hardwareKeyboard {
            row.append(.key(.hideKeyboard))
        }
        return TerminalKeysPlan(column: column, row: row, controllerInBar: s.verticalBar)
    }
}
