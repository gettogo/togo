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

    /// New session asks for the machine: the roster's bar button in a compact window, ⌘T beside
    /// the sidebar.
    func testMenuOpensNewSession() {
        let app = launchTogo()
        app.pickFromRosterMenu("New session")
        XCTAssertTrue(app.navigationBars["Machine"].waitForExistence(timeout: 5), "The new-session sheet opened")
    }

    /// New session, Settings and the filter show once each; there is no status display.
    /// Beside the sidebar the roster's bar has no New session: each machine's "+" starts one.
    func testEachControlShowsOnce() {
        let app = launchTogo()
        XCTAssertTrue(app.buttons["roster.settings"].waitForExistence(timeout: 10))
        assertTappable(app.rosterMenu)
        let wide = app.windows.firstMatch.frame.width >= 700
        XCTAssertEqual(app.buttons.matching(identifier: "roster.newSession").count, wide ? 0 : 1, "New session is not where it belongs")
        for id in ["roster.settings", "roster.filter"] {
            XCTAssertEqual(app.buttons.matching(identifier: id).count, 1, "\(id) is not there once")
        }
        XCTAssertTrue(app.buttons["New Session on Studio"].exists, "Studio's heading has no +")
        XCTAssertFalse(app.buttons["Debug tools"].exists, "Debug lives in Settings")
        XCTAssertFalse(app.buttons["Status display"].exists, "The status display is gone")
        XCTAssertFalse(app.buttons["roster.statusDisplay"].exists, "The status display is gone")
    }

    /// A machine's "+" opens New session on that machine: the sheet skips the machine step.
    func testMachinePlusStartsOnThatMachine() {
        let app = launchTogo()
        let plus = app.buttons["New Session on Studio"]
        XCTAssertTrue(plus.waitForExistence(timeout: 10), "Studio's heading has no +")
        plus.tap()
        XCTAssertTrue(app.navigationBars["Folder"].waitForExistence(timeout: 5), "The sheet did not open on Studio's folders")
        XCTAssertFalse(app.navigationBars["Machine"].exists, "The sheet asked for the machine again")
    }

    /// Beside the sidebar Settings is the detail column's, with no session open and with one:
    /// never in the sidebar, which has no bottom bar.
    func testSettingsIsInTheDetailColumnBesideTheSidebar() throws {
        let app = try launchShell()
        let settings = app.buttons["roster.settings"]
        let filter = app.buttons["roster.filter"]
        XCTAssertTrue(filter.waitForExistence(timeout: 5))
        XCTAssertGreaterThanOrEqual(settings.frame.minX, filter.frame.maxX, "Settings sits in the sidebar")

        let row = app.row("01", "api")
        XCTAssertTrue(row.waitForExistence(timeout: 5))
        row.tap()
        let tab = app.scrollViews["Open sessions"].buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "01, ")).firstMatch
        XCTAssertTrue(tab.waitForExistence(timeout: 5), "The workspace shows the session")
        XCTAssertTrue(settings.waitForExistence(timeout: 5), "The workspace's bar has no Settings")
        XCTAssertEqual(app.buttons.matching(identifier: "roster.settings").count, 1)
        XCTAssertGreaterThanOrEqual(settings.frame.minX, filter.frame.maxX, "Settings sits in the sidebar")
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
