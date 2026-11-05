import XCTest

/// Run by scripts/e2e-push.sh, which passes its values as `TEST_RUNNER_TOGO_E2E_*`; without
/// them each test skips, so ordinary UI test runs leave them alone.
///
/// `testAllowNotifications` launches the app for the push check and allows notifications.
/// `testApproveFromNotification` runs once a sealed push has reached the simulator: it finds
/// the request in Notification Center, checks what it shows and answers it from there.
final class LockScreenApproveUITests: XCTestCase {
    @MainActor private var springboard: XCUIApplication { XCUIApplication(bundleIdentifier: "com.apple.springboard") }

    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor func testAllowNotifications() throws {
        guard let args = ProcessInfo.processInfo.environment["TOGO_E2E_ARGS"], !args.isEmpty else {
            throw XCTSkip("Runs only from scripts/e2e-push.sh")
        }
        let app = XCUIApplication()
        app.launchArguments = args.split(separator: " ").map(String.init)
        app.launch()
        let allow = springboard.alerts.buttons["Allow"]
        XCTAssertTrue(allow.waitForExistence(timeout: 20), "No notification permission question")
        allow.tap()
        // Registration with APNs and the device file follow the answer.
        _ = XCTWaiter.wait(for: [XCTestExpectation(description: "registration")], timeout: 10)
    }

    @MainActor func testApproveFromNotification() throws {
        guard let question = ProcessInfo.processInfo.environment["TOGO_E2E_QUESTION"], !question.isEmpty else {
            throw XCTSkip("Runs only from scripts/e2e-push.sh")
        }
        springboard.activate()
        let top = springboard.coordinate(withNormalizedOffset: CGVector(dx: 0.3, dy: 0.01))
        top.press(forDuration: 0.1, thenDragTo: springboard.coordinate(withNormalizedOffset: CGVector(dx: 0.3, dy: 0.7)))

        let shown = springboard.descendants(matching: .any)
            .matching(NSPredicate(format: "label CONTAINS %@", question)).firstMatch
        XCTAssertTrue(shown.waitForExistence(timeout: 10), "No notification shows the question")
        let label = shown.label
        XCTAssertFalse(label.unicodeScalars.contains { (0x2500...0x259F).contains($0.value) }, "Box drawing survived")
        XCTAssertFalse(label.contains("Needs you"), "The generic alert was not replaced")

        shown.press(forDuration: 1.2)
        let approve = springboard.buttons["Approve"]
        if approve.waitForExistence(timeout: 3) {
            approve.tap()
        } else {
            // Requests whose options carry no approve role offer no actions; the script answers
            // through the app instead.
            springboard.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.05)).tap()
        }
    }
}
