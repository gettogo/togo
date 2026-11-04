import Foundation
import Testing
@testable import TogoTerminal

/// Parse cost of a full-screen TUI repaint, the shape Claude Code produces while streaming.
/// Runs only with `TEST_RUNNER_TOGO_PERF=1`; simulator numbers say nothing about the device
/// frame rate, which the owner checks on hardware. The figures go to a test attachment.
@MainActor
@Suite(.serialized) struct PerformanceTests {
    @Test(.enabled(if: ProcessInfo.processInfo.environment["TOGO_PERF"] == "1"))
    func fullScreenRepaintParseCost() {
        let h = EngineHarness(width: 1200, height: 900)
        let size = h.engine.size
        var frame = "\(esc)[?2026h\(esc)[H"
        for row in 0..<size.rows {
            frame += "\(esc)[\(31 + row % 6);1m" + String(repeating: "x", count: max(size.cols - 1, 0)) + "\(esc)[0m\r\n"
        }
        frame += "\(esc)[?2026l"
        let bytes = Array(frame.utf8)
        let clock = ContinuousClock()
        let frames = 600
        var total = Duration.zero
        for _ in 0..<frames {
            total += clock.measure { h.engine.feed(bytes[...]) }
        }
        let seconds = Double(total.components.seconds) + Double(total.components.attoseconds) / 1e18
        let perFrameMilliseconds = seconds * 1000 / Double(frames)
        let mebibytesPerSecond = Double(bytes.count * frames) / 1_048_576 / seconds
        let summary = String(
            format: "grid %dx%d, %d bytes per repaint, %.3f ms per repaint, %.1f MiB/s",
            size.cols, size.rows, bytes.count, perFrameMilliseconds, mebibytesPerSecond)
        Attachment.record(summary, named: "repaint-parse-cost.txt")
        #expect(perFrameMilliseconds < 100, Comment(rawValue: summary))
    }
}
