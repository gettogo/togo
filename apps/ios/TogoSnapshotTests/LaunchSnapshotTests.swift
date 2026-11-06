import SnapshotTesting
import SwiftUI
import Testing
import UIKit
@testable import Togo

@MainActor
struct LaunchSnapshotTests {
    /// The mark covers under 1% of the screen, so the default 1% pixel tolerance would pass a
    /// frame with the mark missing.
    private static let precision: Float = 1
    private static let perceptualPrecision: Float = 0.99

    /// The whole mark, the walk, the beacon lighting, and the hand-off half way.
    @Test(arguments: [0, 280, 640, 920])
    func launchFrame(milliseconds: Int) {
        let frame = LaunchFrame(time: .milliseconds(milliseconds), reduceMotion: false)
        assertScreen(named: "launch-\(milliseconds)ms", precision: Self.precision, perceptualPrecision: Self.perceptualPrecision) {
            ZStack {
                StateView(
                    kind: .empty, title: "No machines yet",
                    message: "Add a Mac or Linux machine to see its sessions here."
                )
                LaunchOverlayFrame(frame: frame)
            }
        }
    }

    /// On a first run the mark shrinks into the first-run screen's own mark.
    @Test func handoffToFirstRun() {
        let frame = LaunchFrame(time: LaunchTimeline.handoffStart + LaunchTimeline.handoff / 2, reduceMotion: false)
        assertScreen(named: "launch-first-run-handoff", precision: Self.precision, perceptualPrecision: Self.perceptualPrecision) {
            FirstRunView(launchPlaying: true) {}
                .overlayPreferenceValue(LaunchTargetKey.self) { anchor in
                    GeometryReader { proxy in
                        LaunchOverlayFrame(frame: frame, target: anchor.map { proxy[$0] })
                    }
                    .ignoresSafeArea()
                }
        }
    }

    @Test func reduceMotionFrame() {
        assertScreen(named: "launch-reduce-motion", precision: Self.precision, perceptualPrecision: Self.perceptualPrecision) {
            LaunchOverlayFrame(frame: LaunchFrame(time: .zero, reduceMotion: true))
        }
    }
}

/// The overlay's first frame against the system launch screen it replaces: the `LaunchMark`
/// image centred on ink at its own size, as iOS draws `UILaunchScreen`. Any difference in size
/// or place shows as a jump on every cold launch.
@MainActor
struct LaunchScreenMatchTests {
    private static let screen = CGSize(width: 402, height: 874)

    @Test(arguments: [ColorScheme.light, .dark])
    func firstFrameIsTheLaunchScreen(scheme: ColorScheme) throws {
        let launchScreen = try #require(Self.pixels(
            ZStack {
                Color.togoInk
                Image("LaunchMark")
            }, scheme: scheme
        ))
        let firstFrame = try #require(Self.pixels(
            LaunchOverlayFrame(frame: LaunchFrame(time: .zero, reduceMotion: false)), scheme: scheme
        ))
        #expect(launchScreen.count == firstFrame.count)
        let screenMark = Self.markExtent(launchScreen)
        let frameMark = Self.markExtent(firstFrame)
        #expect(screenMark.count > 1_000, "the launch screen draws the mark")
        // PDF and SwiftUI anti-alias edges differently, so the test compares where the mark is
        // and how big it is, in pixels at 3x: a jump of even half a point would show here.
        #expect(abs(screenMark.centre.x - frameMark.centre.x) < 1.5, "the mark moves sideways at launch")
        #expect(abs(screenMark.centre.y - frameMark.centre.y) < 1.5, "the mark moves up or down at launch")
        #expect(abs(screenMark.box.width - frameMark.box.width) <= 3, "the mark changes width at launch")
        #expect(abs(screenMark.box.height - frameMark.box.height) <= 3, "the mark changes height at launch")
        #expect(abs(screenMark.count - frameMark.count) < screenMark.count / 10, "the mark's weight changes at launch")
    }

    /// The pixels that differ from the ink in the top-left corner: where the mark is drawn.
    private static func markExtent(_ bytes: [UInt8]) -> (box: CGRect, centre: CGPoint, count: Int) {
        let width = Int(screen.width * 3)
        let ink = Array(bytes[0..<4])
        var minX = Int.max, minY = Int.max, maxX = 0, maxY = 0, sumX = 0, sumY = 0, count = 0
        for index in stride(from: 0, to: bytes.count, by: 4) {
            let distance = (0..<3).reduce(0) { $0 + abs(Int(bytes[index + $1]) - Int(ink[$1])) }
            guard distance > 96 else { continue }
            let x = (index / 4) % width, y = (index / 4) / width
            minX = min(minX, x); maxX = max(maxX, x); minY = min(minY, y); maxY = max(maxY, y)
            sumX += x; sumY += y; count += 1
        }
        guard count > 0 else { return (.zero, .zero, 0) }
        return (
            CGRect(x: minX, y: minY, width: maxX - minX, height: maxY - minY),
            CGPoint(x: Double(sumX) / Double(count), y: Double(sumY) / Double(count)), count
        )
    }

    private static func pixels(_ view: some View, scheme: ColorScheme) -> [UInt8]? {
        let renderer = ImageRenderer(content: view
            .frame(width: screen.width, height: screen.height)
            .environment(\.colorScheme, scheme))
        renderer.scale = 3
        guard let image = renderer.cgImage else { return nil }
        let width = image.width, height = image.height
        var bytes = [UInt8](repeating: 0, count: width * height * 4)
        let drawn = bytes.withUnsafeMutableBytes { buffer -> Bool in
            guard let context = CGContext(
                data: buffer.baseAddress, width: width, height: height, bitsPerComponent: 8, bytesPerRow: width * 4,
                space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue
            ) else { return false }
            context.draw(image, in: CGRect(x: 0, y: 0, width: width, height: height))
            return true
        }
        return drawn ? bytes : nil
    }
}
