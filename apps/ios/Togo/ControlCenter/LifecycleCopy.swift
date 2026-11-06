import Foundation
import TogoKit
import TogoPush

/// How a session whose agent stopped reads in the app. The words are the ones notifications,
/// the Live Activity and the Watch use, so a session reads the same everywhere.
enum LifecycleCopy {
    /// The status in words: "Ended", "Stopped with an error", "Hit a usage limit" or "Ended
    /// by restart" for a stopped agent, the plain status word otherwise.
    static func status(_ s: Session) -> String {
        status(s.status, exit: s.exit)
    }

    static func status(_ status: SessionStatus, exit: SessionExit?) -> String {
        LifecycleWords.status(status: status.rawValue, reason: exit?.reason.rawValue, code: exit?.code)
            ?? StatusWord.label(status)
    }

    /// "Exited with code 1" when the agent exited on its own with a code other than 0.
    static func detail(_ e: SessionExit) -> String? {
        LifecycleWords.detail(reason: e.reason.rawValue, code: e.code)
    }

    /// "Stopped with an error · Exited with code 1": the line the workspace shows under a stopped
    /// session's title. Nil while the agent runs.
    static func line(status: SessionStatus, exit: SessionExit?) -> String? {
        guard status == .ended || status == .failed else { return nil }
        let words = Self.status(status, exit: exit)
        guard let detail = exit.flatMap(detail) else { return words }
        return "\(words) · \(detail)"
    }

    /// Whether the agent can be started again. After a usage limit, or an error it reported
    /// with no exit code yet, it is still running at its prompt, which the helper refuses to
    /// restart.
    static func canRunAgain(_ status: SessionStatus, exit: SessionExit?) -> Bool {
        LifecycleWords.canRunAgain(status: status.rawValue, reason: exit?.reason.rawValue, code: exit?.code)
    }

    /// Whether the stopped agent is still alive at its prompt and takes a reply.
    static func agentWaiting(_ status: SessionStatus, exit: SessionExit?) -> Bool {
        LifecycleWords.agentWaiting(status: status.rawValue, reason: exit?.reason.rawValue, code: exit?.code)
    }

    static let runAgain = String(localized: "lifecycle.runAgain", defaultValue: "Run again")

    static func runAgainFailed(_ label: String, reason: String) -> String {
        String(localized: "lifecycle.runAgainFailed", defaultValue: "Couldn't run \(label) again: \(reason)")
    }

    /// Run again from the Watch for a machine this phone no longer has.
    static let runAgainUnknownMachine = String(
        localized: "lifecycle.runAgainUnknownMachine", defaultValue: "Couldn't run it again: this machine is no longer in Togo."
    )

    /// Run again from the Watch for a session the machine no longer lists.
    static let runAgainGone = String(
        localized: "lifecycle.runAgainGone", defaultValue: "Couldn't run it again: the session is gone."
    )
}
