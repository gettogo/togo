import Darwin
import QuartzCore
import TogoKit
import UIKit
import XCTest
@testable import Togo

/// Scrolls the 10,000-line fixture diff top to bottom the way a fast fling at 120 Hz moves it,
/// one frame per step, and checks every frame and every row build against the budget.
@MainActor final class CodeLinesPerformanceTests: XCTestCase {
    private static let frameBudget = 1.0 / 120
    private static let step: CGFloat = 120

    private func makeView() throws -> (CodeLinesUIView, UIWindow) {
        let diff = GitDiff(diff: ReviewFixtures.bigDiff, truncated: true, binary: [])
        let file = try XCTUnwrap(DiffParser.parse(diff).first)
        let lines = file.hunks.flatMap(\.lines)
        let tokens = Tokenizer.tokenize(lines.map(\.text), language: .json)
        var byLine: [DiffLine: [TokenSpan]] = [:]
        for (line, spans) in zip(lines, tokens) { byLine[line] = spans }
        let rows = CodeRow.unified(file) { byLine[$0] ?? [] }
        XCTAssertGreaterThan(rows.count, 10_000)

        let scene = try XCTUnwrap(UIApplication.shared.connectedScenes.lazy.compactMap { $0 as? UIWindowScene }.first)
        let window = UIWindow(windowScene: scene)
        window.frame = CGRect(x: 0, y: 0, width: 402, height: 874)
        let view = CodeLinesUIView(frame: window.bounds)
        let controller = UIViewController()
        controller.view = view
        window.rootViewController = controller
        window.isHidden = false
        view.update(rows: rows, wrap: false, showsOldNumbers: true, scrollTo: nil, onLongPress: nil)
        window.layoutIfNeeded()
        return (view, window)
    }

    /// Seconds each frame took to lay out, configure new cells and draw.
    private func scroll(_ view: CodeLinesUIView) -> [Double] {
        let collection = view.collectionView
        let top = -collection.adjustedContentInset.top
        let bottom = collection.contentSize.height + collection.adjustedContentInset.bottom - collection.bounds.height
        collection.contentOffset = CGPoint(x: collection.contentOffset.x, y: top)
        collection.layoutIfNeeded()
        CATransaction.flush()
        var frames: [Double] = []
        var y = top
        while y < bottom {
            y = min(y + Self.step, bottom)
            let start = CACurrentMediaTime()
            collection.contentOffset.y = y
            collection.layoutIfNeeded()
            CATransaction.flush()
            frames.append(CACurrentMediaTime() - start)
        }
        return frames
    }

    /// Runnable threads per core on the Mac running the simulator, over the last minute.
    private static func hostLoad() -> Double {
        var load = [Double](repeating: 0, count: 1)
        guard getloadavg(&load, 1) == 1 else { return 0 }
        return load[0] / Double(ProcessInfo.processInfo.activeProcessorCount)
    }

    /// `TOGO_ENFORCE_FRAME_BUDGET=1` in the test's environment enforces the budget whatever the
    /// load. xcodebuild passes it as `TEST_RUNNER_TOGO_ENFORCE_FRAME_BUDGET=1`. Set it wherever
    /// the budget must hold, such as CI.
    nonisolated static func enforced(_ environment: [String: String] = ProcessInfo.processInfo.environment) -> Bool {
        environment["TOGO_ENFORCE_FRAME_BUDGET"] == "1"
    }

    /// Whether a missed budget is skipped: only when the Mac is overloaded and nothing asks
    /// for enforcement.
    nonisolated static func skips(missed: Bool, load: Double, enforced: Bool) -> Bool {
        missed && load > 1 && !enforced
    }

    /// A budget missed while other work holds every core of the Mac measures the Mac, not the
    /// code: the simulator shares its CPUs. Such a run is skipped, with the numbers, rather than
    /// passed. On a Mac with cores to spare, or with enforcement on, the budget is enforced as
    /// written.
    private func skipIfHostBusy(over budget: Bool, _ report: String) throws {
        let load = Self.hostLoad()
        try XCTSkipIf(
            Self.skips(missed: budget, load: load, enforced: Self.enforced()),
            "The Mac is overloaded (\(String(format: "%.1f", load)) runnable threads per core), so frame times measure it: \(report)"
        )
    }

