import CoreGraphics
import SwiftUI

/// How a foldable is held. Read from the shape of the fold band, never from hinge angles.
enum Posture: Equatable, Sendable {
    case flat, book, tabletop
}

/// Where the fold is, in the coordinates of the view that read it.
struct FoldInfo: Equatable, Sendable {
    var posture: Posture
    /// The fold band while the device is bent; nil when it is flat or has no fold.
    var division: CGRect?
    /// Where the fold would be; nil on devices without one.
    var inactiveDivision: CGRect?

    static let flat = FoldInfo(posture: .flat, division: nil, inactiveDivision: nil)

    /// Active band taller than wide -> .book, wider than tall -> .tabletop, nil -> .flat.
    static func from(active: CGRect?, inactive: CGRect?) -> FoldInfo {
        guard let active else {
            return FoldInfo(posture: .flat, division: nil, inactiveDivision: inactive)
        }
        let posture: Posture = active.height > active.width ? .book : .tabletop
        return FoldInfo(posture: posture, division: active, inactiveDivision: inactive)
    }
}

extension EnvironmentValues {
    @Entry var fold: FoldInfo = .flat
}

extension FoldInfo {
    /// The fold, read by a view at `anchor` in the window, as a view at `origin` of `size` sees
    /// it: in that view's own coordinates, and `.flat` when the band misses the view, as it does
    /// for a detail column on the page after the fold. Without an anchor the fold is taken to be
    /// in the view's coordinates already, as tests and previews give it.
    func seen(from anchor: CGPoint?, at origin: CGPoint, size: CGSize) -> FoldInfo {
        guard let anchor else { return self }
        return moved(by: CGPoint(x: anchor.x - origin.x, y: anchor.y - origin.y)).within(size)
    }

    /// Where a column of text keeps to in a view `width` wide whose leading edge is at `minX` in
    /// the window: in book posture, the wider page either side of the fold, in the view's own
    /// coordinates. Nil when the fold is flat, tabletop or misses the view.
    func readingPage(from anchor: CGPoint?, minX: CGFloat, width: CGFloat) -> ClosedRange<CGFloat>? {
        guard posture == .book, var band = division, width > 0 else { return nil }
        if let anchor { band = band.offsetBy(dx: anchor.x - minX, dy: 0) }
        guard band.maxX > 0, band.minX < width else { return nil }
        let leading = max(0, band.minX)
        let trailing = min(width, band.maxX)
        return leading >= width - trailing ? 0...leading : trailing...width
    }
}

extension EnvironmentValues {
    /// Where the view that read `fold` sits in the window, so a view inside it can move the fold
    /// into its own coordinates. Nil when the fold was set from outside.
    @Entry var foldOrigin: CGPoint?
}
