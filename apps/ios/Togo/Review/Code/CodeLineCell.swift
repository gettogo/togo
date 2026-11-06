import CoreText
import TogoKit
import UIKit

/// Sizes that follow from the code font at the current text size.
struct CodeMetrics: Equatable {
    static let pad: CGFloat = 8
    /// Code keeps growing with Dynamic Type up to here, where a phone still fits a short line.
    static let maximumPointSize: CGFloat = 28

    let font: UIFont
    let lineHeight: CGFloat
    let advance: CGFloat
    /// Baseline below the top of each visual line.
    let baseline: CGFloat
    let digits: Int
    let showsOld: Bool
    let showsGlyphs: Bool

    init(traits: UITraitCollection, digits: Int, showsOld: Bool, showsGlyphs: Bool) {
        let base = UIFont.monospacedSystemFont(ofSize: 13, weight: .regular)
        font = UIFontMetrics(forTextStyle: .body)
            .scaledFont(for: base, maximumPointSize: Self.maximumPointSize, compatibleWith: traits)
        lineHeight = (font.lineHeight * 1.3).rounded(.up)
        advance = ("0" as NSString).size(withAttributes: [.font: font]).width
        baseline = ((lineHeight - font.lineHeight) / 2 + font.ascender).rounded()
        self.digits = max(digits, 3)
        self.showsOld = showsOld
        self.showsGlyphs = showsGlyphs
    }

    var numberWidth: CGFloat { CGFloat(digits) * advance }
    var glyphWidth: CGFloat { showsGlyphs ? 2 * advance : 0 }
    /// Pad, old number, new number, glyph; each column followed by a one-column gap. Without
    /// glyphs the code keeps a second column of space from the numbers.
    var gutterWidth: CGFloat { Self.pad + (numberWidth + advance) * (showsOld ? 2 : 1) + trailingGap }
    var sideGutterWidth: CGFloat { Self.pad + numberWidth + advance + trailingGap }
    private var trailingGap: CGFloat { showsGlyphs ? glyphWidth : advance }
}

/// Everything one row needs to draw, built when the row first comes on screen and kept in the
/// renderer's cache. Text is laid out with Core Text once; drawing only positions lines.
final class CodeRender: NSObject {
    /// Text as Core Text lines. A long line that does not wrap is cut into segments laid end
    /// to end, so a frame draws only the ones on screen.
    struct Block {
        var x: CGFloat
        var lines: [CTLine]
        var ranges: [Range<Int>]
        /// Which visual line each segment is on, and its x within that line.
        var rows: [Int]
        var offsets: [CGFloat]
        var widths: [CGFloat]
        var highlights: [Range<Int>]
    }

    struct Half {
        var minX: CGFloat
        var width: CGFloat
        var background: CGColor?
        var word: CGColor?
        /// Line numbers, each with the right edge of its column.
        var numbers: [(line: CTLine, right: CGFloat)]
        var glyph: (line: CTLine, x: CGFloat)?
        var text: Block
    }

    let halves: [Half]
    /// Banners and notices stay in place while code scrolls sideways.
    let scrolls: Bool
    let divider: CGColor?

    init(halves: [Half], scrolls: Bool, divider: CGColor? = nil) {
        self.halves = halves
        self.scrolls = scrolls
        self.divider = divider
    }
}

/// Builds and caches `CodeRender`s for the rows on screen. Line breaks come from the layout,
/// so what is drawn always fills the row the layout made for it.
@MainActor final class CodeRenderer {
    private let cache = NSCache<NSNumber, CodeRender>()
    /// Text of rows that scroll sideways, drawn in tiles that a fling only moves.
    let tiles = CodeTileCache()
    private(set) var rows: [CodeRow] = []
    private(set) var metrics: CodeMetrics?
    private var traits = UITraitCollection()
    private var limits: [CGFloat] = []
    private var wrap = false

    init() {
        cache.countLimit = 1_500
    }

    /// Starts over with new rows, text size or appearance.
    func reset(rows: [CodeRow], metrics: CodeMetrics, traits: UITraitCollection) {
        cache.removeAllObjects()
        tiles.removeAll()
        self.rows = rows
        self.metrics = metrics
        self.traits = traits
    }

