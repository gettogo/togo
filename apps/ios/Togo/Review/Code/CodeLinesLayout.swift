import UIKit

/// Places code rows in one collection view that scrolls both ways. Without wrap every row is one
/// line tall, so finding the rows in a rect is a division; with wrap, or when rows are side by
/// side, heights come from `CodeFlow` line breaks and a prefix sum that is rebuilt only when the width
/// or the metrics change. Cells always span the visible width and sit at the horizontal scroll
/// offset, so the gutter stays put while the text inside them slides.
final class CodeLinesLayout: UICollectionViewLayout {
    /// What the layout needs to know about a row: the text it shows.
    enum Row: Sendable {
        /// A line after the gutter.
        case code(CodeText)
        /// Two halves, each with its own gutter. They always wrap.
        case split(CodeText, CodeText)
        /// A hunk header or notice spanning the row. It wraps with the code but never scrolls.
        case banner(CodeText)
    }

    var lineHeight: CGFloat = 20 { didSet { if oldValue != lineHeight { invalidateGeometry() } } }
    var advance: CGFloat = 8 { didSet { if oldValue != advance { invalidateGeometry() } } }
    var gutterWidth: CGFloat = 0 { didSet { if oldValue != gutterWidth { invalidateGeometry() } } }
    var sideGutterWidth: CGFloat = 0 { didSet { if oldValue != sideGutterWidth { invalidateGeometry() } } }
    var wrap = false { didSet { if oldValue != wrap { invalidateGeometry() } } }
    /// Glyph widths in the code font; without it, wide characters count as two advances.
    var measure: CodeMeasure? { didSet { if oldValue !== measure { invalidateGeometry() } } }
    var rows: [Row] = [] {
        didSet {
            hasSplit = rows.contains { if case .split = $0 { true } else { false } }
            invalidateGeometry()
        }
    }

    static let verticalInset: CGFloat = 8
    static let trailingInset: CGFloat = 12
    static let bannerInset: CGFloat = 12

    /// How many times row tops were summed; a test watches it.
    private(set) var recomputeCount = 0
    private(set) var width: CGFloat = 0
    private var hasSplit = false
    /// Widest code line in points, measured once per set of rows and font.
    private var widest: CGFloat?
    /// `tops[i]` is the top of row `i` below the inset; it has one extra entry for the end.
    private var tops: [CGFloat] = []
    private var topsWidth: CGFloat?
    private var offsetX: CGFloat = 0

    private var variableHeights: Bool { wrap || hasSplit }
    var scrollsHorizontally: Bool { !wrap && !hasSplit }

    // MARK: - Geometry

    var codeColumns: Int { max(1, Int(((width - gutterWidth - Self.trailingInset) / advance).rounded(.down))) }
    var sideColumns: Int {
        max(1, Int(((halfWidth - sideGutterWidth - Self.trailingInset) / advance).rounded(.down)))
    }
    var bannerColumns: Int { max(1, Int(((width - 2 * Self.bannerInset) / advance).rounded(.down))) }
    var halfWidth: CGFloat { (width / 2).rounded(.down) }

    /// How wide a line may be before it breaks, for code, each split half, and banners.
    var codeLimit: CGFloat { CGFloat(codeColumns) * advance }
    var sideLimit: CGFloat { CGFloat(sideColumns) * advance }
    var bannerLimit: CGFloat { CGFloat(bannerColumns) * advance }

    /// Visual lines `text` takes within `limit`; the renderer draws exactly these. Wrapped
    /// code hangs its continuation lines under the code, past the line's own indent.
    func lineCount(_ text: CodeText, limit: CGFloat) -> Int {
        CodeFlow.lineCount(text, limit: limit, advance: advance, measure: measure, indent: indent(text, limit: limit))
    }

    func breaks(_ text: CodeText, limit: CGFloat) -> [Int] {
        CodeFlow.breaks(text, limit: limit, advance: advance, measure: measure, indent: indent(text, limit: limit))
    }

    /// Columns a continuation line of `text` starts in, when it wraps within `limit`.
    func indent(_ text: CodeText, limit: CGFloat) -> Int {
        text.words ? 0 : Self.continuationIndent(for: text, columns: CodeFlow.perLine(limit: limit, advance: advance))
    }

    /// The line's leading whitespace plus two columns, so wrapped code reads as one statement,
    /// capped at half of `columns` so a deeply indented line still has room.
    nonisolated static func continuationIndent(for text: CodeText, columns: Int) -> Int {
        let leading = text.string.utf8.prefix { $0 == 0x20 }.count
        return min(leading + 2, columns / 2)
    }

    /// Lays the rows out for a visible width; does nothing when that width was already used.
    func update(width: CGFloat) {
        self.width = width
        guard variableHeights, topsWidth != width else { return }
        recomputeCount += 1
        topsWidth = width
        var tops = [CGFloat](repeating: 0, count: rows.count + 1)
        var y: CGFloat = 0
        let code = codeLimit, side = sideLimit, banner = bannerLimit
        for (index, row) in rows.enumerated() {
            tops[index] = y
            let lines = switch row {
            case let .code(text): wrap ? lineCount(text, limit: code) : 1
            case let .split(left, right): max(lineCount(left, limit: side), lineCount(right, limit: side))
            case let .banner(text): wrap ? lineCount(text, limit: banner) : 1
            }
            y += CGFloat(lines) * lineHeight
        }
        tops[rows.count] = y
        self.tops = tops
    }