    func testEnforcementOverridesTheBusyHostSkip() {
        XCTAssertTrue(Self.enforced(["TOGO_ENFORCE_FRAME_BUDGET": "1"]))
        XCTAssertFalse(Self.enforced([:]))
        XCTAssertTrue(Self.skips(missed: true, load: 3, enforced: false))
        XCTAssertFalse(Self.skips(missed: true, load: 3, enforced: true))
        XCTAssertFalse(Self.skips(missed: true, load: 0.5, enforced: false))
        XCTAssertFalse(Self.skips(missed: false, load: 3, enforced: false))
    }

    func testScrolls10kLinesWithinFrameBudget() throws {
        let (view, window) = try makeView()
        defer { window.isHidden = true }

        _ = scroll(view)
        view.renderer.reset(rows: view.rows, metrics: try XCTUnwrap(view.metrics), traits: view.traitCollection)
        let frames = scroll(view)
        let hitch = frames.reduce(0) { $0 + max(0, $1 - Self.frameBudget) }
        let ratio = hitch * 1_000 / (Double(frames.count) * Self.frameBudget)
        XCTAssertGreaterThan(frames.count, 1_500)
        let report = "hitch ratio \(ratio) ms/s, slowest frame \(frames.max() ?? 0) s"

        // A row's build cost is its best of three cold builds, so a moment the simulator spends
        // elsewhere does not count against the row.
        var best = [Double](repeating: .infinity, count: view.rows.count)
        for _ in 0..<3 {
            view.renderer.reset(rows: view.rows, metrics: try XCTUnwrap(view.metrics), traits: view.traitCollection)
            for index in view.rows.indices {
                let start = CACurrentMediaTime()
                _ = view.renderer.render(index, layout: view.layout)
                best[index] = min(best[index], CACurrentMediaTime() - start)
            }
        }
        let slowest = best.max() ?? 0
        let slowestIndex = best.firstIndex(of: slowest) ?? 0
        // Both budgets are measured before either can skip, so a skip never hides the other.
        try skipIfHostBusy(over: ratio >= 5 || slowest >= 0.004, "\(report), slowest row build \(slowest * 1_000) ms")
        XCTAssertLessThan(ratio, 5, report)
        XCTAssertLessThan(slowest, 0.004, "slowest row build \(slowest * 1_000) ms at row \(slowestIndex)")
        print("CodeLines perf: frames \(frames.count), hitch ratio \(ratio) ms/s, slowest frame \((frames.max() ?? 0) * 1_000) ms, slowest row build \(slowest * 1_000) ms")

        let metrics = try XCTUnwrap(view.metrics)
        measure(metrics: [XCTClockMetric()]) {
            view.renderer.reset(rows: view.rows, metrics: metrics, traits: view.traitCollection)
            _ = scroll(view)
        }
    }

