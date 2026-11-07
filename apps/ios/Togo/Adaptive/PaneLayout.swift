import CoreGraphics
import SwiftUI

/// One of the two places a tab can sit in a window.
enum Pane: String, Codable, Hashable, Sendable {
    case primary, secondary
}

/// How the workspace divides its space between the panes.
enum PaneArrangement: Equatable, Sendable {
    case single
    /// `gap` is the fold band, or nil when the panes meet at a 1 pt hairline.
    case sideBySide(primaryWidth: CGFloat, gap: CGRect?)
    case tabletop(topHeight: CGFloat, gap: CGRect)
}

/// Where the workspace's panes go, from window size, size class and the fold alone.
enum PaneLayout {
    static let minimumPaneWidth: CGFloat = 360
    /// Window width that can carry a sidebar column.
    static let sidebarMinimumWidth: CGFloat = 700
    /// The sidebar column's width in a flat window.
    static let sidebarIdealWidth: CGFloat = 320
    static let fractions: [CGFloat] = [1.0 / 3, 0.5, 2.0 / 3]
    /// The line between two panes that meet without a fold.
    static let hairline: CGFloat = 1

    static func arrange(
        size: CGSize, sizeClass: UserInterfaceSizeClass?, fold: FoldInfo,
        hasSecondary: Bool, primaryFraction: CGFloat
    ) -> PaneArrangement {
        // A horizontal fold decides before any width rule: a held-up Duo is narrow and compact.
        if fold.posture == .tabletop, let gap = fold.division {
            return .tabletop(topHeight: gap.minY, gap: gap)
        }
        guard sizeClass == .regular else { return .single }
        switch fold.posture {
        case .tabletop:
            return .single
        case .book:
            guard hasSecondary, let gap = fold.division else { return .single }
            return .sideBySide(primaryWidth: gap.minX, gap: gap)
        case .flat:
            guard hasSecondary, size.width >= 2 * minimumPaneWidth + hairline else { return .single }
            let fraction = snap(primaryFraction, width: size.width)
            return .sideBySide(primaryWidth: (size.width - hairline) * fraction, gap: nil)
        }
    }

    static func showsSidebar(size: CGSize, sizeClass: UserInterfaceSizeClass?) -> Bool {
        sizeClass == .regular && size.width >= sidebarMinimumWidth
    }

    /// Hide the sidebar by default when two panes would fall under minimumPaneWidth beside it.
    static func prefersSidebarHidden(size: CGSize, hasSecondary: Bool) -> Bool {
        hasSecondary && size.width - sidebarIdealWidth < 2 * minimumPaneWidth + hairline
    }

    /// The roster column beside the terminal: narrow on a foldable, a little wider on iPad. It
    /// never follows the fold, so nothing sits on the hinge but the terminal.
    static let rosterWidthFoldable: CGFloat = 290
    static let rosterWidthTablet: CGFloat = 320

    static func rosterWidth(fold: FoldInfo) -> CGFloat {
        fold.division != nil || fold.inactiveDivision != nil ? rosterWidthFoldable : rosterWidthTablet
    }

    /// The nearest of `fractions` that keeps both panes at or above the minimum width. When
    /// none does, the panes split evenly.
    static func snap(_ fraction: CGFloat, width: CGFloat) -> CGFloat {
        let available = width - hairline
        let fitting = fractions.filter {
            available * $0 >= minimumPaneWidth && available * (1 - $0) >= minimumPaneWidth
        }
        return fitting.min { abs($0 - fraction) < abs($1 - fraction) } ?? 0.5
    }
}