    func height(ofRow index: Int) -> CGFloat {
        variableHeights ? tops[index + 1] - tops[index] : lineHeight
    }

    func top(ofRow index: Int) -> CGFloat {
        Self.verticalInset + (variableHeights ? tops[index] : CGFloat(index) * lineHeight)
    }

    var contentSize: CGSize {
        let rowsHeight = variableHeights ? (tops.last ?? 0) : CGFloat(rows.count) * lineHeight
        guard scrollsHorizontally else { return CGSize(width: width, height: rowsHeight + 2 * Self.verticalInset) }
        let wide = gutterWidth + widestLine() + Self.trailingInset
        return CGSize(width: max(width, wide.rounded(.up)), height: rowsHeight + 2 * Self.verticalInset)
    }

    private func widestLine() -> CGFloat {
        if let widest { return widest }
        var result: CGFloat = 0
        for case let .code(text) in rows {
            result = max(result, CodeFlow.width(text, advance: advance, measure: measure))
        }
        widest = result
        return result
    }

    /// The first row whose bottom is below `y`.
    func row(at y: CGFloat) -> Int {
        guard !rows.isEmpty else { return 0 }
        let local = y - Self.verticalInset
        guard variableHeights else { return min(max(0, Int((local / lineHeight).rounded(.down))), rows.count - 1) }
        var low = 0, high = rows.count - 1
        while low < high {
            let mid = (low + high) / 2
            if tops[mid + 1] <= local { low = mid + 1 } else { high = mid }
        }
        return low
    }

    private func invalidateGeometry() {
        topsWidth = nil
        widest = nil
        invalidateLayout()
    }

    // MARK: - UICollectionViewLayout

    override func prepare() {
        super.prepare()
        guard let collectionView else { return }
        let insets = collectionView.adjustedContentInset
        offsetX = collectionView.bounds.minX + insets.left
        update(width: max(0, collectionView.bounds.width - insets.left - insets.right))
    }

    override var collectionViewContentSize: CGSize { contentSize }

    override func shouldInvalidateLayout(forBoundsChange newBounds: CGRect) -> Bool {
        guard let collectionView else { return false }
        let old = collectionView.bounds
        return newBounds.width != old.width || (scrollsHorizontally && newBounds.minX != old.minX)
    }

    override func layoutAttributesForElements(in rect: CGRect) -> [UICollectionViewLayoutAttributes]? {
        guard !rows.isEmpty, rect.maxY > Self.verticalInset else { return [] }
        let first = row(at: rect.minY)
        var result: [UICollectionViewLayoutAttributes] = []
        var index = first
        while index < rows.count, top(ofRow: index) < rect.maxY {
            result.append(attributes(index))
            index += 1
        }
        return result
    }

    override func layoutAttributesForItem(at indexPath: IndexPath) -> UICollectionViewLayoutAttributes? {
        indexPath.item < rows.count ? attributes(indexPath.item) : nil
    }

    private func attributes(_ index: Int) -> UICollectionViewLayoutAttributes {
        let attributes = UICollectionViewLayoutAttributes(forCellWith: IndexPath(item: index, section: 0))
        attributes.frame = CGRect(x: scrollsHorizontally ? offsetX : 0, y: top(ofRow: index), width: width, height: height(ofRow: index))
        return attributes
    }

    // MARK: - Columns

    /// Columns `text` fills on screen: a tab runs to the next stop, East Asian wide characters
    /// and emoji take two, everything else one.
    nonisolated static func displayColumns(_ text: String, tabWidth: Int) -> Int {
        var columns = 0
        if text.utf8.allSatisfy({ $0 < 0x80 }) {
            for byte in text.utf8 { columns += byte == 9 ? tabWidth - columns % tabWidth : 1 }
            return columns
        }
        for character in text { columns += Self.columns(of: character, at: columns, tabWidth: tabWidth) }
        return columns
    }

    nonisolated static func columns(of character: Character, at column: Int, tabWidth: Int) -> Int {
        if character == "\t" { return tabWidth - column % tabWidth }
        guard let first = character.unicodeScalars.first else { return 0 }
        if first.isASCII { return 1 }
        if character.unicodeScalars.contains(where: { $0.value == 0xFE0F }) { return 2 }
        return first.properties.isEmojiPresentation || isWide(first.value) ? 2 : 1
    }

    nonisolated private static func isWide(_ value: UInt32) -> Bool {
        switch value {
        case 0x1100...0x115F, 0x2E80...0x303E, 0x3041...0x33FF, 0x3400...0x4DBF, 0x4E00...0x9FFF, 0xA000...0xA4CF,
             0xAC00...0xD7A3, 0xF900...0xFAFF, 0xFE30...0xFE4F, 0xFF00...0xFF60, 0xFFE0...0xFFE6, 0x20000...0x3FFFD:
            true
        default:
            false
        }
    }
}
