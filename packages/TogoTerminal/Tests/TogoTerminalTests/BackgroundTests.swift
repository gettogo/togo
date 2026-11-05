import Testing
import UIKit
@testable import TogoTerminal

/// The canvas shows the theme background where no cell sets its own, with the Core Graphics
/// renderer every terminal starts on and that previews and snapshots keep.
@MainActor
struct BackgroundTests {
    @Test(arguments: ["beacon-dark", "beacon-light"])
    func emptyCellsShowTheThemeBackground(themeID: String) throws {
        let theme = try #require(BuiltInThemes.all.first { $0.id == themeID })
        let harness = EngineHarness(width: 240, height: 160)
        harness.engine.apply(theme)
        harness.feed("hello")

        let pixel = try renderedPixel(of: harness.engine.view, at: CGPoint(x: 200, y: 120))
        #expect(pixel == RGB(hex: theme.background))
    }

    @Test func themeSwitchKeepsTheBackground() throws {
        let harness = EngineHarness(width: 240, height: 160)
        harness.engine.apply(BuiltInThemes.beaconLight)
        harness.engine.apply(BuiltInThemes.beaconDark)

        let pixel = try renderedPixel(of: harness.engine.view, at: CGPoint(x: 200, y: 120))
        #expect(pixel == RGB(hex: BuiltInThemes.beaconDark.background))
        #expect(harness.engine.backgroundColor == harness.engine.view.layer.backgroundColor.map(UIColor.init))
    }

    /// Composites the view as the screen would: its layer background under the backing store
    /// that `draw(_:)` filled.
    private func renderedPixel(of view: UIView, at point: CGPoint) throws -> RGB {
        view.layer.display()
        try #require(view.layer.contents != nil, "the view never drew")

        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        format.opaque = true
        let image = UIGraphicsImageRenderer(bounds: view.bounds, format: format).image { context in
            view.layer.render(in: context.cgContext)
        }
        let cgImage = try #require(image.cgImage)
        let data = try #require(cgImage.dataProvider?.data)
        let bytes = try #require(CFDataGetBytePtr(data))
        let offset = Int(point.y) * cgImage.bytesPerRow + Int(point.x) * cgImage.bitsPerPixel / 8
        let order = cgImage.bitmapInfo.rawValue & CGBitmapInfo.byteOrderMask.rawValue
        // Little-endian 32-bit pixels are stored BGRA.
        let bgra = order == CGBitmapInfo.byteOrder32Little.rawValue
        return RGB(red: bytes[offset + (bgra ? 2 : 0)], green: bytes[offset + 1], blue: bytes[offset + (bgra ? 0 : 2)])
    }
}
