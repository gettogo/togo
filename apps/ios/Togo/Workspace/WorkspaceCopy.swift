import Foundation

/// Every sentence the workspace shows, keyed under `ws.` in the string catalog. Action names
/// shared with the session menu in the control center come from `ControlCenterCopy`.
enum WorkspaceCopy {
    static let sessions = String(localized: "ws.sessions", defaultValue: "Sessions")
    static let actions = String(localized: "ws.actions", defaultValue: "Session actions")
    static let tabs = String(localized: "ws.tabs", defaultValue: "Open sessions")
    /// VoiceOver reads the screen's text as the value, so the label only names what it is.
    static let terminal = String(localized: "ws.terminal", defaultValue: "Terminal")
    /// VoiceOver's name for the terminal row the cursor is on, and for an empty one.
    static let cursorLine = String(localized: "ws.terminal.cursorLine", defaultValue: "Cursor line")
    static let blankLine = String(localized: "ws.terminal.blank", defaultValue: "blank")
    static let closeTab = String(localized: "ws.closeTab", defaultValue: "Close tab")

    static func ended(_ label: String) -> String {
        String(localized: "ws.ended", defaultValue: "\(label) ended")
    }

    static func goTo(_ label: String) -> String {
        String(localized: "ws.goTo", defaultValue: "Go to \(label)")
    }

    // MARK: Link

    static let connecting = String(localized: "ws.link.connecting", defaultValue: "Connecting")
    static let waking = String(localized: "ws.link.waking", defaultValue: "Waking")
    static let roaming = String(localized: "ws.link.roaming", defaultValue: "Roaming")
    static let reconnecting = String(localized: "ws.link.reconnecting", defaultValue: "Reconnecting")
    /// Shown over the terminal when typing goes nowhere because the link is reconnecting.
    static let typingPaused = String(
        localized: "ws.link.typingPaused", defaultValue: "Reconnecting. Typing isn't sent until it's back."
    )
    static let notConnected = String(localized: "ws.link.notConnected", defaultValue: "Not connected")
    static let showFallback = String(localized: "ws.link.showFallback", defaultValue: "Shows why")

    /// Why the link is not on the transport the user asked for: "SSH, Mosh isn't installed on mini".
    static func fallback(transport: String, note: String) -> String {
        String(localized: "ws.link.fallback", defaultValue: "\(transport), \(note)")
    }

    static func connectingTo(_ machine: String) -> String {
        String(localized: "ws.overlay.connecting", defaultValue: "Connecting to \(machine)")
    }

    static func wakingMachine(_ machine: String) -> String {
        String(localized: "ws.overlay.waking", defaultValue: "Waking \(machine)")
    }

    static func failed(_ machine: String) -> String {
        String(localized: "ws.overlay.failed", defaultValue: "Couldn't connect to \(machine)")
    }

    static func offline(_ machine: String) -> String {
        String(localized: "ws.overlay.offline", defaultValue: "\(machine) is offline")
    }

    static let offlineMessage = String(
        localized: "ws.overlay.offlineMessage", defaultValue: "It may be asleep or off the network."
    )
    static func keyChanged(_ machine: String) -> String {
        String(localized: "ws.overlay.keyChanged", defaultValue: "\(machine)'s key changed")
    }

    static let keyChangedMessage = String(
        localized: "ws.overlay.keyChangedMessage",
        defaultValue: "Togo won't connect until you review it. If nobody reinstalled this machine, someone may be intercepting the connection."
    )
    static let reviewKey = String(localized: "ws.overlay.reviewKey", defaultValue: "Review key")

    static func deviceOffline(_ device: String) -> String {
        String(localized: "ws.overlay.deviceOffline", defaultValue: "This \(device) is offline")
    }

    static let deviceOfflineMessage = String(
        localized: "ws.overlay.deviceOfflineMessage", defaultValue: "The terminal reconnects when the network is back."
    )
    static let tryAgain = String(localized: "ws.overlay.tryAgain", defaultValue: "Try again")
    static let signInAgain = String(localized: "ws.overlay.signInAgain", defaultValue: "Sign in again")
    static let machineGone = String(
        localized: "ws.overlay.machineGone", defaultValue: "This machine is no longer in Togo. Add it again to open its sessions."
    )
    static let addMachine = String(localized: "ws.overlay.addMachine", defaultValue: "Add machine")

    static func signInUnfinished(_ machine: String) -> String {
        String(localized: "ws.overlay.signInUnfinished", defaultValue: "\(machine) asked a question to finish signing in.")
    }

    // MARK: Actions

    static let copyLines = String(localized: "ws.copyLines", defaultValue: "Copy last 500 lines")
    static let copied = String(localized: "ws.copied", defaultValue: "Copied the last 500 lines")

    static func copyFailed(_ machine: String) -> String {
        String(localized: "ws.copyFailed", defaultValue: "Couldn't copy from \(machine). Try again.")
    }

    static func killFailed(_ label: String, _ reason: String) -> String {
        String(localized: "ws.killFailed", defaultValue: "Couldn't end \(label): \(reason)")
    }

    static let find = String(localized: "ws.find", defaultValue: "Find")
    static let findPrompt = String(localized: "ws.find.prompt", defaultValue: "Find in terminal")
    static let findNext = String(localized: "ws.find.next", defaultValue: "Next match")
    static let findPrevious = String(localized: "ws.find.previous", defaultValue: "Previous match")
    static let findDone = String(localized: "ws.find.done", defaultValue: "Done")
    static let noMatches = String(localized: "ws.find.none", defaultValue: "No matches")

    // MARK: Requests

    static func more(_ count: Int) -> String {
        String(localized: "ws.incoming.more", defaultValue: "\(count) more")
    }

    static let moreHint = String(localized: "ws.incoming.moreHint", defaultValue: "Opens the list of sessions")
}
