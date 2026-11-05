import XCTest

@MainActor final class WorkspaceUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    /// Opens 01 and then 04, so the workspace holds two tabs with 04 on screen.
    private func openTwoTabs() -> XCUIApplication {
        let app = launchTogo()
        let api = app.row("01", "api")
        XCTAssertTrue(api.waitForExistence(timeout: 10))
        api.tap()
        let back = app.buttons["Sessions"]
        XCTAssertTrue(back.waitForExistence(timeout: 5))
        back.tap()
        let appRow = app.row("04", "app")
        XCTAssertTrue(appRow.waitForExistence(timeout: 5))
        appRow.tap()
        XCTAssertTrue(chip(app, "04").waitForExistence(timeout: 5))
        return app
    }

    /// A tab chip reads its bib, then its agent.
    private func chip(_ app: XCUIApplication, _ bib: String) -> XCUIElement {
        app.scrollViews["Open sessions"].buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "\(bib), ")).firstMatch
    }

    private func waitUntilSelected(_ element: XCUIElement, timeout: TimeInterval = 5) -> Bool {
        let selected = NSPredicate(format: "isSelected == true")
        return XCTWaiter.wait(for: [expectation(for: selected, evaluatedWith: element)], timeout: timeout) == .completed
    }

    func testSwipingTheTerminalChangesTabs() {
        let app = openTwoTabs()
        let api = chip(app, "01"), appChip = chip(app, "04")
        XCTAssertTrue(waitUntilSelected(appChip))

        app.swipeRight()
        XCTAssertTrue(waitUntilSelected(api))
        app.swipeLeft()
        XCTAssertTrue(waitUntilSelected(appChip))
    }

    func testTabChipsMeetTheMinimumSize() {
        let app = openTwoTabs()
        XCTAssertEqual(chip(app, "01").label, "01, Claude Code")
        assertTappable(chip(app, "01"))
        assertTappable(chip(app, "04"))
        assertTappable(app.buttons["Sessions"])
        assertTappable(app.buttons["Session actions"])
        audit(app, glass: ["Sessions"])
    }

    func testChipTapSelectsItsTab() {
        let app = openTwoTabs()
        let api = chip(app, "01")
        api.tap()
        XCTAssertTrue(waitUntilSelected(api))
    }
}
