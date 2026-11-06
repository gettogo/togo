import CoreGraphics
import SwiftUI

/// What the workspace needs to know about the panes to place the chrome, the requests and the
/// compose bar around them. Published by `WorkspacePanes` as a preference.
struct PanesReport: Equatable {
    var arrangement: PaneArrangement
    var geometry: PaneGeometry
    /// The window could show a second pane if the user put a tab beside.
    var canSplit: Bool

    /// `fold` in the panes' own coordinates. A fold that misses the panes, such as one under a
    /// sidebar on the leading page, counts as none.
    init(size: CGSize, sizeClass: UserInterfaceSizeClass?, fold: FoldInfo, hasSecondary: Bool, primaryFraction: CGFloat) {
        let fold = fold.within(size)
        arrangement = PaneLayout.arrange(
            size: size, sizeClass: sizeClass, fold: fold, hasSecondary: hasSecondary, primaryFraction: primaryFraction
        )
        geometry = PaneGeometry(arrangement, size: size, fold: fold.division)
        let split = PaneLayout.arrange(
            size: size, sizeClass: sizeClass, fold: fold, hasSecondary: true, primaryFraction: primaryFraction
        )
        if case .sideBySide = split { canSplit = true } else { canSplit = false }
    }

    static let single = PanesReport(size: .zero, sizeClass: nil, fold: .flat, hasSecondary: false, primaryFraction: 0.5)

    struct Key: PreferenceKey {
        static let defaultValue = PanesReport.single
        static func reduce(value: inout PanesReport, nextValue: () -> PanesReport) {
            value = nextValue()
        }
    }
}

/// Where each part of the workspace sits for one arrangement, in the panes' own coordinates.
struct PaneGeometry: Equatable {
    var primary: CGRect
    var secondary: CGRect?
    /// The fold band, or the hairline where two panes meet without a fold.
    var band: CGRect?
    /// True when `band` is a fold, where nothing interactive may sit.
    var bandIsFold = false
    /// Tabletop: the region under the fold for requests, the compose bar and the keyboard.
    var deck: CGRect?
    /// Book: the page before the fold, where the chrome and the bars go when one pane spans
    /// both pages.
    var leadingPage: CGRect?

    static let headerHeight: CGFloat = 44
    /// The coordinate space of the whole workspace, where the fold was read.
    static let space = "workspace"

    /// `fold` is the active fold band, if any, in the same coordinates as `size`.
    init(_ arrangement: PaneArrangement, size: CGSize, fold: CGRect? = nil) {
        let all = CGRect(origin: .zero, size: size)
        if let fold, fold.height > fold.width, fold.minX > 0 {
            leadingPage = CGRect(x: 0, y: 0, width: fold.minX, height: size.height)
        }
        switch arrangement {
        case .single:
            primary = all
        case let .sideBySide(primaryWidth, gap):
            if let gap {
                primary = CGRect(x: 0, y: 0, width: gap.minX, height: size.height)
                band = CGRect(x: gap.minX, y: 0, width: gap.width, height: size.height)
                secondary = CGRect(x: gap.maxX, y: 0, width: max(0, size.width - gap.maxX), height: size.height)
                bandIsFold = true
            } else {
                let line = PaneLayout.hairline
                primary = CGRect(x: 0, y: 0, width: primaryWidth, height: size.height)
                band = CGRect(x: primaryWidth, y: 0, width: line, height: size.height)
                secondary = CGRect(
                    x: primaryWidth + line, y: 0, width: max(0, size.width - primaryWidth - line), height: size.height
                )
            }
        case let .tabletop(topHeight, gap):
            primary = CGRect(x: 0, y: 0, width: size.width, height: topHeight)
            band = CGRect(x: 0, y: topHeight, width: size.width, height: gap.height)
            deck = CGRect(x: 0, y: gap.maxY, width: size.width, height: max(0, size.height - gap.maxY))
            bandIsFold = true
        }
    }

    var showsTwoPanes: Bool { secondary != nil }

    var isTabletop: Bool { deck != nil }

    func frame(of pane: Pane) -> CGRect? {
        pane == .primary ? primary : secondary
    }

    /// The pane's own header, shown only while two panes are on screen.
    func headerFrame(of pane: Pane) -> CGRect? {
        guard showsTwoPanes, let frame = frame(of: pane) else { return nil }
        return CGRect(x: frame.minX, y: frame.minY, width: frame.width, height: Self.headerHeight)
    }

    /// Where request cards go: over the focused pane, never across the fold, or in the deck.
    func requestsFrame(focused: Pane) -> CGRect {
        if let deck { return deck }
        guard showsTwoPanes else { return leadingPage ?? primary }
        let pane = frame(of: focused) ?? primary
        return CGRect(x: pane.minX, y: pane.minY + Self.headerHeight, width: pane.width,
                      height: max(0, pane.height - Self.headerHeight))
    }

    /// The columns the compose bar and the Keyboard button keep to: under the focused pane, or
    /// on the leading page of a book. Nil leaves them the full width.
    func composeFrame(focused: Pane) -> CGRect? {
        if deck != nil { return nil }
        if showsTwoPanes { return frame(of: focused) }
        return leadingPage
    }

    /// The columns the header and tab strip keep to: the leading page of a book. Nil leaves
    /// them the full width.
    func chromeFrame() -> CGRect? {
        leadingPage
    }
}

extension PaneArrangement {
    enum Kind: Equatable {
        case single, sideBySide, tabletop
    }

    var kind: Kind {
        switch self {
        case .single: .single
        case .sideBySide: .sideBySide
        case .tabletop: .tabletop
        }
    }
}

extension FoldInfo {
    /// The same fold in coordinates whose origin is `offset` away.
    func moved(by offset: CGPoint) -> FoldInfo {
        FoldInfo(
            posture: posture,
            division: division?.offsetBy(dx: offset.x, dy: offset.y),
            inactiveDivision: inactiveDivision?.offsetBy(dx: offset.x, dy: offset.y)
        )
    }
}

extension FoldInfo {
    /// The fold as far as a view of `size` is concerned: a band that misses it is no fold.
    func within(_ size: CGSize) -> FoldInfo {
        let bounds = CGRect(origin: .zero, size: size)
        func inside(_ band: CGRect?) -> CGRect? {
            guard let band else { return nil }
            let overlap = band.intersection(bounds)
            return overlap.isNull || overlap.width <= 0 || overlap.height <= 0 ? nil : band
        }
        let division = inside(division)
        return FoldInfo(posture: division == nil ? .flat : posture, division: division,
                        inactiveDivision: inside(inactiveDivision))
    }
}
