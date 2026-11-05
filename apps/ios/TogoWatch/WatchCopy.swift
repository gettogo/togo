import Foundation

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
    static let sessions = String(localized: "watch.sessions.title", defaultValue: "Sessions")
    static let noSessions = String(
        localized: "watch.sessions.empty", defaultValue: "Open Togo on iPhone to see your sessions"
    )

    /// The phone has not given this Watch a pairing for the machine yet.
    static func notSetUp(machine: String?) -> String {
        guard let machine, !machine.isEmpty else {
            return String(
                localized: "watch.notSetUp.unnamed",
                defaultValue: "This Watch isn't set up for this machine yet. Open Togo on iPhone."
            )
        }
        return String(localized: "watch.notSetUp", defaultValue: "This Watch isn't set up for \(machine) yet. Open Togo on iPhone.")
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

    /// The session status in words.
    static func status(_ wire: String) -> String {
        switch wire {
        case "needs_you": String(localized: "watch.status.needsYou", defaultValue: "Needs you")
        case "working": String(localized: "watch.status.working", defaultValue: "Working")
        case "done": String(localized: "watch.status.done", defaultValue: "Done")
        case "idle": String(localized: "watch.status.idle", defaultValue: "Idle")
        case "exited": String(localized: "watch.status.exited", defaultValue: "Exited")
        default: String(localized: "watch.status.unknown", defaultValue: "Unknown")
        }
    }
}
