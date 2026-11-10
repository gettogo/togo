import Foundation

/// Words of the diff screen, under `review.diff.`.
enum DiffCopy {
    static let truncated = String(localized: "review.diff.truncated", defaultValue: "Showing the first 512 KB of this diff.")
    static let binary = String(localized: "review.diff.binary", defaultValue: "Binary file, not shown")
    static let openFile = String(localized: "review.diff.openFile", defaultValue: "Open file")
    static let emptyNewFile = String(localized: "review.diff.emptyNewFile", defaultValue: "This new file is empty.")
    static let emptyDeletedFile = String(
        localized: "review.diff.emptyDeletedFile", defaultValue: "This file was empty when it was deleted."
    )

    static func modeOnly(old: String, new: String) -> String {
        String(localized: "review.diff.modeOnly", defaultValue: "Only the file mode changed (\(old) to \(new)).")
    }

    static func renamedOnly(_ old: String) -> String {
        String(localized: "review.diff.renamedOnly", defaultValue: "Renamed from \(old). The content is the same.")
    }

    static let loading = String(localized: "review.diff.loading", defaultValue: "Reading the diff")
    static let failed = String(localized: "review.diff.failed", defaultValue: "Couldn't show this diff")
    static let tryAgain = ReviewCopy.tryAgain
    static let noChanges = String(localized: "review.diff.noChanges", defaultValue: "No changes")
    static let noChangesMessage = String(
        localized: "review.diff.noChangesMessage", defaultValue: "There is nothing to compare in this file."
    )
    static let discarded = String(localized: "review.diff.discarded", defaultValue: "Changes discarded")
    static let deleted = String(localized: "review.diff.deleted", defaultValue: "File deleted")

    static func discardedMessage(_ name: String) -> String {
        String(localized: "review.diff.discardedMessage", defaultValue: "\(name) is back as it was in the last commit.")
    }

    static func deletedMessage(_ name: String) -> String {
        String(localized: "review.diff.deletedMessage", defaultValue: "\(name) was deleted.")
    }

    // Toolbar
    static let layout = String(localized: "review.diff.layout", defaultValue: "Layout")
    static let unified = String(localized: "review.diff.unified", defaultValue: "Unified")
    static let sideBySide = String(localized: "review.diff.sideBySide", defaultValue: "Side by side")
    static let wrap = String(localized: "review.diff.wrap", defaultValue: "Wrap lines")
    static let options = String(localized: "review.diff.options", defaultValue: "View options")
    static let previousHunk = String(localized: "review.diff.previousHunk", defaultValue: "Previous change")
    static let nextHunk = String(localized: "review.diff.nextHunk", defaultValue: "Next change")
    static let files = String(localized: "review.diff.files", defaultValue: "Files")
    static let stage = String(localized: "review.diff.stage", defaultValue: "Stage")
    static let unstage = String(localized: "review.diff.unstage", defaultValue: "Unstage")
    static let dismiss = String(localized: "review.diff.dismiss", defaultValue: "Dismiss")

    // After a discard
    static let renameUndone = String(localized: "review.diff.renameUndone", defaultValue: "Rename undone")

    static func renameUndoneMessage(_ old: String) -> String {
        String(localized: "review.diff.renameUndoneMessage", defaultValue: "\(old) is back as it was in the last commit.")
    }

    // Comments
    static let sendToSession = String(localized: "review.diff.sendToSession", defaultValue: "Send to session")
    static let openSessionToSend = String(
        localized: "review.diff.openSessionToSend", defaultValue: "Open the session to send comments"
    )
    static let commentField = String(localized: "review.diff.commentField", defaultValue: "Your comment")
    static let commentNotSent = String(
        localized: "review.diff.commentNotSent",
        defaultValue: "Not sent: the session's terminal is closed. Open the session, then try again."
    )
    static let cancel = String(localized: "review.diff.cancel", defaultValue: "Cancel")
}
