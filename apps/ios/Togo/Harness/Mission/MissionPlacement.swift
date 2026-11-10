import SwiftUI
import TogoKit

/// How mission control is presented: a sheet over the chat where the window is narrow, a
/// side inspector beside it where there is room (iPad, an unfolded Duo).
enum MissionPresentation: Equatable {
    case sheet
    case inspector

    /// The app's policy for side context (`AdaptivePresentation`): an inspector where the
    /// window keeps the sidebar, a usable chat and the inspector side by side, else a sheet. It
    /// reads the window, not the detail column, so showing or hiding the sidebar never changes
    /// it.
    static func choose(_ horizontalSizeClass: UserInterfaceSizeClass?, windowWidth: CGFloat? = nil) -> MissionPresentation {
        let style = AdaptivePresentation.style(
            for: .sideContext, sizeClass: horizontalSizeClass, containerWidth: windowWidth, hostsInspector: true
        )
        return style == .inspector ? .inspector : .sheet
    }
}

/// Where mission control sits and what its bar item says, apart from the views.
///
/// Rules: one bar item on every size class, shown only while the session has a mission; its
/// badge is the needs-you count, the one critical state. Mission control opens only when the
/// user asks (the item, ⌘I, a roster line's menu): never by itself, never because the selection,
/// the sidebar or the window changed. The navigation subtitle carries the counts the badge does
/// not.
enum MissionPlacement {
    /// Mission control's symbol: Apple's inspector glyph where it toggles the inspector column,
    /// an overview glyph where it opens a sheet. Never a gauge.
    static func symbol(_ presentation: MissionPresentation) -> String {
        presentation == .inspector ? "sidebar.trailing" : sheetSymbol
    }

    /// Wherever mission control is not the inspector column's toggle (a roster line's menu, a
    /// placeholder).
    static let sheetSymbol = "list.bullet.rectangle"

    /// The inspector's ideal column width (`MissionControlPresenter`).
    static let inspectorIdeal: CGFloat = 320

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

/// A session's key for mission control's per-session state, such as the item's nudge.
enum MissionKey {
    static func of(_ subject: HarnessSubject) -> String {
        "\(subject.hostID.uuidString)/\(subject.sessionID)"
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
