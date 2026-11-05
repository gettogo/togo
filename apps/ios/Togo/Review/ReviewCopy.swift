import Foundation

/// Sentences shared by the review screens, keyed under `review.`. Each screen keeps its own
/// under `review.<screen>.`.
enum ReviewCopy {
    static func tabName(_ tab: ReviewTab) -> String {
        switch tab {
        case .changes: String(localized: "review.tab.changes", defaultValue: "Changes")
        case .history: String(localized: "review.tab.history", defaultValue: "History")
        case .files: String(localized: "review.tab.files", defaultValue: "Files")
        case .ports: String(localized: "review.tab.ports", defaultValue: "Ports")
        }
    }

    /// A write was refused because the file is not as the user last saw it.
    static let stale = String(localized: "review.stale", defaultValue: "This file changed since you looked. Check it again.")
    static let tryAgain = String(localized: "review.tryAgain", defaultValue: "Try again")
    static let unavailable = String(
        localized: "review.unavailable", defaultValue: "Review isn't available on this machine yet."
    )

    static let truncatedAll = String(
        localized: "review.truncatedAll", defaultValue: "Too many files changed to act on all of them. Pick files one by one."
    )
    static let nothingSelected = String(localized: "review.nothingSelected", defaultValue: "Pick at least one file.")

    static func invalidPath(_ path: String) -> String {
        String(localized: "review.invalidPath", defaultValue: "\(path) can't be changed from here.")
    }

    /// The port is written as digits, never grouped as a number would be.
    static func nothingListening(_ port: Int) -> String {
        String(localized: "review.nothingListening", defaultValue: "Nothing is listening on \(String(port)) any more")
    }
}