    /// The row's drawing, built on first use. A new width or wrap setting drops the cache.
    func render(_ index: Int, layout: CodeLinesLayout) -> CodeRender {
        let limits = [layout.width, layout.codeLimit, layout.sideLimit, layout.bannerLimit]
        if limits != self.limits || layout.wrap != wrap {
            cache.removeAllObjects()
            tiles.removeAll()
            self.limits = limits
            wrap = layout.wrap
        }
        if let cached = cache.object(forKey: index as NSNumber) { return cached }
        let render = build(rows[index], text: layout.rows[index], layout: layout)
        cache.setObject(render, forKey: index as NSNumber)
        return render
    }

    private func color(_ color: UIColor) -> CGColor { color.resolvedColor(with: traits).cgColor }

    /// Where a row that scrolls sideways gets its text tiles; nil for rows drawn whole.
    func tileSource(_ index: Int, render: CodeRender, height: CGFloat) -> CodeTileView.Source? {
        guard render.scrolls, render.halves.count == 1, let half = render.halves.first, let metrics else { return nil }
        let scale = max(traits.displayScale, 1)
        return CodeTileView.Source(
            cache: tiles, row: index, text: half.text, height: height,
            style: CodeTextDrawing.Style(metrics: metrics, word: half.word, underline: color(.togoMute), hairline: 1 / scale),
            appearance: traits.userInterfaceStyle.rawValue * 10 + Int(scale), scale: scale
        )
    }

    private func build(_ row: CodeRow, text: CodeLinesLayout.Row, layout: CodeLinesLayout) -> CodeRender {
        guard let m = metrics else { return CodeRender(halves: [], scrolls: false) }
        let width = layout.width
        switch (row, text) {
        case let (.line(number, oldNumber, source, kind, tokens, changes, _), .code(shown)):
            var numbers: [(Int?, CGFloat)] = []
            let first = CodeMetrics.pad + m.numberWidth
            if m.showsOld {
                numbers = [(oldNumber, first), (number, first + m.advance + m.numberWidth)]
            } else {
                numbers = [(kind == .removed ? oldNumber : number ?? oldNumber, first)]
            }
            let side = CodeRowSide(number: nil, text: source, kind: kind ?? .context, tokens: tokens, changes: changes)
            let starts = wrap ? layout.breaks(shown, limit: layout.codeLimit) : [0]
            let segments = wrap ? nil : Self.segments(shown, layout: layout)
            let indent = wrap ? CGFloat(layout.indent(shown, limit: layout.codeLimit)) * layout.advance : 0
            let half = self.half(
                side, shown: shown, starts: starts, segments: segments, indent: indent, numbers: numbers, minX: 0,
                width: width, textX: m.gutterWidth
            )
            return CodeRender(halves: [half], scrolls: !wrap)
        case let (.split(left, right), .split(leftShown, rightShown)):
            let halfWidth = layout.halfWidth
            let halves = [(left, leftShown, CGFloat(0)), (right, rightShown, halfWidth)].map { side, shown, minX in
                let side = side ?? CodeRowSide(number: nil, text: "", kind: .context, tokens: [], changes: [])
                return half(side, shown: shown, starts: layout.breaks(shown, limit: layout.sideLimit),
                            indent: CGFloat(layout.indent(shown, limit: layout.sideLimit)) * layout.advance,
                            numbers: [(side.number, minX + CodeMetrics.pad + m.numberWidth)], minX: minX,
                            width: minX == 0 ? halfWidth : width - halfWidth, textX: minX + m.sideGutterWidth)
            }
            return CodeRender(halves: halves, scrolls: false, divider: color(.togoLine))
        case let (_, .banner(shown)), let (_, .code(shown)), let (_, .split(shown, _)):
            let background = row.isHunk ? color(DiffPalette.hunk) : nil
            let string = NSAttributedString(string: shown.string, attributes: attributes(.togoMute, m.font))
            let starts = wrap ? layout.breaks(shown, limit: layout.bannerLimit) : [0]
            let block = Self.block(string, x: CodeLinesLayout.bannerInset, starts: starts, highlights: [])
            let half = CodeRender.Half(minX: 0, width: width, background: background, word: nil, numbers: [], glyph: nil, text: block)
            return CodeRender(halves: [half], scrolls: false)
        }
    }

