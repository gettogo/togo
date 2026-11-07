import XCTest

/// Ending a session from the roster must never take the app down. With the session's tab open
/// behind the roster, closing that last tab once rewrote `workspaceOpen` in the same pass as
/// the List deleted the line under its open swipe action, and the collection view crashed.
@MainActor final class RosterRemovalUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    private func end(_ row: XCUIElement, in app: XCUIApplication) {
        row.swipeLeft()
        guard let kill = app.visibleButton("End session") else { return XCTFail("Swiping the row offers no End session") }
        kill.tap()
        XCTAssertTrue(app.staticTexts["The agent stops and its screen is gone."].waitForExistence(timeout: 5))
        app.visibleButton("End session")?.tap()
    }

    func testEndingASessionWithAnOpenTabKeepsTheAppRunning() {
        let app = launchTogo()
        let api = app.row("01", "api")
        XCTAssertTrue(api.waitForExistence(timeout: 10))
        api.tap()
        let back = app.buttons["Sessions"]
        XCTAssertTrue(back.waitForExistence(timeout: 5))
        back.tap()
        XCTAssertTrue(api.waitForExistence(timeout: 5))
        end(api, in: app)
        XCTAssertTrue(api.waitForNonExistence(timeout: 5))
        XCTAssertEqual(app.state, .runningForeground)
        XCTAssertTrue(app.row("04", "app").exists)
    }
}
