import CoreGraphics

/// Picks the font size a pinch settles on: the size that fits one of the widths TUIs are laid
/// out for, when one is near the pinched size; otherwise the pinched size itself.
public enum ZoomSnapper {
    static let columnStops = [80, 100, 120, 132]
    static let sizeRange: ClosedRange<CGFloat> = 9...24
    /// Sizes are kept to a tenth of a point; finer steps change nothing on screen.
    static let stepsPerPoint: CGFloat = 10
    /// A stop further than this, as a fraction of the pinched size, would undo the pinch.
    static let reach: CGFloat = 0.25
    /// Points of width left spare, so rounding of the cell width to the pixel grid cannot
    /// cost the last column.
    static let margin: CGFloat = 1

    /// `charWidth` gives the cell advance at a font size and must grow with it.
    public static func snap(width: CGFloat, fontSize: CGFloat, charWidth: (CGFloat) -> CGFloat) -> CGFloat {
        let clamped = min(max(fontSize, sizeRange.lowerBound), sizeRange.upperBound)
        let pinched = (clamped * stepsPerPoint).rounded() / stepsPerPoint
        guard width > margin, charWidth(pinched) > 0 else { return pinched }
        let candidates = columnStops.compactMap { size(fitting: $0, width: width - margin, charWidth: charWidth) }
            .filter { abs($0 - pinched) <= pinched * reach }
        return candidates.min { abs($0 - pinched) < abs($1 - pinched) } ?? pinched
    }

    /// The largest size that still shows `columns` columns, or nil when that size falls
    /// outside the range. Found by bisection.
    private static func size(fitting columns: Int, width: CGFloat, charWidth: (CGFloat) -> CGFloat) -> CGFloat? {
        func fits(_ size: CGFloat) -> Bool {
            // The tolerance keeps an exact fit from losing a column to rounding.
            width / charWidth(size) + 1e-6 >= CGFloat(columns)
        }
        guard fits(sizeRange.lowerBound), !fits(sizeRange.upperBound) else { return nil }
        var low = sizeRange.lowerBound
        var high = sizeRange.upperBound
        for _ in 0..<40 {
            let mid = (low + high) / 2
            if fits(mid) { low = mid } else { high = mid }
        }
        // Rounded down, so the size still fits.
        return max((low * stepsPerPoint).rounded(.down) / stepsPerPoint, sizeRange.lowerBound)
    }
}
