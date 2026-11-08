import Foundation

public enum HelperError: Error, Equatable, Sendable, LocalizedError {
    /// The helper answered with a JSON-RPC error object.
    case rpc(RPCErrorObject)
    /// No response arrived within the client's call timeout. A late response is dropped.
    case timeout(method: String)
    /// The channel ended, sent an oversized frame or sent bytes that are not protocol v1.
    /// Every pending and later call fails with this.
    case closed
    /// A call other than `hello` was made before `hello` succeeded.
    case notReady
    case unsupported(capability: String)
    /// The response's `result` did not have the shape the method promises.
    case malformedResponse
    /// `approval.answer` was refused because the approval was already answered, its
    /// prompt is gone or it expired; no keys reached the pane. Or a git write was refused
    /// and nothing changed (see `gitConflict`). Carries the helper's reason.
    case conflict(String)
    /// The helper turned the request down for a reason it names (`-32008`), such as
    /// `needs_admin_setup` for battery keep-awake. Nothing changed.
    case refused(String)
    /// The payload has this many bytes, more than the method accepts. Nothing was sent.
    case tooLarge(Int)
    /// A git write failed after changing these paths; the rest are as they were.
    case partialWrite(changed: [String])
    /// `git.commit` made this commit, but HEAD moved while it ran (`GitConflict.raced`).
    case racedCommit(hash: String)

    /// The helper asked for has no tmux session with this id: it was killed or ended.
    public var isSessionGone: Bool {
        if case .rpc(let error) = self { error.code == RPCErrorCode.notFound } else { false }
    }

    /// The reason a git write was refused, nil for any other error or an unknown reason.
    public var gitConflict: GitConflict? {
        switch self {
        case .conflict(let reason): GitConflict(rawValue: reason)
        case .partialWrite: .partial
        case .racedCommit: .raced
        default: nil
        }
    }

    public var errorDescription: String? {
        switch self {
        case .conflict(let reason):
            if let conflict = GitConflict(rawValue: reason) { Self.describe(conflict) }
            else if let text = Self.describeReason(reason) { text }
            else { String(localized: "That request was already answered, or it is no longer waiting.", bundle: .module) }
        case .refused(let reason): Self.describeRefused(reason)
        case .rpc(let error): Self.describe(error)
        case .timeout: String(localized: "The Togo helper on the machine didn't answer in time.", bundle: .module)
        case .closed: String(localized: "The connection to the Togo helper on the machine closed.", bundle: .module)
        case .notReady: String(localized: "The Togo helper on the machine isn't ready yet.", bundle: .module)
        case .unsupported: String(localized: "The Togo helper on the machine is too old for this. Update it there.", bundle: .module)
        case .malformedResponse: String(localized: "The Togo helper on the machine sent a reply that couldn't be read.", bundle: .module)
        case .tooLarge: String(localized: "That file is too large to send. The limit is about 700 KB.", bundle: .module)
        case .partialWrite(let changed): Self.describePartial(changed)
        case .racedCommit(let hash): Self.describeRaced(hash)
        }
    }

    private static func describeReason(_ reason: String) -> String? {
        switch reason {
        case "running": String(localized: "The agent is still running.", bundle: .module)
        case "shell": String(localized: "This session is a plain shell, so nothing was typed.", bundle: .module)
        case "ended": String(localized: "The agent has stopped, so nothing was typed.", bundle: .module)
        case "needs_you": String(localized: "The agent is waiting on a request. Answer that first.", bundle: .module)
        case "symlink": String(localized: "That file is a link, so it was left alone.", bundle: .module)
        case "missing": String(localized: "The machine has no list of authorized keys to change, so nothing was removed.", bundle: .module)
        case "not_regular": String(localized: "That file is not a regular file, so it was left alone.", bundle: .module)
        default: nil
        }
    }

    private static func describeRefused(_ reason: String) -> String {
        switch reason {
        case "needs_admin_setup": String(localized: "This needs a one-time setup on the machine first.", bundle: .module)
        default: String(localized: "The Togo helper on the machine turned that down.", bundle: .module)
        }
    }

    /// Names at most three paths, so a long list stays readable.
    private static func describePartial(_ changed: [String]) -> String {
        guard !changed.isEmpty else {
            return String(localized: "Only some of the changes were made. Refresh to see where things stand.", bundle: .module)
        }
        let shown = changed.prefix(3)
        let list = changed.count > shown.count
            ? String(localized: "\(shown.joined(separator: ", ")) and \(changed.count - shown.count) more", bundle: .module)
            : ListFormatter.localizedString(byJoining: Array(shown))
        return String(localized: "Some changes were made: \(list). Refresh to see where things stand.", bundle: .module)
    }

    private static func describeRaced(_ hash: String) -> String {
        hash.isEmpty
            ? String(localized: "Someone else committed at the same time. Refresh to see both commits.", bundle: .module)
            : String(localized: "Someone else committed at the same time. Your commit is \(hash.prefix(7)).", bundle: .module)
    }

    private static func describe(_ conflict: GitConflict) -> String {
        switch conflict {
        case .stale: String(localized: "This file changed since you looked. Check it again.", bundle: .module)
        case .locked: String(localized: "Git is busy in this folder. Try again in a moment.", bundle: .module)
        case .filtered: String(localized: "This file uses a git filter such as LFS. Stage it at your desk.", bundle: .module)
        case .unmerged: String(localized: "Resolve the conflict first.", bundle: .module)
        case .nothingStaged: String(localized: "Nothing is staged to commit.", bundle: .module)
        case .identity: String(localized: "Set your git name and email on the machine first.", bundle: .module)
        case .unbornAmend: String(localized: "There is no commit to amend yet.", bundle: .module)
        case .partial: describePartial([])
        case .raced: describeRaced("")
        case .caseRename: String(localized: "A rename that only changes letter case can't be undone from here.", bundle: .module)
        }
    }

    private static func describe(_ error: RPCErrorObject) -> String {
        switch error.code {
        case RPCErrorCode.notFound: String(localized: "This session has ended.", bundle: .module)
        case RPCErrorCode.tmuxUnavailable: String(localized: "The machine needs tmux 3.2 or newer.", bundle: .module)
        case RPCErrorCode.notReady: String(localized: "The Togo helper on the machine was asked before it was ready.", bundle: .module)
        case RPCErrorCode.invalidPath: String(localized: "That folder does not exist on the machine.", bundle: .module)
        case RPCErrorCode.unsupportedVersion: String(localized: "The Togo helper on the machine speaks a different protocol version. Update the app or the helper.", bundle: .module)
        case RPCErrorCode.conflict: String(localized: "That request was already answered.", bundle: .module)
        // The helper says why in its own words when it can ("That mode is not in this
        // session's cycle"); otherwise the reason picks the sentence.
        case RPCErrorCode.refused: error.refusalMessage ?? describeRefused(error.reason ?? "")
        case RPCErrorCode.notManaged: String(localized: "That session was not started from Togo, so it cannot be changed from here.", bundle: .module)
        case RPCErrorCode.invalidParams: String(localized: "The Togo helper on the machine turned that request down. Update the app or the helper.", bundle: .module)
        default: String(localized: "The Togo helper on the machine could not do that.", bundle: .module)
        }
    }
}