    private func half(
        _ side: CodeRowSide, shown: CodeText, starts: [Int], segments: [Int]? = nil, indent: CGFloat,
        numbers: [(Int?, CGFloat)], minX: CGFloat, width: CGFloat, textX: CGFloat
    ) -> CodeRender.Half {
        guard let m = metrics else { preconditionFailure("metrics are set before rows render") }
        let gutter = attributes(DiffPalette.gutter, m.font)
        let lines = numbers.compactMap { number, right in
            number.map { (line: CTLineCreateWithAttributedString(NSAttributedString(string: "\($0)", attributes: gutter)), right: right) }
        }
        var glyph: (CTLine, CGFloat)?
        if m.showsGlyphs, let mark = side.kind == .added ? "+" : side.kind == .removed ? "-" : nil {
            glyph = (CTLineCreateWithAttributedString(NSAttributedString(string: mark, attributes: gutter)), textX - m.glyphWidth)
        }
        let styled = side.kind == .noNewline
            ? (NSAttributedString(string: shown.string, attributes: attributes(.togoMute, m.font)), [])
            : styledText(side, shown: shown)
        return CodeRender.Half(
            minX: minX, width: width, background: DiffPalette.background(side.kind).map(color),
            word: DiffPalette.word(side.kind).map(color), numbers: lines, glyph: glyph,
            text: Self.block(styled.0, x: textX, starts: starts, segments: segments, indent: indent, highlights: styled.1)
        )
    }

    private func attributes(_ foreground: UIColor, _ font: UIFont) -> [NSAttributedString.Key: Any] {
        [.font: font, .foregroundColor: color(foreground)]
    }

    /// Token colours and changed words, moved to where tab expansion put them.
    private func styledText(_ side: CodeRowSide, shown: CodeText) -> (NSAttributedString, [Range<Int>]) {
        guard let m = metrics else { preconditionFailure("metrics are set before rows render") }
        func moved(_ range: Range<Int>) -> NSRange? {
            let lower = shown.position(range.lowerBound), upper = shown.position(range.upperBound)
            return upper > lower ? NSRange(location: lower, length: upper - lower) : nil
        }
        let string = NSMutableAttributedString(string: shown.string, attributes: attributes(SyntaxPalette.uiColor(.plain), m.font))
        for token in side.tokens where token.kind != .plain {
            guard let range = moved(token.range) else { continue }
            string.addAttribute(.foregroundColor, value: color(SyntaxPalette.uiColor(token.kind)), range: range)
        }
        if shown.length > shown.sourceLength {
            let marker = NSRange(location: shown.sourceLength, length: shown.length - shown.sourceLength)
            string.addAttribute(.foregroundColor, value: color(.togoMute), range: marker)
        }
        let highlights = side.changes.compactMap { moved($0).map { $0.location..<($0.location + $0.length) } }
        return (string, highlights)
    }

    /// Lines no longer than this many advances draw as one segment.
    static let segmentColumns = 64

    /// One Core Text line per visual line, starting at each of `starts`, lines after the first
    /// `indent` points in. With `segments`, the single unwrapped line is cut at those offsets instead.
    static func block(
        _ string: NSAttributedString, x: CGFloat, starts: [Int], segments: [Int]? = nil, indent: CGFloat = 0,
        highlights: [Range<Int>]
    ) -> CodeRender.Block {
        let length = string.length
        let typesetter = CTTypesetterCreateWithAttributedString(string)
        let pieces = segments ?? starts
        var block = CodeRender.Block(x: x, lines: [], ranges: [], rows: [], offsets: [], widths: [], highlights: highlights)
        var offset: CGFloat = 0
        for (index, start) in pieces.enumerated() {
            let end = index + 1 < pieces.count ? pieces[index + 1] : length
            let line = CTTypesetterCreateLine(typesetter, CFRange(location: start, length: end - start))
            let width = CGFloat(CTLineGetTypographicBounds(line, nil, nil, nil))
            block.lines.append(line)
            block.ranges.append(start..<end)
            block.rows.append(segments == nil ? index : 0)
            block.offsets.append(segments == nil ? (index == 0 ? 0 : indent) : offset)
            block.widths.append(width)
            offset += width
        }
        return block
    }

    /// Segment offsets for a line drawn without wrapping, or nil when it is short.
    static func segments(_ shown: CodeText, layout: CodeLinesLayout) -> [Int]? {
        guard shown.length > segmentColumns else { return nil }
        return CodeFlow.breaks(shown, limit: CGFloat(segmentColumns) * layout.advance, advance: layout.advance, measure: layout.measure)
    }
}

