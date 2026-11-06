import Foundation
import TogoPush

/// Every string the Watch app shows.
enum WatchCopy {
    static let waiting = String(localized: "watch.waiting.title", defaultValue: "Waiting")
    static let nothingWaiting = String(localized: "watch.waiting.empty", defaultValue: "Nothing is waiting on you")
    static let approve = String(localized: "watch.approve", defaultValue: "Approve")
    static let deny = String(localized: "watch.deny", defaultValue: "Deny")
    static let approved = String(localized: "watch.approved", defaultValue: "Approved")
    static let denied = String(localized: "watch.denied", defaultValue: "Denied")
    static let answered = String(localized: "watch.answered", defaultValue: "Answered")
    /// Answered on the phone or at the desk, or closed by the machine.
    static let answeredElsewhere = String(localized: "watch.answeredElsewhere", defaultValue: "Answered elsewhere")
    static let expired = String(localized: "watch.expired", defaultValue: "Expired")
    static let sending = String(localized: "watch.sending", defaultValue: "Sending")
    static let answerOnPhone = String(localized: "watch.answerOnPhone", defaultValue: "Open Togo on iPhone to answer")
    static let sendFailed = String(localized: "watch.sendFailed", defaultValue: "Could not send. Try again.")
    /// The title of a notice the Watch posts itself.
    static let noticeTitle = String(localized: "watch.notice.title", defaultValue: "Togo")
    static let reply = String(localized: "watch.reply", defaultValue: "Reply")
    static let send = String(localized: "watch.reply.send", defaultValue: "Send")
    static let replyPlaceholder = String(localized: "watch.reply.placeholder", defaultValue: "Message")
    static let replySent = String(localized: "watch.reply.sent", defaultValue: "Sent")
    /// The phone could not reach the machine without asking; the words stay on the Watch.
    static let needsPhone = String(localized: "watch.reply.needsPhone", defaultValue: "Open Togo on your iPhone to send this.")
    /// The phone could not start the session again without asking.
    static let runAgainNeedsPhone = String(
        localized: "watch.runAgain.needsPhone", defaultValue: "Open Togo on your iPhone to run it again."
    )
    static let sessions = String(localized: "watch.sessions.title", defaultValue: "Sessions")
    static let noSessions = String(
        localized: "watch.sessions.empty", defaultValue: "Open Togo on iPhone to see your sessions"
    )

    /// The phone has not given this Watch a pairing for the machine yet.
    static func notSetUp(machine: String?) -> String {
        guard let machine, !machine.isEmpty else {
            return String(
                localized: "watch.notSetUp.unnamed",
                defaultValue: "Not set up for this machine. Open Togo on iPhone."
            )
        }
        return String(localized: "watch.notSetUp", defaultValue: "Not set up for \(machine). Open Togo on iPhone.")
    }

    /// "As of 12:04", in the Watch's own time format.
    static func asOf(_ date: Date, locale: Locale = .autoupdatingCurrent, timeZone: TimeZone = .autoupdatingCurrent)
        -> String {
        var style = Date.FormatStyle(date: .omitted, time: .shortened)
        style.locale = locale
        style.timeZone = timeZone
        let time = date.formatted(style)
        return String(localized: "watch.sessions.asOf", defaultValue: "As of \(time)")
    }

    /// The session status in words; a stopped agent says how it stopped, as on the phone.
    static func status(_ session: WatchSession) -> String {
        LifecycleWords.status(status: session.status, reason: session.exitReason, code: session.exitCode)
            ?? status(session.status)
    }

    /// The session status in words.
    static func status(_ wire: String) -> String {
        switch wire {
        case "needs_you": String(localized: "watch.status.needsYou", defaultValue: "Needs you")
        case "working": String(localized: "watch.status.working", defaultValue: "Working")
        case "done": String(localized: "watch.status.done", defaultValue: "Done")
        case "idle": String(localized: "watch.status.idle", defaultValue: "Idle")
        case "exited", "ended": LifecycleWords.ended
        case "failed": LifecycleWords.error
        default: String(localized: "watch.status.unknown", defaultValue: "Unknown")
        }
    }
}
