import Foundation
import TogoKit

/// The chat's words. Machine words (the work line, durations, counts) are drawn in SF Mono by
/// the views; these only say them.
enum ChatCopy {
    static let chat = String(localized: "chat.layer.chat", defaultValue: "Chat")
    static let terminal = String(localized: "chat.layer.terminal", defaultValue: "Terminal")
    static let showChatHint = String(localized: "chat.layer.chat.hint", defaultValue: "Shows the conversation")
    static let showTerminalHint = String(localized: "chat.layer.terminal.hint", defaultValue: "Shows the live terminal")
    static let fallback = String(
        localized: "chat.fallback", defaultValue: "Chat isn't available for this session right now. Showing the terminal."
    )

    static let reading = String(localized: "chat.reading", defaultValue: "Reading the conversation")
    static let reconnecting = String(localized: "chat.reconnecting", defaultValue: "Reconnecting")
    static let emptyTitle = String(localized: "chat.empty.title", defaultValue: "Nothing said yet")
    static let emptyDetail = String(localized: "chat.empty.detail.short", defaultValue: "Write below to start.")
    static let earlier = String(localized: "chat.earlier", defaultValue: "Show earlier")

    static let copy = String(localized: "chat.copy", defaultValue: "Copy")
    static let showInTerminal = String(localized: "chat.showInTerminal", defaultValue: "Show in terminal")
    static let quote = String(localized: "chat.quote", defaultValue: "Quote in reply")
    static let expand = String(localized: "chat.expand", defaultValue: "Expand")
    static let collapse = String(localized: "chat.collapse", defaultValue: "Collapse")
    static let collapsed = String(localized: "chat.collapsed", defaultValue: "collapsed")
    static let expanded = String(localized: "chat.expanded", defaultValue: "expanded")
    static let done = String(localized: "chat.done", defaultValue: "Done")
    static let details = String(localized: "chat.details", defaultValue: "Details")

    static let rotorPrompts = String(localized: "chat.rotor.prompts", defaultValue: "Prompts")
    static let rotorNeeds = String(localized: "chat.rotor.needs", defaultValue: "Needs you")
    static let rotorReplies = String(localized: "chat.rotor.replies", defaultValue: "Replies")

    // MARK: Prompts

    static let queued = String(localized: "chat.prompt.queued", defaultValue: "Queued")
    static let sending = String(localized: "chat.prompt.sending", defaultValue: "Sending")
    static let notSent = String(localized: "chat.prompt.notSent", defaultValue: "Not sent")
    static let cancel = String(localized: "chat.prompt.cancel", defaultValue: "Cancel")
    static let tryAgain = String(localized: "chat.prompt.retry", defaultValue: "Try again")
    static let fromDesk = String(localized: "chat.prompt.desk", defaultValue: "Typed at the desk")

    /// "Opening OMP": a session on its way to its chat or terminal.
    static func opening(_ agent: String) -> String {
        agent.isEmpty
            ? String(localized: "chat.opening", defaultValue: "Opening")
            : String(localized: "chat.opening.agent", defaultValue: "Opening \(agent)")
    }

    /// "Ran /compact": a command sent from the composer.
    static func ran(_ line: String) -> String {
        String(localized: "chat.prompt.ran", defaultValue: "Ran \(line)")
    }

    static func spokenPrompt(_ text: String) -> String {
        String(localized: "chat.prompt.spoken", defaultValue: "You: \(text)")
    }

    static func spokenReply(_ text: String) -> String {
        String(localized: "chat.reply.spoken", defaultValue: "Agent: \(text)")
    }

    // MARK: Needs

    static let approve = String(localized: "chat.needs.approve", defaultValue: "Approve")
    static let deny = String(localized: "chat.needs.deny", defaultValue: "Deny")
    static let more = String(localized: "chat.needs.more", defaultValue: "More")
    static let send = String(localized: "chat.needs.send", defaultValue: "Send")
    static let answerPlaceholder = String(localized: "chat.needs.answer", defaultValue: "Your answer")
    static let approved = String(localized: "chat.needs.approved", defaultValue: "Approved")
    static let denied = String(localized: "chat.needs.denied", defaultValue: "Denied")
    static let answered = String(localized: "chat.needs.answered", defaultValue: "Answered")
    static let answeredElsewhere = String(localized: "chat.needs.elsewhere", defaultValue: "Answered at the desk")
    static let expired = String(localized: "chat.needs.expired", defaultValue: "No longer waiting")
    static let waitingInTerminal = String(
        localized: "chat.needs.inTerminal", defaultValue: "Waiting for an answer in the terminal"
    )
    static let answerFailed = String(localized: "chat.needs.failed", defaultValue: "Couldn't send the answer. Try again.")
    /// The card cannot be answered from the phone: its answers live in the terminal.
    static let answerInTerminal = String(localized: "chat.needs.answerInTerminal", defaultValue: "Answer in the terminal")
    /// The answer reached the terminal but the harness could not take it from the phone.
    static let finishInTerminal = String(localized: "chat.needs.finishInTerminal", defaultValue: "Finish this in the terminal")
    static let otherAnswer = String(localized: "chat.needs.other", defaultValue: "Other answer")
    static let questionChanged = String(
        localized: "chat.needs.changed", defaultValue: "The question changed. Check your answer."
    )
    static let jumpToNeeds = String(localized: "chat.needs.jump", defaultValue: "Needs you, below")
    static let jumpToNeedsHint = String(localized: "chat.needs.jump.hint", defaultValue: "Scrolls to the request")

    // MARK: Links and text

    static let selectText = String(localized: "chat.selectText", defaultValue: "Select text")
    static let openLink = String(localized: "chat.link.open", defaultValue: "Open")

