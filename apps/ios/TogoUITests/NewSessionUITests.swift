import XCTest

@MainActor final class NewSessionUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    func testNewSessionOpensInTheWorkspace() {
        let app = launchTogo()
        app.pickFromRosterMenu("New session")

        let studio = app.buttons["Studio"]
        XCTAssertTrue(studio.waitForExistence(timeout: 5))
        XCTAssertTrue(app.navigationBars["Machine"].waitForExistence(timeout: 5))
        audit(app, glass: ["Close"], overList: true)
        studio.tap()

        let folder = app.buttons["~/src/app"]
        XCTAssertTrue(folder.waitForExistence(timeout: 5))
        audit(app, glass: ["Close"], overList: true)
        folder.tap()

        let name = app.textFields["Name (optional)"]
        XCTAssertTrue(name.waitForExistence(timeout: 5))
        audit(app, glass: ["Close"], overList: true)
        name.tap()
        name.typeText("payments")

        let start = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Start")).firstMatch
        XCTAssertTrue(start.isEnabled)
        assertTappable(start)
        start.tap()

        XCTAssertTrue(app.staticTexts["09, payments"].waitForExistence(timeout: 5))
        // The workspace is found by its tab strip: a cover with a back button on a phone, the
        // detail beside the sidebar on iPad and an open iPhone Duo.
        let tab = app.scrollViews["Open sessions"].buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "09, ")).firstMatch
        XCTAssertTrue(tab.waitForExistence(timeout: 5), "The workspace shows the new session's tab")
    }

    /// Every step can close the sheet, not only the first.
    func testAgentStepCloses() {
        let app = launchTogo()
        app.pickFromRosterMenu("New session")
        let studio = app.buttons["Studio"]
        XCTAssertTrue(studio.waitForExistence(timeout: 5))
        studio.tap()
        let folder = app.buttons["~/src/app"]
        XCTAssertTrue(folder.waitForExistence(timeout: 5))
        folder.tap()
        let name = app.textFields["Name (optional)"]
        XCTAssertTrue(name.waitForExistence(timeout: 5), "The agent step shows")

        let close = app.buttons["Close"]
        XCTAssertTrue(close.isHittable, "The agent step has a Close button")
        close.tap()
        XCTAssertTrue(name.waitForNonExistence(timeout: 5), "Close dismissed the sheet")
    }

    /// Runs on every size: phone, iPad, and iPhone Duo open, where the sidebar shell is up.
    func testFolderStepListsFolders() {
        let app = launchTogo()
        app.pickFromRosterMenu("New session")

        let studio = app.buttons["Studio"]
        XCTAssertTrue(studio.waitForExistence(timeout: 5))
        studio.tap()

        XCTAssertTrue(app.navigationBars["Folder"].waitForExistence(timeout: 5), "The folder step has its title")
        XCTAssertTrue(app.buttons["~/src/app"].waitForExistence(timeout: 5), "The folder step lists recent folders")
        XCTAssertTrue(app.buttons["Browse"].exists)
        app.buttons["~/src/app"].tap()
        XCTAssertTrue(app.textFields["Name (optional)"].waitForExistence(timeout: 5), "The agent step shows")
    }

    /// With one machine there is nothing to choose, so the sheet opens on the folder step.
    func testFolderStepListsFoldersForTheOnlyMachine() {
        let app = launchTogo(arguments: ["-ui-one-machine"])
        app.pickFromRosterMenu("New session")

        XCTAssertTrue(app.navigationBars["Folder"].waitForExistence(timeout: 5), "The folder step has its title")
        XCTAssertTrue(app.buttons["~/src/app"].waitForExistence(timeout: 5), "The folder step lists recent folders")
        app.buttons["Browse"].tap()
        // The back button sits in a vertical bar on iPhone Duo, so the page is found by its content.
        let use = app.buttons["Use this folder"]
        XCTAssertTrue(use.waitForExistence(timeout: 5), "Browsing shows its own page")
        XCTAssertTrue(app.navigationBars["Browse"].exists, "The browse page has its title")
        use.tap()
        XCTAssertTrue(app.textFields["Name (optional)"].waitForExistence(timeout: 5), "The agent step shows")
    }
}
