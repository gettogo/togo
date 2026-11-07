import Observation
import SwiftUI
import UIKit
import TogoKit

/// Words for the ways into the review screens, under `review.entry.`.
enum ReviewEntryCopy {
    static func changes(_ count: Int, more: Bool = false) -> String {
        if more { return String(localized: "review.entry.changesMore", defaultValue: "\(count)+ changes") }
        return count == 1
            ? String(localized: "review.entry.changesOne", defaultValue: "1 change")
            : String(localized: "review.entry.changesMany", defaultValue: "\(count) changes")
    }

    static let preview = String(localized: "review.entry.preview", defaultValue: "Preview")
    static let previewHint = String(localized: "review.entry.previewHint", defaultValue: "Opens the servers this session's machine is running")
    static let changesHint = String(localized: "review.entry.changesHint", defaultValue: "Opens the changes in this session's folder")
    static let close = String(localized: "review.entry.close", defaultValue: "Close")
    static let sections = String(localized: "review.entry.sections", defaultValue: "Section")
    static let nothingOpen = String(localized: "review.entry.nothingOpen", defaultValue: "Nothing open")
    static let nothingOpenMessage = String(
        localized: "review.entry.nothingOpenMessage", defaultValue: "Choose a file to see its changes or contents here."
    )

    static func helperMissing(_ machine: String) -> String {
        String(localized: "review.entry.helperMissing", defaultValue: "Changes, files and ports need the Togo helper on \(machine).")
    }

    static func helperOutdated(_ machine: String) -> String {
        String(localized: "review.entry.helperOutdated", defaultValue: "Update the helper on \(machine) to review changes.")
    }

    static func helperOutdatedPorts(_ machine: String) -> String {
        String(localized: "review.entry.helperOutdatedPorts", defaultValue: "Update the helper on \(machine) to see its ports.")
    }

    static func machineRemoved(_ machine: String) -> String {
        String(localized: "review.entry.machineRemoved", defaultValue: "\(machine) was removed from Togo.")
    }

    static func machineLost(_ machine: String) -> String {
        String(localized: "review.entry.machineLost", defaultValue: "Togo can no longer connect to \(machine).")
    }

    static let sessionEnded = String(localized: "review.entry.sessionEnded", defaultValue: "This session has ended.")

    /// Over a review kept open for its commit message.
    static func ended(_ why: String) -> String {
        String(
            localized: "review.entry.ended",
            defaultValue: "\(why) Nothing can be changed from here now; your commit message is kept so you can copy it."
        )
    }
}

/// What the workspace bar knows about the session on screen: how many files changed and
/// whether anything is listening. It reads both when the tab is selected and again whenever the
/// agent stops: idle, done, or waiting for the user.
@MainActor @Observable final class ReviewHeaderModel {
    private(set) var changes = 0
    /// The list was cut short, so there are more changes than `changes`.
    private(set) var moreChanges = false
    private(set) var hasPorts = false
    private var subject: ReviewSubject?
    /// Only the newest read may change what the header shows.
    private var generation = 0
    private let services: any ReviewServices

    init(services: any ReviewServices) {
        self.services = services
    }

    /// "3 changes", or nil when there is nothing to show.
    var changesLabel: String? { changes > 0 ? ReviewEntryCopy.changes(changes, more: moreChanges) : nil }

    /// Another session's counts are cleared at once, so they never show under this one.
    func select(_ subject: ReviewSubject) async {
        if subject != self.subject {
            changes = 0
            moreChanges = false
            hasPorts = false
        }
        self.subject = subject
        await refresh()
    }

    func statusChanged(to status: SessionStatus) async {
        guard Self.settles(status) else { return }
        await refresh()
    }

    /// A write in the review went through, or the review closed: the count may have changed.
    func reviewChanged(open: Bool) async {
        guard !open else { return }
        await refresh()
    }

    /// The moments the agent has likely finished touching files.
    nonisolated static func settles(_ status: SessionStatus) -> Bool {
        switch status {
        case .idle, .done, .needsYou: true
        case .working, .exited, .ended, .failed, .unknown: false
        }
    }

    /// A failed read hides the button rather than showing a count that may be wrong.
    func refresh() async {
        guard let subject else { return }
        generation += 1
        let generation = generation
        var changes = 0, more = false
        if subject.canRead, let status = try? await services.status(subject), status.repo {
            changes = status.files.count
            more = status.isTruncated
        }
        var hasPorts = false
        if subject.canPorts, let ports = try? await services.ports(subject) {
            hasPorts = !ports.isEmpty
        }
        guard generation == self.generation else { return }
        // The bar's items come and go, and the count changes, in a `respond` transaction, so
        // the system bar crossfades them rather than snapping.
        let changed = changes != self.changes || more != moreChanges || hasPorts != self.hasPorts
        guard changed else { return }
        // motion: state change — the bar's items come and go by a crossfade rather than snapping.
        withAnimation(Motion.animation(.respond, reduce: UIAccessibility.isReduceMotionEnabled)) {
            self.changes = changes
            moreChanges = more
            self.hasPorts = hasPorts
        }
    }
}

/// The review sheet as the store holds it: which session and which tab. Identified by the
/// session alone, so choosing another tab while it is open switches tabs in place.
struct ReviewRequest: Identifiable, Hashable {
    var subject: ReviewSubject
    var tab: ReviewTab
    var id: ReviewSubject { subject }
}

extension AppStore {
    var reviewing: ReviewRequest? {
        guard case .review(let subject, let tab)? = sheet else { return nil }
        return ReviewRequest(subject: subject, tab: tab)
    }

    func reviewSubject(for row: SessionRow) -> ReviewSubject {
        ReviewSubject(
            hostID: row.hostID, sessionID: row.id, bib: row.bib, title: row.name, cwd: row.cwd,
            managed: row.managed, capabilities: machine(for: row.hostID)?.capabilities ?? []
        )
    }

    /// Whether ⌘⇧D, ⌘⇧O or ⌘⇧P can open `tab` for the session on screen.
    func canOpenReview(_ tab: ReviewTab) -> Bool {
        guard workspaceOpen, let selected = selectedTab, let row = session(for: selected) else { return false }
        let subject = reviewSubject(for: row)
        return tab == .ports ? subject.canPorts : subject.canRead
    }

    func openReview(_ tab: ReviewTab) {
        guard canOpenReview(tab), let selected = selectedTab, let row = session(for: selected) else { return }
        review(row, tab: tab)
    }

    func closeReview() {
        if reviewing != nil { sheet = nil }
    }

    /// Closes the review when its session (or, with no `sessionID`, any session on the machine)
    /// is gone, since nothing in it can be read or changed any more. A commit message being
    /// written keeps it open, read-only, with `why` over it, so the message is not lost.
    func endReview(hostID: HostID, sessionID: String?, why: String) {
        guard let subject = reviewing?.subject, subject.hostID == hostID,
              sessionID == nil || subject.sessionID == sessionID else { return }
        if reviewHasDraft {
            reviewEnded = why
        } else {
            sheet = nil
        }
    }

}
