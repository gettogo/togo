import Foundation

/// The words of subagent chats, background terminals, the capture sheet and command blocks.
enum TerminalSyncCopy {
    // MARK: Subagents

    static let subagent = String(localized: "sync.subagent", defaultValue: "Subagent")
    static let mainChat = String(localized: "sync.subagent.main", defaultValue: "Main chat")
    static let subagentEmpty = String(localized: "sync.subagent.empty", defaultValue: "Nothing yet")
    static let subagentEmptyDetail = String(
        localized: "sync.subagent.empty.detail", defaultValue: "What this agent does shows here as it works."
    )

    static func back(to name: String) -> String {
        String(localized: "sync.subagent.back", defaultValue: "Back to \(name)")
    }

    /// "Researcher  Running" under a subagent's title: role and state, spaced, no dots.
    static func agentLine(role: String?, state: String) -> String {
        [role, state].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: "  ")
    }

    // MARK: Background

    static let openTerminal = String(localized: "sync.background.open", defaultValue: "Open terminal")
    static let openTerminalHint = String(
        localized: "sync.background.open.hint", defaultValue: "Opens a live terminal on this process"
    )
    static let showOutput = String(localized: "sync.background.output", defaultValue: "Show output")
    static let noOutput = String(localized: "sync.background.noOutput", defaultValue: "No output yet")
    static let backgroundGone = String(
        localized: "sync.background.gone", defaultValue: "This process is no longer listed."
    )

    // MARK: Capture sheet

    static let loading = String(localized: "sync.capture.loading", defaultValue: "Reading the terminal")
    static let captureFailed = String(localized: "sync.capture.failed", defaultValue: "Couldn't read the terminal")
    static let tryAgain = String(localized: "sync.capture.retry", defaultValue: "Try again")
    static let copyAll = String(localized: "sync.capture.copy", defaultValue: "Copy all")
    static let done = String(localized: "sync.capture.done", defaultValue: "Done")
    static let marked = String(localized: "sync.capture.marked", defaultValue: "Marked line")
    /// The history sheet's title: the lines come from the machine, not the phone.
    static let fromMachine = String(localized: "sync.capture.note", defaultValue: "From the machine")

    // MARK: Command blocks

    static let command = String(localized: "sync.block.command", defaultValue: "Command")
    static let copyOutput = String(localized: "sync.block.copyOutput", defaultValue: "Copy output")
    static let copyCommand = String(localized: "sync.block.copyCommand", defaultValue: "Copy command")
    static let rerun = String(localized: "sync.block.rerun", defaultValue: "Rerun")
    static let collapse = String(localized: "sync.block.collapse", defaultValue: "Collapse")
    static let expand = String(localized: "sync.block.expand", defaultValue: "Expand")
    static let copied = String(localized: "sync.block.copied", defaultValue: "Copied")
    static let rerunFailed = String(localized: "sync.block.rerunFailed", defaultValue: "Couldn't run it again")

    static func exit(_ code: Int) -> String {
        String(localized: "sync.block.exit", defaultValue: "exit \(code)")
    }

    static func lines(_ n: Int) -> String {
        String(localized: "sync.block.lines", defaultValue: "\(n) lines")
    }

    static func spokenBlock(_ command: String, status: String) -> String {
        String(localized: "sync.block.spoken", defaultValue: "Command \(command), \(status)")
    }

    static let running = String(localized: "sync.block.running", defaultValue: "Running")
    static let succeeded = String(localized: "sync.block.ok", defaultValue: "Succeeded")
    static let finished = String(localized: "sync.block.finished", defaultValue: "Finished")

    static func failed(_ code: Int) -> String {
        String(localized: "sync.block.failed", defaultValue: "Failed with exit \(code)")
    }
}
