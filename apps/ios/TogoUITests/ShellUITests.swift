import XCTest

/// The roster's shell: its one menu on every size, and the roster column beside the terminal
/// in a regular-width window (iPad, an open iPhone Duo), where those tests skip on a phone.
@MainActor final class ShellUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    /// Launches and waits for the roster column beside the terminal, or skips where the window
    /// is compact.
    private func launchShell() throws -> XCUIApplication {
        let app = launchTogo()
        XCTAssertTrue(app.rosterMenu.waitForExistence(timeout: 10), "The app never loaded")
        guard app.windows.firstMatch.frame.width >= 700 else {
            throw XCTSkip("A compact window has no roster column")
        }
        return app
    }

    /// New session is a button in the roster's bar on every size.
    func testMenuOpensNewSession() {
        let app = launchTogo()
        app.pickFromRosterMenu("New session")
        XCTAssertTrue(app.navigationBars["Machine"].waitForExistence(timeout: 5), "The new-session sheet opened")
    }

    /// New session and Settings are bar buttons; the menu holds the rarer things.
    func testRosterBarHoldsOneMenu() {
        let app = launchTogo()
        XCTAssertTrue(app.buttons["roster.settings"].waitForExistence(timeout: 10))
        assertTappable(app.rosterMenu)
        for id in ["roster.newSession", "roster.settings", "roster.filter"] {
            XCTAssertTrue(app.buttons[id].exists, "The roster's bar has no \(id)")
        }
        XCTAssertFalse(app.buttons["Debug tools"].exists, "Debug lives in Settings")
        app.rosterMenu.tap()
        for item in ["Status display", "Add machine", "Archive"] {
            XCTAssertTrue(app.buttons[item].waitForExistence(timeout: 5), "The menu has no \(item)")
        }
    }

    /// The status display comes from the menu and has a way back by touch.
    func testMonitorOpensAndCloses() {
        let app = launchTogo()
        app.pickFromRosterMenu("Status display")
        let done = app.buttons["monitor.done"]
        XCTAssertTrue(done.waitForExistence(timeout: 5), "The status display has no way out")
        done.tap()
        XCTAssertTrue(done.waitForNonExistence(timeout: 5), "Done left the status display up")
    }

    /// Settings is a sheet in every layout; closing it and tapping a line shows the session.
    func testSessionShowsAfterSettings() throws {
        let app = try launchShell()
        app.pickFromRosterMenu("Settings")
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
        app.pickFromRosterMenu("Settings")
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
