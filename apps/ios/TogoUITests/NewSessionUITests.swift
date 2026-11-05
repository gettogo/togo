import XCTest

@MainActor final class NewSessionUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    func testNewSessionOpensInTheWorkspace() {
        let app = launchTogo()
        let plus = app.buttons["New session"]
        XCTAssertTrue(plus.waitForExistence(timeout: 10))
        plus.tap()

        let studio = app.buttons["Studio"]
        XCTAssertTrue(studio.waitForExistence(timeout: 5))
        XCTAssertTrue(app.navigationBars["Machine"].waitForExistence(timeout: 5))
        audit(app, glass: ["Cancel"], overList: true)
        studio.tap()

        let folder = app.buttons["~/src/app"]
        XCTAssertTrue(folder.waitForExistence(timeout: 5))
        audit(app, glass: ["Cancel"], overList: true)
        folder.tap()

        let name = app.textFields["Name (optional)"]
        XCTAssertTrue(name.waitForExistence(timeout: 5))
        audit(app, glass: ["Cancel"], overList: true)
        name.tap()
        name.typeText("payments")

        let start = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Start")).firstMatch
        XCTAssertTrue(start.isEnabled)
        assertTappable(start)
        start.tap()

        XCTAssertTrue(app.staticTexts["09, payments"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["Sessions"].exists, "The workspace shows its way back")
    }
}
