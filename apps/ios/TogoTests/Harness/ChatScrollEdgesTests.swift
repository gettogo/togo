import SwiftUI
import Testing
import UIKit
@testable import Togo

@MainActor struct ChatScrollEdgesTests {
    @Test func restingEndsAndShortConversationsStayOpaque() {
        let start = ChatScrollEdgeLengths(offset: -16, topInset: 16, container: 500, content: 1500)
        #expect(start.top == 0 && start.bottom == 24)
        let foot = ChatScrollEdgeLengths(offset: 984, topInset: 16, container: 500, content: 1500)
        #expect(foot.top == 24 && foot.bottom == 0)
        let short = ChatScrollEdgeLengths(offset: -16, topInset: 16, container: 500, content: 100)
        #expect(short == ChatScrollEdgeLengths())
        let bounce = ChatScrollEdgeLengths(offset: 1000, topInset: 16, container: 500, content: 1500)
        #expect(bounce.bottom == 0)
    }

    @Test func onlyTheOverflowingPartOfAnEdgeFades() {
        let nearStart = ChatScrollEdgeLengths(offset: -10, topInset: 16, container: 500, content: 1500)
        #expect(nearStart.top == 6 && nearStart.bottom == 24)
        let nearFoot = ChatScrollEdgeLengths(offset: 980, topInset: 16, container: 500, content: 1500)
        #expect(nearFoot.bottom == 4)
        let middle = ChatScrollEdgeLengths(offset: 500, topInset: 16, container: 500, content: 1500)
        #expect(middle.top == 24 && middle.bottom == 24)
    }

    private func alpha(_ image: CGImage, y: Int) throws -> UInt8 {
        var pixels = [UInt8](repeating: 0, count: 4 * image.width * image.height)
        let space = CGColorSpaceCreateDeviceRGB()
        try pixels.withUnsafeMutableBytes { bytes in
            let context = try #require(CGContext(data: bytes.baseAddress, width: image.width, height: image.height,
                bitsPerComponent: 8, bytesPerRow: 4 * image.width, space: space,
                bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
            context.draw(image, in: CGRect(x: 0, y: 0, width: image.width, height: image.height))
        }
        return pixels[4 * (y * image.width + image.width / 2) + 3]
    }

    @Test(arguments: [ColorScheme.light, .dark])
    func renderedMaskDissolvesBothEdgesWithoutChangingTheMiddle(scheme: ColorScheme) throws {
        let lengths = ChatScrollEdgeLengths(offset: 500, topInset: 16, container: 500, content: 1500)
        let renderer = ImageRenderer(content: Rectangle().fill(.white)
            .frame(width: 40, height: 100).mask { ChatScrollEdgeMask(lengths: lengths) }
            .environment(\.colorScheme, scheme))
        renderer.scale = 1
        let image = try #require(renderer.cgImage)
        #expect(try alpha(image, y: 0) < 16)
        #expect(try alpha(image, y: 12) > 100 && alpha(image, y: 12) < 160)
        #expect(try alpha(image, y: 50) == 255)
        #expect(try alpha(image, y: 87) > 100 && alpha(image, y: 87) < 160)
        #expect(try alpha(image, y: 99) < 16)
    }

    @Test func theFootMaskKeepsTheLastLineAndItsMarginFullyOpaque() throws {
        let lengths = ChatScrollEdgeLengths(offset: 984, topInset: 16, container: 500, content: 1500)
        let renderer = ImageRenderer(content: ChatScrollEdgeMask(lengths: lengths).frame(width: 40, height: 100))
        renderer.scale = 1
        let image = try #require(renderer.cgImage)
        #expect(try alpha(image, y: 83) == 255)
        #expect(try alpha(image, y: 99) == 255)
    }
}
