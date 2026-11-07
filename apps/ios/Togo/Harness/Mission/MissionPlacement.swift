import SwiftUI
import TogoKit

/// How mission control is presented: a sheet over the chat where the window is narrow, a
/// side inspector beside it where there is room (iPad, an unfolded Duo).
enum MissionPresentation: Equatable {
    case sheet
    case inspector

    static func choose(_ horizontalSizeClass: UserInterfaceSizeClass?) -> MissionPresentation {
        horizontalSizeClass == .regular ? .inspector : .sheet
    }
}

/// Where mission control sits and what its bar item says, apart from the views.
///
/// Rules: one bar item on every size class, shown only while the session has a mission; its
/// badge is the needs-you count, the one critical state. On a regular width the inspector is
/// open by default once the chat and the inspector both fit, unless the user closed it for that
/// session. The navigation subtitle carries the counts the badge does not.
enum MissionPlacement {
    /// The narrowest chat worth keeping beside an open inspector.
    static let chatMinimum: CGFloat = 540
    /// The inspector's ideal column width (`MissionControlPresenter`).
    static let inspectorIdeal: CGFloat = 320
    /// The workspace width, bars included, from which the inspector opens by default: an
    /// unfolded Duo held wide (about 951 pt) and an iPad in landscape qualify, an iPad 11" in
    /// portrait (834 pt) and a Duo's inner display held tall do not.
    static var openByDefaultWidth: CGFloat { chatMinimum + inspectorIdeal }

    /// Whether the inspector should be open without being asked: a regular width wide enough
    /// for both, a session with a mission, and no close by the user for this session.
    static func opensByDefault(
        _ presentation: MissionPresentation, width: CGFloat, hasMission: Bool, closedByUser: Bool
    ) -> Bool {
        presentation == .inspector && hasMission && !closedByUser && width >= openByDefaultWidth
    }

    /// The bar item's badge: what needs the user, nothing else.
    static func badge(_ summary: MissionSummary) -> Int { max(0, summary.needsYou) }

    /// The glance under the session's title on a horizontal bar: "2 agents  plan 3/7". The
    /// needs count is the badge's, so it is left out; empty when nothing is counted.
    static func glance(_ summary: MissionSummary) -> String {
        var counted = summary
        counted.needsYou = 0
        return MissionText.parts(counted).joined(separator: "  ")
    }
}

/// Which sessions the user closed the inspector for, so it stays closed for them while the app
/// runs. A session's inspector the user opens again is forgotten here.
@MainActor final class MissionInspectorMemory {
    static let shared = MissionInspectorMemory()

    private var closed: Set<String> = []

    init() {}

    static func key(_ subject: HarnessSubject) -> String {
        "\(subject.hostID.uuidString)/\(subject.sessionID)"
    }

    func closedByUser(_ subject: HarnessSubject) -> Bool {
        closed.contains(Self.key(subject))
    }

    /// The user opened or closed mission control. Only the inspector's state is remembered: a
    /// sheet is closed after every use anyway.
    func record(shown: Bool, for subject: HarnessSubject, in presentation: MissionPresentation) {
        guard presentation == .inspector else { return }
        if shown {
            closed.remove(Self.key(subject))
        } else {
            closed.insert(Self.key(subject))
        }
    }

    /// The bar item's tap: shows or hides mission control, and remembers an inspector's close.
    func toggle(_ store: HarnessSessionStore, in presentation: MissionPresentation) {
        store.showsMission.toggle()
        record(shown: store.showsMission, for: store.subject, in: presentation)
    }
}

/// When the bar item bounces, apart from the view: once when the needs count rises on the same
/// session, never when the focused session changes and never under Reduce Motion.
enum MissionNudge {
    struct Snapshot: Equatable {
        var session: String?
        var needsYou: Int
    }

    /// A new request arrived on the session already on screen.
    static func arrived(_ old: Snapshot, _ new: Snapshot) -> Bool {
        old.session == new.session && new.needsYou > old.needsYou
    }

    static func bounces(_ old: Snapshot, _ new: Snapshot, reduceMotion: Bool) -> Bool {
        !reduceMotion && arrived(old, new)
    }
}
