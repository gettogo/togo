import SwiftUI
import TogoKit

/// What the roster list draws, mirrored through `IdentityKeyed` so a session starting, ending
/// or being killed moves its line in one `resize` transaction, with the machine's status rows,
/// the headline and the notice in the same one (motion spec §2.5, inventory U-49 to U-53). The
/// words inside a line (status, last action, tally) keep updating in place without animation
/// (design-language §3), and a search or filter lands at once.
struct RosterShown: Equatable {
    var model: RosterModel
    var notice: Notice?
    var wakeNotes: [HostID: String]
    /// The search query and the filter.
    var narrowing: String

    /// What moves the list's layout: which rows exist, by identity.
    static func shape(_ shown: RosterShown) -> [String] {
        let headline = shown.model.waiting > 0 || !shown.model.tallyText.isEmpty
        var out = ["headline \(headline)", "notice \(shown.notice != nil)"]
        for group in shown.model.groups {
            let machine = group.machine
            out.append(
                "machine \(machine.id) \(String(describing: machine.reachability)) "
                    + "\(String(describing: machine.helper)) wake \(shown.wakeNotes[machine.id] != nil)"
            )
            out += group.lines.map { "line \($0.id)" }
            if machine.reachability == .online {
                out += machine.restorable.map { "restorable \($0.id)" }
            }
        }
        return out
    }

    /// A new query or filter: its results land with no animation.
    static func quiet(_ shown: RosterShown) -> AnyHashable {
        shown.narrowing
    }
}

extension AppStore {
    /// The roster with the state around it that adds or removes rows.
    var rosterShown: RosterShown {
        RosterShown(
            model: roster, notice: notice, wakeNotes: wakeNotes,
            narrowing: "\(filter) \(query.trimmingCharacters(in: .whitespacesAndNewlines))"
        )
    }
}
