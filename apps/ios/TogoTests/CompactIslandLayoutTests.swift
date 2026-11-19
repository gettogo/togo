import SwiftUI
import Testing
import TogoPush
import UIKit
@testable import Togo

@MainActor struct CompactIslandLayoutTests {
    private let now = Date(timeIntervalSince1970: 1_900_000_000)

    private func clock(age: Int) -> some View {
        let state = TogoActivityAttributes.ContentState(
            bib: "01", agent: "OMP", status: "working", waiting: 0,
            since: Int(now.timeIntervalSince1970) - age, requestId: nil, command: nil
        )
        let card = LiveCard(agent: "omp", status: "working", waiting: 0, requestId: nil,
            command: nil, context: nil, machine: nil, stale: false)
        return IslandCompactTrailing(state: state, card: card)
            .environment(\.liveClockFrozenAt, now)
    }

    @Test(arguments: [CGFloat(26), CGFloat(36), CGFloat(52)], [37, 3661, 360_001])
    func elapsedFitsTheCompactRegion(width: CGFloat, age: Int) {
        let host = UIHostingController(rootView: clock(age: age))
        let size = host.sizeThatFits(in: .init(width: width, height: 32))
        #expect(size.width <= width + 0.5, "Compact elapsed must respect the width offered by either island orientation")
        #expect(size.height <= 32)
    }

    @Test func aShortElapsedReadingIsCentredWithinItsRegion() throws {
        let renderer = ImageRenderer(content: clock(age: 37).frame(width: 52, height: 32))
        renderer.scale = 2
        let image = try #require(renderer.cgImage)
        let width = image.width, height = image.height
        var pixels = [UInt8](repeating: 0, count: width * height * 4)
        let bounds: CGRect = try pixels.withUnsafeMutableBytes { bytes in
            let context = try #require(CGContext(data: bytes.baseAddress, width: width, height: height,
                bitsPerComponent: 8, bytesPerRow: width * 4, space: CGColorSpaceCreateDeviceRGB(),
                bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
            context.draw(image, in: .init(x: 0, y: 0, width: CGFloat(width), height: CGFloat(height)))
            var left = width, right = -1
            for y in 0..<height {
                for x in 0..<width where bytes[(y * width + x) * 4 + 3] > 40 {
                    left = min(left, x); right = max(right, x)
                }
            }
            return CGRect(x: CGFloat(left), y: 0, width: CGFloat(max(0, right - left + 1)), height: CGFloat(height))
        }
        #expect(bounds.width > 0)
        #expect(abs(bounds.midX - CGFloat(width) / 2) <= 4, "The elapsed digits must be centred, not trailing in a wider hidden placeholder")
    }
}
