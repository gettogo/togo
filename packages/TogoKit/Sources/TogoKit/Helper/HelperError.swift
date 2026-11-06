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
            else { "That request was already answered, or it is no longer waiting." }
        case .refused(let reason): Self.describeRefused(reason)
        case .rpc(let error): Self.describe(error)
        case .timeout: "The Togo helper on the machine did not answer in time."
        case .closed: "The connection to the Togo helper on the machine closed."
        case .notReady: "The Togo helper on the machine is not ready yet."
        case .unsupported: "The Togo helper on the machine is too old for this. Update it on the machine."
        case .malformedResponse: "The Togo helper on the machine sent a reply that could not be read."
        case .tooLarge: "That file is too large to send. The limit is about 700 KB."
        case .partialWrite(let changed): Self.describePartial(changed)
        case .racedCommit(let hash): Self.describeRaced(hash)
        }
    }

    private static func describeReason(_ reason: String) -> String? {
        switch reason {
        case "running": "The agent is still running."
        case "shell": "This session is a plain shell, so nothing was typed."
        case "ended": "The agent has stopped, so nothing was typed."
        case "needs_you": "The agent is waiting on a request. Answer that first."
        case "symlink": "That file is a link, so it was left alone."
        case "missing": "The machine has no list of authorized keys to change, so nothing was removed."
        case "not_regular": "That file is not a regular file, so it was left alone."
        default: nil
        }
    }

    private static func describeRefused(_ reason: String) -> String {
        switch reason {
        case "needs_admin_setup": "This needs a one-time setup on the machine first."
        default: "The Togo helper on the machine turned that down."
        }
    }

    /// Names at most three paths, so a long list stays readable.
    private static func describePartial(_ changed: [String]) -> String {
        let refresh = "Refresh to see where things stand."
        guard !changed.isEmpty else { return "Only some of the changes were made. \(refresh)" }
        let shown = changed.prefix(3)
        let list = changed.count > shown.count
            ? shown.joined(separator: ", ") + " and \(changed.count - shown.count) more"
            : ListFormatter.localizedString(byJoining: Array(shown))
        return "Some changes were made: \(list). \(refresh)"
    }

    private static func describeRaced(_ hash: String) -> String {
        hash.isEmpty
            ? "Someone else committed at the same time. Refresh to see both commits."
            : "Someone else committed at the same time. Your commit is \(hash.prefix(7))."
    }

    private static func describe(_ conflict: GitConflict) -> String {
        switch conflict {
        case .stale: "This file changed since you looked. Check it again."
        case .locked: "Git is busy in this folder. Try again in a moment."
        case .filtered: "This file uses a git filter such as LFS. Stage it at your desk."
        case .unmerged: "Resolve the conflict first."
        case .nothingStaged: "Nothing is staged to commit."
        case .identity: "Set your git name and email on the machine first."
        case .unbornAmend: "There is no commit to amend yet."
        case .partial: describePartial([])
        case .raced: describeRaced("")
        case .caseRename: "A rename that only changes letter case can't be undone from here."
        }
    }

    private static func describe(_ error: RPCErrorObject) -> String {
        switch error.code {
        case RPCErrorCode.notFound: "This session has ended."
        case RPCErrorCode.tmuxUnavailable: "The host needs tmux 3.2 or later."
        case RPCErrorCode.notReady: "The Togo helper on the machine was asked before it was ready."
        case RPCErrorCode.invalidPath: "That folder does not exist on the machine."
        case RPCErrorCode.unsupportedVersion: "The Togo helper on the machine speaks a different protocol version. Update the app or the helper."
        case RPCErrorCode.conflict: "That request was already answered."
        case RPCErrorCode.refused: describeRefused("")
        case RPCErrorCode.notManaged: "That session was not started from Togo, so it cannot be changed from here."
        case RPCErrorCode.invalidParams: "The Togo helper on the machine turned that request down. Update the app or the helper."
        default: "The Togo helper on the machine could not do that."
        }
    }
}
