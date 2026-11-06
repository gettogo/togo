import CoreGraphics
import Testing
import UIKit
@testable import Togo

@MainActor struct CodeTileCacheTests {
    /// A tile image of `width` x `height` pixels, 4 bytes each.
    private func image(width: Int = 16, height: Int = 4) -> CGImage? {
        let context = CGContext(
            data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: width * 4,
            space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue
        )
        return context?.makeImage()
    }

    private func key(_ row: Int, _ tile: Int = 0) -> CodeTileKey {
        CodeTileKey(row: row, tile: tile, appearance: 0)
    }

    private func fill(_ cache: CodeTileCache, rows: Range<Int>) {
        for row in rows { _ = cache.image(key(row)) { image() } }
    }

    @Test func memoryWarningEmptiesTheCache() {
        let center = NotificationCenter()
        let cache = CodeTileCache(notifications: center)
        fill(cache, rows: 0..<5)
        #expect(cache.count == 5)
        center.post(name: UIApplication.didReceiveMemoryWarningNotification, object: nil)
        #expect(cache.count == 0, "tiles are only a cache; a memory warning drops them all")
        #expect(cache.bytes == 0)
    }

    @Test func goingToTheBackgroundEmptiesTheCache() {
        let center = NotificationCenter()
        let cache = CodeTileCache(notifications: center)
        fill(cache, rows: 0..<5)
        center.post(name: UIApplication.didEnterBackgroundNotification, object: nil)
        #expect(cache.count == 0)
    }

    @Test func byteBudgetEvictsLeastRecentlyUsed() throws {
        let tile = try #require(image())
        let size = tile.bytesPerRow * tile.height
        let cache = CodeTileCache(notifications: NotificationCenter(), byteLimit: size * 3)
        fill(cache, rows: 0..<3)
        // Row 0 is used again, so row 1 is now the oldest.
        _ = cache.image(key(0)) { Issue.record("a kept tile was drawn again"); return nil }
        fill(cache, rows: 3..<4)
        #expect(cache.bytes <= size * 3)
        #expect(cache.count == 3)
        #expect(cache.contains(key(0)))
        #expect(!cache.contains(key(1)), "the least recently used tile goes first")
        #expect(cache.contains(key(2)) && cache.contains(key(3)))
    }

    @Test func countLimitEvictsInUseOrder() {
        let cache = CodeTileCache(notifications: NotificationCenter())
        fill(cache, rows: 0..<CodeTileCache.capacity)
        // Touch every even row, so the odd ones are the oldest, lowest first.
        for row in stride(from: 0, to: CodeTileCache.capacity, by: 2) {
            _ = cache.image(key(row)) { nil }
        }
        fill(cache, rows: CodeTileCache.capacity..<(CodeTileCache.capacity + 3))
        #expect(cache.count == CodeTileCache.capacity)
        #expect(!cache.contains(key(1)) && !cache.contains(key(3)) && !cache.contains(key(5)))
        #expect(cache.contains(key(7)) && cache.contains(key(0)))
    }

    @Test func newTraitsDropTheTiles() {
        let renderer = CodeRenderer()
        fill(renderer.tiles, rows: 0..<3)
        let traits = UITraitCollection(preferredContentSizeCategory: .extraLarge)
        renderer.reset(rows: [], metrics: CodeMetrics(traits: traits, digits: 2, showsOld: true, showsGlyphs: true), traits: traits)
        #expect(renderer.tiles.count == 0, "tiles drawn for another text size or appearance are never shown")
    }

    @Test func tilesSitOnWholePixels() {
        #expect(CodeTileView.pixelAligned(10.3, scale: 3) == 31.0 / 3)
        #expect(CodeTileView.pixelAligned(10.3, scale: 2) == 10.5)
        #expect(CodeTileView.pixelAligned(-0.2, scale: 2) == 0)
        let view = CodeTileView(frame: CGRect(x: 0, y: 0, width: 100, height: 20))
        view.show(nil, shift: 10.3, scale: 3)
        #expect(view.bounds.origin.x == 31.0 / 3, "a tile between pixels is resampled, which softens the text")
    }
}