/// One row on screen. One layer draws the gutter and backgrounds; a row that scrolls sideways
/// shows its text in tiles over it, so sliding the code moves layers and redraws nothing.
final class CodeLineCell: UICollectionViewCell {
    static let reuseIdentifier = "CodeLineCell"
    private let canvas = CodeCanvas()
    private let tileView = CodeTileView()
    private var tileSource: CodeTileView.Source?
    private var shift: CGFloat = 0
    var row: CodeRow?

    override init(frame: CGRect) {
        super.init(frame: frame)
        canvas.frame = contentView.bounds
        canvas.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        contentView.addSubview(canvas)
        tileView.isHidden = true
        tileView.accessibilityElementsHidden = true
        contentView.addSubview(tileView)
        isAccessibilityElement = true
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    /// `tiles` draws the text of a row that scrolls sideways; without it the canvas draws it.
    func configure(row: CodeRow, render: CodeRender, metrics: CodeMetrics, tiles: CodeTileView.Source? = nil) {
        self.row = row
        canvas.render = render
        canvas.metrics = metrics
        canvas.drawsText = tiles == nil
        canvas.setNeedsDisplay()
        tileSource = tiles
        tileView.isHidden = tiles == nil
        placeTiles()
        tileView.show(tiles, shift: shift)
        accessibilityTraits = row.isHunk ? .header : .staticText
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        guard tileSource != nil else { return }
        placeTiles()
        tileView.scroll(to: shift)
    }

    private func placeTiles() {
        guard let source = tileSource else { return }
        let x = CodeTileView.pixelAligned(source.text.x, scale: source.scale)
        let frame = CGRect(x: x, y: 0, width: max(0, contentView.bounds.width - x), height: contentView.bounds.height)
        if tileView.frame != frame { tileView.frame = frame }
    }

    /// Cells sit at the horizontal scroll offset, so their x is how far the code has slid.
    override func apply(_ layoutAttributes: UICollectionViewLayoutAttributes) {
        super.apply(layoutAttributes)
        guard shift != layoutAttributes.frame.minX else { return }
        shift = layoutAttributes.frame.minX
        canvas.shift = shift
        if tileSource != nil {
            tileView.scroll(to: shift)
        } else {
            canvas.setNeedsDisplay()
        }
    }

    override var accessibilityLabel: String? {
        get { row?.accessibilityLabel }
        set {}
    }
}

private final class CodeCanvas: UIView {
    var render: CodeRender?
    var metrics: CodeMetrics?
    var shift: CGFloat = 0
    var drawsText = true

    override init(frame: CGRect) {
        super.init(frame: frame)
        isOpaque = true
        contentMode = .redraw
        backgroundColor = .togoInk
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    override func draw(_ rect: CGRect) {
        guard let render, let metrics, let context = UIGraphicsGetCurrentContext() else { return }
        context.setFillColor(UIColor.togoInk.resolvedColor(with: traitCollection).cgColor)
        context.fill(bounds)
        context.textMatrix = CGAffineTransform(scaleX: 1, y: -1)
        let underline = UIColor.togoMute.resolvedColor(with: traitCollection).cgColor
        let hairline = 1 / max(traitCollection.displayScale, 1)
        for half in render.halves {
            let area = CGRect(x: half.minX, y: 0, width: half.width, height: bounds.height)
            if let background = half.background {
                context.setFillColor(background)
                context.fill(area)
            }
            for number in half.numbers {
                let width = CTLineGetTypographicBounds(number.line, nil, nil, nil)
                context.textPosition = CGPoint(x: number.right - width, y: metrics.baseline)
                CTLineDraw(number.line, context)
            }
            if let glyph = half.glyph {
                context.textPosition = CGPoint(x: glyph.x, y: metrics.baseline)
                CTLineDraw(glyph.line, context)
            }
            guard drawsText, area.maxX > half.text.x else { continue }
            context.saveGState()
            let shift = render.scrolls ? shift : 0
            context.clip(to: CGRect(x: half.text.x, y: 0, width: area.maxX - half.text.x, height: bounds.height))
            CodeTextDrawing.draw(
                half.text, in: context, originX: half.text.x - shift, visible: half.text.x...area.maxX,
                style: CodeTextDrawing.Style(metrics: metrics, word: half.word, underline: underline, hairline: hairline)
            )
            context.restoreGState()
        }
        if let divider = render.divider, let first = render.halves.first {
            context.setFillColor(divider)
            context.fill(CGRect(x: first.minX + first.width - hairline, y: 0, width: hairline, height: bounds.height))
        }
    }
}
