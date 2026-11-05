import SnapshotTesting
import SwiftUI
import Testing
@testable import Togo

@MainActor
struct LaunchSnapshotTests {
    /// The mark covers under 1% of the screen, so the default 1% pixel tolerance would pass a
    /// frame with the mark missing.
    private static let precision: Float = 1
    private static let perceptualPrecision: Float = 0.99

    @Test(arguments: [0, 300, 600, 900])
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

    @Test func reduceMotionFrame() {
        assertScreen(named: "launch-reduce-motion", precision: Self.precision, perceptualPrecision: Self.perceptualPrecision) {
            LaunchOverlayFrame(frame: LaunchFrame(time: .zero, reduceMotion: true))
        }
    }
}
