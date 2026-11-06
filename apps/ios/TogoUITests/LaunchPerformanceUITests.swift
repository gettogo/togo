import Darwin
import XCTest

/// Skips a timing budget only when the Mac is measurably overloaded, sampled before the test
/// makes load of its own. `TOGO_ENFORCE_FRAME_BUDGET=1` (passed to the runner as
/// `TEST_RUNNER_TOGO_ENFORCE_FRAME_BUDGET=1`) never skips.
@MainActor func skipBudgetIfOverloaded(_ budget: String) throws {
    let enforced = ProcessInfo.processInfo.environment["TOGO_ENFORCE_FRAME_BUDGET"] == "1"
    var load = [Double](repeating: 0, count: 1)
    guard !enforced, getloadavg(&load, 1) == 1 else { return }
    let perCore = load[0] / Double(ProcessInfo.processInfo.activeProcessorCount)
    try XCTSkipIf(
        perCore > 1,
        "\(budget) budget unverified: the Mac is overloaded (\(String(format: "%.1f", perCore)) runnable threads per core)"
    )
}

/// Cold launch until the first screen answers touches, measured by the system's launch metric
/// in a Release build: five launches, whose median must be under one second.
/// `make ios-launch-perf` runs it in Release and holds the median to the budget from the
/// result bundle, which XCTest does not expose to the test.
@MainActor final class LaunchPerformanceUITests: XCTestCase {
    static let runs = 5

    func testColdLaunchUnderBudget() throws {
        #if DEBUG
        throw XCTSkip("Launch budget unverified: a Debug build measures unoptimised code. Run `make ios-launch-perf`.")
        #else
        try skipBudgetIfOverloaded("Launch")
        let app = XCUIApplication()
        let options = XCTMeasureOptions()
        options.iterationCount = Self.runs
        measure(metrics: [XCTApplicationLaunchMetric(waitUntilResponsive: true)], options: options) {
            app.launch()
        }
        app.terminate()
        #endif
    }
}
