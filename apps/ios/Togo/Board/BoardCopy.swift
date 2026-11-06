import Foundation

/// Every sentence the board shows, keyed under `board.` in the string catalog.
enum BoardCopy {
    static let title = String(localized: "board.title", defaultValue: "Board")
    static let list = String(localized: "board.list", defaultValue: "List")
    /// The menu in the toolbar that picks between the list and the board.
    static let view = String(localized: "board.view", defaultValue: "View")
    static let toggle = String(localized: "board.cmd.toggle", defaultValue: "Board")

    static func section(_ kind: BoardSection.Kind) -> String {
        switch kind {
        case .needsYou: String(localized: "board.section.needsYou", defaultValue: "Needs you")
        case .working: String(localized: "board.section.working", defaultValue: "Working")
        case .done: String(localized: "board.section.done", defaultValue: "Done")
        case .idle: String(localized: "board.section.idle", defaultValue: "Idle")
        }
    }

    static let empty = String(localized: "board.empty", defaultValue: "No sessions")
    static let emptyMessage = String(
        localized: "board.empty.message", defaultValue: "Sessions on your machines show up here as cards."
    )
    /// In place of a thumbnail on a machine whose helper cannot capture the screen.
    static let noPreview = String(localized: "board.noPreview", defaultValue: "No preview from this machine")

    // MARK: Keep screen on

    static let keepScreenOn = String(localized: "board.keepScreenOn", defaultValue: "Keep screen on")
    static let lowBattery = String(
        localized: "board.lowBattery", defaultValue: "Screen can sleep again: battery under 20 %. Plug in to keep it on."
    )
    static let wake = String(localized: "board.wake", defaultValue: "Wake the board")
}