    /// A fling across minified lines: every visible row redraws each frame as the code slides.
    func testFlingsAcross2000ColumnLinesWithinFrameBudget() throws {
        let scene = try XCTUnwrap(UIApplication.shared.connectedScenes.lazy.compactMap { $0 as? UIWindowScene }.first)
        let window = UIWindow(windowScene: scene)
        window.frame = CGRect(x: 0, y: 0, width: 402, height: 874)
        let view = CodeLinesUIView(frame: window.bounds)
        let controller = UIViewController()
        controller.view = view
        window.rootViewController = controller
        window.isHidden = false
        defer { window.isHidden = true }
        let line = String(String(repeating: "var a=b+1,c=\"x\";", count: 120).prefix(ReviewLimits.maxColumns))
        let lines = Array(repeating: line, count: 60)
        let text = TextFile(lines: lines, clipped: Set(lines.indices))
        let rows = CodeRow.file(text, tokens: Tokenizer.tokenize(lines, language: .javascript))
        view.update(rows: rows, wrap: false, showsOldNumbers: false, scrollTo: nil, onLongPress: nil)
        window.layoutIfNeeded()
        CATransaction.flush()

        let collection = view.collectionView
        let end = collection.contentSize.width - collection.bounds.width
        XCTAssertGreaterThan(end, 10_000)
        var frames: [Double] = []
        var x = collection.contentOffset.x
        while x < end {
            x = min(x + Self.step, end)
            let start = CACurrentMediaTime()
            collection.contentOffset.x = x
            collection.layoutIfNeeded()
            CATransaction.flush()
            frames.append(CACurrentMediaTime() - start)
        }
        let hitch = frames.reduce(0) { $0 + max(0, $1 - Self.frameBudget) }
        let ratio = hitch * 1_000 / (Double(frames.count) * Self.frameBudget)
        let sorted = frames.sorted()
        let median = sorted[sorted.count / 2]
        let report = "hitch ratio \(ratio) ms/s, median \(median * 1_000) ms, slowest \((sorted.last ?? 0) * 1_000) ms"
        print("CodeLines fling: frames \(frames.count), \(report)")
        // New tiles come into view as the code slides, so this guards the typical frame.
        XCTAssertLessThan(median, Self.frameBudget, report)
    }

    /// A fast sideways fling over a diff of 2,000-column lines, decelerating the way a scroll
    /// view does, there and back. Sliding only moves tiles drawn once, so frames stay in budget.
    func testNoWrapHorizontalFlingUnderHitchBudget() throws {
        let file = try XCTUnwrap(DiffParser.parse(GitDiff(diff: ReviewFixtures.wideDiff, truncated: false, binary: [])).first)
        let lines = file.hunks.flatMap(\.lines)
        let tokens = Tokenizer.tokenize(lines.map(\.text), language: .json)
        var byLine: [DiffLine: [TokenSpan]] = [:]
        for (line, spans) in zip(lines, tokens) { byLine[line] = spans }
        let rows = CodeRow.unified(file) { byLine[$0] ?? [] }
        let scene = try XCTUnwrap(UIApplication.shared.connectedScenes.lazy.compactMap { $0 as? UIWindowScene }.first)
        let window = UIWindow(windowScene: scene)
        window.frame = CGRect(x: 0, y: 0, width: 402, height: 874)
        let view = CodeLinesUIView(frame: window.bounds)
        let controller = UIViewController()
        controller.view = view
        window.rootViewController = controller
        window.isHidden = false
        defer { window.isHidden = true }
        view.update(rows: rows, wrap: false, showsOldNumbers: true, scrollTo: nil, onLongPress: nil)
        window.layoutIfNeeded()
        CATransaction.flush()

        let collection = view.collectionView
        let start = -collection.adjustedContentInset.left
        let end = collection.contentSize.width - collection.bounds.width + collection.adjustedContentInset.right
        XCTAssertGreaterThan(end, 10_000)
        var frames: [Double] = []
        // `UIScrollView.DecelerationRate.normal` keeps 0.998 of the speed each millisecond.
        let decay = pow(0.998, Self.frameBudget * 1_000)
        for direction in [1.0, -1.0] {
            var velocity = 14_000.0 * direction
            var x = Double(collection.contentOffset.x)
            while abs(velocity) > 30 {
                x = min(max(x + velocity * Self.frameBudget, Double(start)), Double(end))
                velocity *= decay
                let begin = CACurrentMediaTime()
                collection.contentOffset.x = CGFloat(x)
                collection.layoutIfNeeded()
                CATransaction.flush()
                frames.append(CACurrentMediaTime() - begin)
                if x == Double(start) || x == Double(end) { break }
            }
        }
        let hitch = frames.reduce(0) { $0 + max(0, $1 - Self.frameBudget) }
        let ratio = hitch * 1_000 / (Double(frames.count) * Self.frameBudget)
        let report = "frames \(frames.count), hitch ratio \(ratio) ms/s, slowest \((frames.max() ?? 0) * 1_000) ms"
        print("CodeLines wide fling: \(report)")
        try skipIfHostBusy(over: ratio >= 5, report)
        XCTAssertLessThan(ratio, 5, report)
    }
}
