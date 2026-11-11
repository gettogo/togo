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
        XCTAssertTrue(chip(app, "04").waitForExistence(timeout: 5) || title(app, "app").waitForExistence(timeout: 5))
        return app
    }

    /// Whether the tabs are chips in a rail under the bar (regular width, a Duo's vertical bar);
    /// otherwise the compact bar's title menu lists them.
    private func hasRail(_ app: XCUIApplication) -> Bool {
        app.scrollViews["Open sessions"].exists
    }

    /// A tab chip reads its bib, then its agent.
    private func chip(_ app: XCUIApplication, _ bib: String) -> XCUIElement {
        app.scrollViews["Open sessions"].buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "\(bib), ")).firstMatch
    }

    /// The compact bar's title, which names the tab on screen and opens the tab menu.
    private func title(_ app: XCUIApplication, _ name: String) -> XCUIElement {
        app.navigationBars.buttons[name]
    }

    /// A tab in the open title menu, "01 api". Found by its label: a menu item does not carry
    /// its view's identifier. Roster rows read "01, api, …", with a comma.
    private func menuTab(_ app: XCUIApplication, _ bib: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "\(bib) ")).firstMatch
    }

    private func waitUntilSelected(_ element: XCUIElement, timeout: TimeInterval = 5) -> Bool {
        let selected = NSPredicate(format: "isSelected == true")
        return XCTWaiter.wait(for: [expectation(for: selected, evaluatedWith: element)], timeout: timeout) == .completed
    }

    /// A one-finger swipe belongs to the terminal: it never pages to another session (the
    /// terminal's two-finger swipe and the chips do).
    func testOneFingerSwipeKeepsTheTab() {
        let app = openTwoTabs()
        guard hasRail(app) else {
            XCTAssertTrue(title(app, "app").exists)
            app.swipeRight()
            XCTAssertTrue(title(app, "app").waitForExistence(timeout: 2))
            XCTAssertFalse(title(app, "api").exists)
            return
        }
        let appChip = chip(app, "04")
        XCTAssertTrue(waitUntilSelected(appChip))

        app.swipeRight()
        XCTAssertTrue(waitUntilSelected(appChip))
        XCTAssertFalse(chip(app, "01").isSelected)
    }

    func testTabChipsMeetTheMinimumSize() {
        let app = openTwoTabs()
        if hasRail(app) {
            XCTAssertEqual(chip(app, "01").label, "01, Claude Code")
            assertTappable(chip(app, "01"))
            assertTappable(chip(app, "04"))
        } else {
            // System bar items report their glyph's frame, not their touch area.
            XCTAssertTrue(title(app, "app").isHittable)
            title(app, "app").tap()
            XCTAssertTrue(menuTab(app, "01").waitForExistence(timeout: 5))
            assertTappable(menuTab(app, "01"))
            assertTappable(menuTab(app, "04"))
            app.tap()
        }
        assertTappable(app.buttons["Sessions"])
        XCTAssertTrue(app.buttons["Session actions"].isHittable)
        audit(app, glass: ["Sessions"])
    }

    func testChipTapSelectsItsTab() {
        let app = openTwoTabs()
        guard hasRail(app) else {
            title(app, "app").tap()
            let api = menuTab(app, "01")
            XCTAssertTrue(api.waitForExistence(timeout: 5))
            // A menu item tapped while the menu is still opening only closes the menu.
            app.waitForAnswersToArm()
            api.tap()
            XCTAssertTrue(title(app, "api").waitForExistence(timeout: 5), "Picking 01 in the title menu did not switch to it")
            return
        }
        let api = chip(app, "01")
        api.tap()
        XCTAssertTrue(waitUntilSelected(api))
    }
}