    static func spokenNeeds(_ title: String) -> String {
        String(localized: "chat.needs.spoken", defaultValue: "Needs you: \(title)")
    }

    static func resolved(_ how: String?) -> String {
        switch how {
        case "phone", "desk": answered
        case "expired", "gone": expired
        default: answered
        }
    }

    // MARK: Work line

    /// "Worked 1m 12s  6 steps  2 files changed": words separated by two spaces, no dots.
    static func workLine(_ summary: WorkSummary, now: Date) -> String {
        var parts: [String] = []
        let time = summary.elapsed(now: now).map(clock)
        if summary.running {
            parts.append(time.map { String(localized: "chat.work.working", defaultValue: "Working \($0)") }
                ?? String(localized: "chat.work.workingBare", defaultValue: "Working"))
        } else if summary.failed {
            parts.append(time.map { String(localized: "chat.work.stopped", defaultValue: "Stopped after \($0)") }
                ?? String(localized: "chat.work.stoppedBare", defaultValue: "Stopped"))
        } else {
            parts.append(time.map { String(localized: "chat.work.worked", defaultValue: "Worked \($0)") }
                ?? String(localized: "chat.work.workedBare", defaultValue: "Worked"))
        }
        if summary.steps > 0 { parts.append(steps(summary.steps)) }
        if summary.filesChanged > 0 { parts.append(files(summary.filesChanged)) }
        return parts.joined(separator: "  ")
    }

    /// A running line's words before its clock, which comes last: "Working  6 steps  2 files
    /// changed".
    static func workRunningLead(_ summary: WorkSummary) -> String {
        var parts = [String(localized: "chat.work.workingBare", defaultValue: "Working")]
        if summary.steps > 0 { parts.append(steps(summary.steps)) }
        if summary.filesChanged > 0 { parts.append(files(summary.filesChanged)) }
        return parts.joined(separator: "  ")
    }

    /// A running line's counts while the live line carries the clock: "6 steps  2 files
    /// changed"; "Working" before the first count.
    static func workCounts(_ summary: WorkSummary) -> String {
        var parts: [String] = []
        if summary.steps > 0 { parts.append(steps(summary.steps)) }
        if summary.filesChanged > 0 { parts.append(files(summary.filesChanged)) }
        return parts.isEmpty ? String(localized: "chat.work.workingBare", defaultValue: "Working") : parts.joined(separator: "  ")
    }

    /// What VoiceOver reads: "Worked 1 minute 12 seconds, 6 steps, 2 files changed, collapsed".
    static func spokenWork(_ summary: WorkSummary, now: Date, expanded isExpanded: Bool) -> String {
        var parts: [String] = []
        let time = summary.elapsed(now: now).map(spokenClock)
        if summary.running {
            parts.append(time.map { String(localized: "chat.work.working", defaultValue: "Working \($0)") }
                ?? String(localized: "chat.work.workingBare", defaultValue: "Working"))
        } else if summary.failed {
            parts.append(time.map { String(localized: "chat.work.stopped", defaultValue: "Stopped after \($0)") }
                ?? String(localized: "chat.work.stoppedBare", defaultValue: "Stopped"))
        } else {
            parts.append(time.map { String(localized: "chat.work.worked", defaultValue: "Worked \($0)") }
                ?? String(localized: "chat.work.workedBare", defaultValue: "Worked"))
        }
        if summary.steps > 0 { parts.append(steps(summary.steps)) }
        if summary.filesChanged > 0 { parts.append(files(summary.filesChanged)) }
        parts.append(isExpanded ? expanded : collapsed)
        return parts.joined(separator: ", ")
    }

    static func steps(_ n: Int) -> String {
        n == 1 ? String(localized: "chat.work.step", defaultValue: "1 step")
            : String(localized: "chat.work.steps", defaultValue: "\(n) steps")
    }

    static func files(_ n: Int) -> String {
        n == 1 ? String(localized: "chat.work.file", defaultValue: "1 file changed")
            : String(localized: "chat.work.files", defaultValue: "\(n) files changed")
    }

    /// "12s", "1m 12s", "1h 4m".
    static func clock(_ seconds: TimeInterval) -> String {
        let total = Int(seconds.rounded(.down))
        if total < 60 { return "\(total)s" }
        if total < 3600 { return "\(total / 60)m \(total % 60)s" }
        return "\(total / 3600)h \((total % 3600) / 60)m"
    }

    static func spokenClock(_ seconds: TimeInterval) -> String {
        Duration.seconds(Int(seconds.rounded(.down))).formatted(.units(allowed: [.hours, .minutes, .seconds], width: .wide))
    }

    /// "+3 -1" for a changed file.
    static func lines(_ change: HarnessFileChange) -> String {
        "+\(change.added) -\(change.removed)"
    }

    static func exitCode(_ code: Int) -> String {
        String(localized: "chat.step.exit", defaultValue: "exit \(code)")
    }

    static func thinking() -> String {
        String(localized: "chat.step.thinking", defaultValue: "Thought it through")
    }

    static func planStep(_ plan: HarnessItem.Plan) -> String {
        String(localized: "chat.step.plan", defaultValue: "Plan \(plan.done) of \(plan.total)")
    }

    static func stepState(_ state: HarnessState) -> String {
        switch state {
        case .running, .active: String(localized: "chat.state.running", defaultValue: "Running")
        case .waiting: String(localized: "chat.state.waiting", defaultValue: "Waiting")
        case .ok, .done: String(localized: "chat.state.ok", defaultValue: "Finished")
        case .failed: String(localized: "chat.state.failed", defaultValue: "Failed")
        case .denied: String(localized: "chat.state.denied", defaultValue: "Denied")
        case .cancelled, .interrupted: String(localized: "chat.state.stopped", defaultValue: "Stopped")
        default: state.rawValue.capitalized
        }
    }
}
