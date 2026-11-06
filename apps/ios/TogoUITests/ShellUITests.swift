import XCTest

/// The sidebar shell of a regular-width window: iPad and an open iPhone Duo. On a phone the
/// tests skip, since the sessions list has none of these controls.
@MainActor final class ShellUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    /// Launches and waits for the sidebar, or skips where the window is compact.
    private func launchShell() throws -> XCUIApplication {
        let app = launchTogo()
        XCTAssertTrue(app.buttons["Settings"].waitForExistence(timeout: 10), "The app never loaded")
        guard app.buttons["Add machine"].exists else {
            throw XCTSkip("A compact window has no sidebar")
        }
        return app
    }

    func testSidebarPlusOpensNewSession() throws {
        let app = try launchShell()
        let plus = app.navigationBars.buttons["New session"]
        XCTAssertTrue(plus.isHittable, "The plus sits in the sidebar's bar, not in an overflow menu")
        assertTappable(plus)
        plus.tap()
        XCTAssertTrue(app.navigationBars["Machine"].waitForExistence(timeout: 5), "The new-session sheet opened")
    }

    func testSidebarBarHasNoOverflow() throws {
        let app = try launchShell()
        XCTAssertTrue(app.navigationBars.buttons["Filter"].isHittable)
        XCTAssertFalse(app.buttons["More"].exists, "Nothing folds into a More menu")
        XCTAssertFalse(app.buttons["Debug tools"].exists, "Debug lives in Settings")
    }

    /// Settings is a sheet beside the sidebar; closing it and tapping a row shows the session.
    func testSessionShowsAfterSettings() throws {
        let app = try launchShell()
        app.buttons["Settings"].tap()
        XCTAssertTrue(app.navigationBars["Settings"].waitForExistence(timeout: 5))
        app.buttons["Close"].tap()
        XCTAssertTrue(app.navigationBars["Settings"].waitForNonExistence(timeout: 5), "Settings closed")

        let row = app.row("01", "api")
        XCTAssertTrue(row.waitForExistence(timeout: 5))
        row.tap()
        let tab = app.scrollViews["Open sessions"].buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "01, ")).firstMatch
        XCTAssertTrue(tab.waitForExistence(timeout: 5), "The workspace shows the session")
        XCTAssertFalse(app.navigationBars["Settings"].exists)
    }

    func testDebugOpensFromAboutAndCloses() throws {
        let app = launchTogo()
        let settings = app.buttons["Settings"]
        XCTAssertTrue(settings.waitForExistence(timeout: 10))
        settings.tap()
        let about = app.buttons["About"]
        XCTAssertTrue(about.waitForExistence(timeout: 5))
        about.tap()
        let developer = app.buttons["Developer"]
        XCTAssertTrue(developer.waitForExistence(timeout: 5))
        developer.tap()
        XCTAssertTrue(app.navigationBars["Debug"].waitForExistence(timeout: 5))
        app.navigationBars["Debug"].buttons["Close"].tap()
        XCTAssertTrue(app.navigationBars["Debug"].waitForNonExistence(timeout: 5), "Debug closed")
        XCTAssertTrue(app.navigationBars["About"].exists, "Back where Debug was opened")
    }
}
