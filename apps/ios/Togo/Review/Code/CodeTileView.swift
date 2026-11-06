import CoreText
import QuartzCore
import UIKit

/// Draws a row's text, wrapped or not, so the canvas and the tiles share one routine.
@MainActor enum CodeTextDrawing {
    struct Style {
        var metrics: CodeMetrics
        var word: CGColor?
        var underline: CGColor
        var hairline: CGFloat
    }

    /// Draws the lines of `text` whose span meets `visible`, with the text's origin at `originX`.
    static func draw(_ text: CodeRender.Block, in context: CGContext, originX: CGFloat, visible: ClosedRange<CGFloat>, style: Style) {
        let m = style.metrics
        context.textMatrix = CGAffineTransform(scaleX: 1, y: -1)
        for (index, line) in text.lines.enumerated() {
            let top = CGFloat(text.rows[index]) * m.lineHeight
            let x = originX + text.offsets[index]
            guard x < visible.upperBound, x + text.widths[index] > visible.lowerBound else { continue }
            let range = text.ranges[index]
            for word in text.highlights where word.overlaps(range) {
                let start = CTLineGetOffsetForStringIndex(line, max(word.lowerBound, range.lowerBound), nil)
                let end = CTLineGetOffsetForStringIndex(line, min(word.upperBound, range.upperBound), nil)
                if let tint = style.word {
                    context.setFillColor(tint)
                    context.fill(CGRect(x: x + start, y: top, width: end - start, height: m.lineHeight))
                }
                context.setFillColor(style.underline)
                context.fill(CGRect(x: x + start, y: top + m.baseline + 2, width: end - start, height: style.hairline * 2))
            }
            context.textPosition = CGPoint(x: x, y: top + m.baseline)
            CTLineDraw(line, context)
        }
    }
}

/// Which tile of which row, drawn for which appearance.
struct CodeTileKey: Hashable {
    let row: Int
    let tile: Int
    let appearance: Int
}

/// Tiles of row text drawn once and kept, least recently used first out. A horizontal fling
/// then only moves layers; drawing happens when a tile first comes near the screen.
///
/// Kept under both a count and a byte limit, and dropped whole on a memory warning or when the
/// app goes to the background: every tile can be drawn again.
@MainActor final class CodeTileCache {
    static let tileWidth: CGFloat = 512
    static let capacity = 400
    /// 48 MB: about 130 tiles of a 20-point row at 3x.
    static let defaultByteLimit = 48 << 20
    /// Drawing ahead of the screen stops for the frame once this much time went to it.
    static let prefetchBudget: CFTimeInterval = 0.002

    private struct Entry {
        var image: CGImage?
        var bytes: Int
        var used: UInt64
    }

    private let byteLimit: Int
    private var entries: [CodeTileKey: Entry] = [:]
    /// Every use in order, oldest first. A use made stale by a later one is skipped when it
    /// reaches the front, so finding the least recently used tile does not scan them all.
    private var uses: [(key: CodeTileKey, tick: UInt64)] = []
    private var usesHead = 0
    private var tick: UInt64 = 0
    private var frameStart: CFTimeInterval = 0
    private var spentThisFrame: CFTimeInterval = 0
    private var observers: [any NSObjectProtocol] = []
    /// Tiles drawn since the cache was made; a test watches it.
    private(set) var draws = 0
    private(set) var bytes = 0

    init(notifications: NotificationCenter = .default, byteLimit: Int = CodeTileCache.defaultByteLimit) {
        self.byteLimit = byteLimit
        self.notifications = notifications
        for name in [UIApplication.didReceiveMemoryWarningNotification, UIApplication.didEnterBackgroundNotification] {
            observers.append(notifications.addObserver(forName: name, object: nil, queue: .main) { [weak self] _ in
                MainActor.assumeIsolated { self?.removeAll() }
            })
        }
    }

    private let notifications: NotificationCenter

    isolated deinit {
        for observer in observers { notifications.removeObserver(observer) }
    }

    var count: Int { entries.count }

    func removeAll() {
        entries.removeAll(keepingCapacity: true)
        uses.removeAll(keepingCapacity: true)
        usesHead = 0
        bytes = 0
    }

    func contains(_ key: CodeTileKey) -> Bool {
        entries[key] != nil
    }

    /// The tile's image, drawn now if it is not kept. Nil for a tile with nothing in it.
    func image(_ key: CodeTileKey, draw: () -> CGImage?) -> CGImage? {
        tick += 1
        if var entry = entries[key] {
            entry.used = tick
            entries[key] = entry
            noteUse(key)
            return entry.image
        }
        let start = CACurrentMediaTime()
        let image = draw()
        note(spent: CACurrentMediaTime() - start, at: start)
        draws += 1
        let size = image.map { $0.bytesPerRow * $0.height } ?? 0
        entries[key] = Entry(image: image, bytes: size, used: tick)
        bytes += size
        noteUse(key)
        while entries.count > Self.capacity || (bytes > byteLimit && entries.count > 1) {
            guard evictOldest(sparing: key) else { break }
        }
        return image
    }

    private func noteUse(_ key: CodeTileKey) {
        uses.append((key, tick))
        // Stale uses pile up while tiles are reused; drop them once they outnumber the tiles.
        if usesHead > 64, usesHead * 2 > uses.count {
            uses.removeFirst(usesHead)
            usesHead = 0
        }
    }

