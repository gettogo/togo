import XCTest

/// The launch on a cold start: it plays every time the app starts from nothing, never holds up
/// the first screen, and is gone once it has played.
@MainActor final class LaunchUITests: XCTestCase {
    private func coldLaunch(_ arguments: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        // No `-ui-skip-launch`: the launch plays as it does for a person.
        app.launchArguments = ["-ui-fakes"] + arguments
        app.launch()
        return app
    }

    private func launchElement(_ app: XCUIApplication) -> XCUIElement {
        app.descendants(matching: .any)["launch"]
    }

    /// Whether the launch played and removed itself. It keeps the app busy while it plays, so
    /// `launch()`, which waits for the app to settle, returns once it is already gone: the app
    /// leaves a "launch.played" mark instead of a test racing to see it.
    private func launchPlayed(_ app: XCUIApplication) -> Bool {
        app.descendants(matching: .any)["launch.played"].waitForExistence(timeout: 8)
            && !launchElement(app).exists
    }

    func testEveryColdLaunchPlaysTheLaunchAndThenRemovesIt() {
        for attempt in 1...2 {
            let app = coldLaunch()
            XCTAssertTrue(launchPlayed(app), "Cold launch \(attempt) did not play the launch and remove it")
            // Each launch starts with a fresh defaults suite, so the guide is unseen and opens.
            XCTAssertTrue(app.buttons["Skip"].waitForExistence(timeout: 5), "The guide did not open over the machines")
            app.buttons["Skip"].tap()
            XCTAssertTrue(app.rosterMenu.waitForExistence(timeout: 5), "The roster is not there after the launch")
            app.terminate()
        }
    }

    /// A first launch that finds machines already here, as from iCloud, never shows the welcome
    /// that offers the guide, so the guide opens once after the launch, then the roster.
    func testFirstLaunchWithMachinesOpensTheGuideOnce() {
        let app = coldLaunch()
        XCTAssertTrue(launchElement(app).waitForNonExistence(timeout: 8))
        XCTAssertTrue(app.buttons["Skip"].waitForExistence(timeout: 5), "The guide did not open by itself")
        app.buttons["Skip"].tap()
        XCTAssertTrue(app.rosterMenu.waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["Skip"].waitForExistence(timeout: 2), "The guide opened a second time")
    }

    /// A first run lands on the first-run screen, calm: no guide opens by itself.
    func testFreshInstallLandsOnFirstRunWithNoGuide() {
        let app = coldLaunch(["-ui-first-run"])
        XCTAssertTrue(launchPlayed(app), "A first run did not play the launch and remove it")
        XCTAssertTrue(app.buttons["Add a machine"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["How it works"].exists)
        // Give a cover time to arrive, if one were coming.
        XCTAssertFalse(app.buttons["Skip"].waitForExistence(timeout: 2), "The guide opened by itself")
        XCTAssertTrue(app.buttons["Add a machine"].isHittable)
    }
}
