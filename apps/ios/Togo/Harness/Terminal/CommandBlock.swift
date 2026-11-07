import Foundation
import TogoKit

/// One command of a plain shell with shell integration, as its block shows it: the command
/// line, how it ended, how long it took and the tail of what it printed. Built from a `shell`
/// item, which the helper makes from the shell's OSC 133 prompt marks; a shell without those
/// marks has no items, so it shows no blocks.
struct CommandBlock: Equatable {
    enum Status: Equatable {
        case running
        /// Exit status 0.
        case succeeded
        /// A nonzero exit status.
        case failed(Int)
        /// Over, with no status reported.
        case finished
    }

    let id: String
    let command: String
    let cwd: String?
    let status: Status
    let output: String
    let startedAt: Date?
    let endedAt: Date?

    /// Output longer than this many lines starts collapsed.
    static let foldLines = 12

    init?(_ item: HarnessItem) {
        guard case .shell(let shell) = item.body else { return nil }
        id = item.id
        command = shell.command.trimmingCharacters(in: .whitespacesAndNewlines)
        cwd = shell.cwd
        output = shell.output ?? ""
        startedAt = shell.startedAt.date
        endedAt = shell.endedAt?.date
        if shell.state.isLive {
            status = .running
        } else if let code = shell.exitCode {
            status = code == 0 ? .succeeded : .failed(code)
        } else {
            status = shell.state == .failed ? .failed(1) : .finished
        }
    }

    var isRunning: Bool { status == .running }

    /// The command as the block's title; a blank one reads as the plain word.
    var title: String { command.isEmpty ? TerminalSyncCopy.command : command }

    var lineCount: Int {
        output.isEmpty ? 0 : output.split(separator: "\n", omittingEmptySubsequences: false).count
    }

    var hasOutput: Bool { !output.isEmpty }

    /// Long output starts collapsed, so a screen of blocks stays scannable; short output shows.
    var startsCollapsed: Bool { lineCount > Self.foldLines }

    /// What Copy output puts on the pasteboard: the output, or the command when there is none.
    var copyText: String { hasOutput ? output : command }

    /// Rerun types the command again: only once it is over, and only when there is one.
    var canRerun: Bool { !isRunning && !command.isEmpty }

    /// How long it ran, once over.
    var duration: TimeInterval? {
        guard let startedAt, let endedAt else { return nil }
        return max(0, endedAt.timeIntervalSince(startedAt))
    }

    /// The status in words, for the badge and VoiceOver.
    var statusText: String {
        switch status {
        case .running: TerminalSyncCopy.running
        case .succeeded: TerminalSyncCopy.succeeded
        case .failed(let code): TerminalSyncCopy.failed(code)
        case .finished: TerminalSyncCopy.finished
        }
    }

    /// The machine words beside the command: "exit 2  4s", spaced, never dotted. Empty while it
    /// runs (the spinner says so) and for a quiet success with no time.
    var badge: String {
        var words: [String] = []
        if case .failed(let code) = status { words.append(TerminalSyncCopy.exit(code)) }
        if let duration, duration >= 1 { words.append(ChatCopy.clock(duration)) }
        return words.joined(separator: "  ")
    }

    var spoken: String { TerminalSyncCopy.spokenBlock(title, status: statusText) }
}