    private func evictOldest(sparing kept: CodeTileKey) -> Bool {
        while usesHead < uses.count {
            let use = uses[usesHead]
            usesHead += 1
            guard let entry = entries[use.key], entry.used == use.tick, use.key != kept else { continue }
            entries[use.key] = nil
            bytes -= entry.bytes
            return true
        }
        return false
    }

    /// Whether a tile not yet on screen may be drawn in this frame.
    var mayPrefetch: Bool {
        let now = CACurrentMediaTime()
        if now - frameStart > 1.0 / 240 { return true }
        return spentThisFrame < Self.prefetchBudget
    }

    private func note(spent: CFTimeInterval, at start: CFTimeInterval) {
        // Draws less than half a 120 Hz frame apart count as the same frame.
        if start - frameStart > 1.0 / 240 {
            frameStart = start
            spentThisFrame = 0
        }
        spentThisFrame += spent
    }
}

/// The text of a row that scrolls sideways, as a strip of tiles 512 points wide. Scrolling
/// moves the strip; a tile is drawn only the first time it comes near the screen.
final class CodeTileView: UIView {
    private var tiles: [Int: CALayer] = [:]
    private var spare: [CALayer] = []
    private var source: Source?

    struct Source {
        var cache: CodeTileCache
        var row: Int
        var text: CodeRender.Block
        var height: CGFloat
        var style: CodeTextDrawing.Style
        var appearance: Int
        var scale: CGFloat
    }

    override init(frame: CGRect) {
        super.init(frame: frame)
        clipsToBounds = true
        isUserInteractionEnabled = false
        backgroundColor = .clear
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) is not used")
    }

    /// The scale `shift` is rounded to when there is no source to say.
    private var fallbackScale: CGFloat = 1

    /// `value` rounded to the nearest device pixel, never below zero. A tile placed between
    /// pixels is resampled, which softens its text.
    static func pixelAligned(_ value: CGFloat, scale: CGFloat) -> CGFloat {
        let scale = max(scale, 1)
        return max(0, (value * scale).rounded() / scale)
    }

    /// Shows `source`'s text, or nothing when nil.
    func show(_ source: Source?, shift: CGFloat, scale: CGFloat? = nil) {
        self.source = source
        if let scale { fallbackScale = scale }
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        for (_, layer) in tiles { recycle(layer) }
        tiles.removeAll(keepingCapacity: true)
        CATransaction.commit()
        scroll(to: shift)
    }

    /// Slides the text so that `shift` points of it are off the leading edge.
    func scroll(to shift: CGFloat) {
        let shift = Self.pixelAligned(shift, scale: source?.scale ?? fallbackScale)
        bounds.origin.x = shift
        guard let source else { return }
        let width = Self.width(of: source.text)
        let count = Int((width / CodeTileCache.tileWidth).rounded(.up))
        guard count > 0, bounds.width > 0 else { return }
        let first = max(0, Int((shift / CodeTileCache.tileWidth).rounded(.down)))
        let last = min(count - 1, Int(((shift + bounds.width) / CodeTileCache.tileWidth).rounded(.down)))
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        for (index, layer) in tiles where index < first || index > last {
            recycle(layer)
            tiles[index] = nil
        }
        if first <= last {
            for index in first...last where tiles[index] == nil {
                place(index, source: source)
            }
        }
        CATransaction.commit()
        // One tile either side is drawn ahead while the frame has time to spare.
        for index in [last + 1, first - 1] where index >= 0 && index < count {
            let key = CodeTileKey(row: source.row, tile: index, appearance: source.appearance)
            guard !source.cache.contains(key), source.cache.mayPrefetch else { continue }
            _ = source.cache.image(key) { Self.drawTile(index, source: source) }
        }
    }

    private func place(_ index: Int, source: Source) {
        let key = CodeTileKey(row: source.row, tile: index, appearance: source.appearance)
        guard let image = source.cache.image(key, draw: { Self.drawTile(index, source: source) }) else { return }
        let layer = spare.popLast() ?? CALayer()
        layer.contents = image
        layer.contentsScale = source.scale
        layer.frame = CGRect(x: CGFloat(index) * CodeTileCache.tileWidth, y: 0, width: CodeTileCache.tileWidth, height: source.height)
        self.layer.addSublayer(layer)
        tiles[index] = layer
    }

    private func recycle(_ layer: CALayer) {
        layer.removeFromSuperlayer()
        layer.contents = nil
        spare.append(layer)
    }

    static func width(of text: CodeRender.Block) -> CGFloat {
        guard let last = text.offsets.indices.last else { return 0 }
        return text.offsets[last] + text.widths[last]
    }

    /// Tile `index` of the text, transparent where nothing is drawn.
    static func drawTile(_ index: Int, source: Source) -> CGImage? {
        let minX = CGFloat(index) * CodeTileCache.tileWidth
        let span = minX...(minX + CodeTileCache.tileWidth)
        guard source.text.offsets.indices.contains(where: {
            source.text.offsets[$0] < span.upperBound && source.text.offsets[$0] + source.text.widths[$0] > span.lowerBound
        }) else { return nil }
        let format = UIGraphicsImageRendererFormat.preferred()
        format.scale = source.scale
        format.opaque = false
        let size = CGSize(width: CodeTileCache.tileWidth, height: source.height)
        let image = UIGraphicsImageRenderer(size: size, format: format).image { context in
            CodeTextDrawing.draw(
                source.text, in: context.cgContext, originX: -minX, visible: 0...CodeTileCache.tileWidth, style: source.style
            )
        }
        return image.cgImage
    }
}
