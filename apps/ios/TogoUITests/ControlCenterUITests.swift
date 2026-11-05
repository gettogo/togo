import XCTest

/// Launches the app on its scripted machines: Studio with sessions 01, 04 and scratch and a
/// request from 04, and Build box asleep with 02.
@MainActor func launchTogo(firstRun: Bool = false) -> XCUIApplication {
    let app = XCUIApplication()
    app.launchArguments = ["-ui-fakes", "-ui-skip-launch"] + (firstRun ? ["-ui-first-run"] : [])
    app.launch()
    return app
}

extension XCUIApplication {
    /// A session row, found by how VoiceOver reads it: bib, name, status, then folder.
    func row(_ bib: String, _ name: String) -> XCUIElement {
        buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "\(bib), \(name), ")).firstMatch
    }

    /// The button with this label that is on screen, for labels a dialog shares with a swipe action.
    func visibleButton(_ label: String) -> XCUIElement? {
        buttons.matching(identifier: label).allElementsBoundByIndex.last { $0.exists && $0.isHittable }
    }
}

extension XCTestCase {
    /// Fails on anything the system audit finds except its Dynamic Type checks. Those flag text
    /// styles that do scale (list headers, bar buttons), and the snapshot suite records every
    /// screen at the largest size instead.
    /// `glass` names Liquid Glass buttons whose contrast the audit measures against the glass
    /// edge and shadow rather than the fill behind the text; their light and dark snapshots
    /// show the label at full ink or snow.
    /// `overList` is set while a form sheet sits over the control center. On iPad the list stays
    /// visible around the sheet, dimmed and cut off at its edge, and is rightly hidden from
    /// VoiceOver while the sheet is up; the audit still reads that text off the screen and
    /// reports it with no element. Only that report is let through, never one with an element.
    @MainActor func audit(
        _ app: XCUIApplication, glass: Set<String> = [], overList: Bool = false,
        file: StaticString = #filePath, line: UInt = #line
    ) {
        do {
            try app.performAccessibilityAudit(for: .all.subtracting([.textClipped, .dynamicType])) { issue in
                if issue.auditType == .contrast, let label = issue.element?.label, glass.contains(label) {
                    return true
                }
                if overList, issue.auditType == .elementDetection, issue.element == nil {
                    return true
                }
                let element = issue.element.map { "\($0.elementType.rawValue) '\($0.label)'" } ?? "screen"
                XCTFail(
                    "\(issue.compactDescription) (audit \(issue.auditType.rawValue)) on \(element): \(issue.detailedDescription)",
                    file: file, line: line
                )
                return true
            }
        } catch {
            XCTFail("Accessibility audit: \(error)", file: file, line: line)
        }
    }

    func assertTappable(_ element: XCUIElement, file: StaticString = #filePath, line: UInt = #line) {
        let size = element.frame.size
        XCTAssertGreaterThanOrEqual(size.width, 44, "\(element.label) is too narrow", file: file, line: line)
        XCTAssertGreaterThanOrEqual(size.height, 44, "\(element.label) is too short", file: file, line: line)
    }
}

@MainActor final class ControlCenterUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    func testRowsAndCardsReadAsOneSentence() {
        let app = launchTogo()
        let row = app.row("01", "api")
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        XCTAssertEqual(row.label, "01, api, Working, in ~/src/api")
        XCTAssertEqual(row.value as? String, "Editing routes.ts")

        let card = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "Run npm test?")).firstMatch
        XCTAssertTrue(card.exists)
        XCTAssertTrue(card.label.contains("04"))
        XCTAssertTrue(card.label.contains("Studio"))
        audit(app, glass: ["New session"])
    }

    func testRequestAnswersMeetTheMinimumSize() {
        let app = launchTogo()
        let yes = app.buttons["Yes"]
        XCTAssertTrue(yes.waitForExistence(timeout: 10))
        assertTappable(yes)
        assertTappable(app.buttons["No"])
    }

    func testAnsweringARequestClearsItsCard() {
        let app = launchTogo()
        let yes = app.buttons["Yes"]
        XCTAssertTrue(yes.waitForExistence(timeout: 10))
        yes.tap()
        let title = app.staticTexts["Run npm test?"]
        XCTAssertTrue(title.waitForNonExistence(timeout: 5))
    }

    func testKillAsksFirstThenRemovesTheRow() {
        let app = launchTogo()
        let row = app.row("01", "api")
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        row.swipeLeft()

        guard let kill = app.visibleButton("Kill") else { return XCTFail("Swiping the row offers no Kill") }
        assertTappable(kill)
        kill.tap()

        XCTAssertTrue(app.staticTexts["The agent stops and its screen is gone."].waitForExistence(timeout: 5))
        XCTAssertTrue(row.exists, "The row stays until the kill is confirmed")
        app.visibleButton("Kill")?.tap()
        XCTAssertTrue(row.waitForNonExistence(timeout: 5))
    }

    func testAddedMachineJoinsTheList() {
        let app = launchTogo(firstRun: true)
        let add = app.buttons["Add a machine"]
        XCTAssertTrue(add.waitForExistence(timeout: 10), "First run offers no Add a machine")
        audit(app)
        add.tap()

        let discovered = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Sam's MacBook Pro")).firstMatch
        XCTAssertTrue(discovered.waitForExistence(timeout: 5), "Discovery lists no machine")
        XCTAssertTrue(app.navigationBars["Add a machine"].waitForExistence(timeout: 5), "The sheet has no title bar")
        audit(app, glass: ["Cancel"])
        discovered.tap()

        let user = app.textFields.matching(
            NSPredicate(format: "label == %@ OR placeholderValue == %@", "User", "Your user name on the machine")
        ).firstMatch
        XCTAssertTrue(user.waitForExistence(timeout: 5), "The form has no User field")
        user.tap()
        user.typeText("sam")
        app.buttons["Connect"].tap()

        let save = app.buttons["Save"]
        XCTAssertTrue(save.waitForExistence(timeout: 5), "Connecting never offers Save")
        audit(app, glass: ["Cancel"])
        save.tap()

        // The form names the machine from its Bonjour name without the owner.
        XCTAssertTrue(app.staticTexts["MacBook Pro"].waitForExistence(timeout: 5), "The new machine has no section")
        XCTAssertTrue(app.staticTexts["No sessions yet"].exists, "The new machine shows no empty state")
    }

    func testSettingsPassesTheAudit() {
        let app = launchTogo()
        let settings = app.buttons["Settings"]
        XCTAssertTrue(settings.waitForExistence(timeout: 10))
        settings.tap()
        XCTAssertTrue(app.navigationBars["Settings"].waitForExistence(timeout: 5))
        audit(app)
    }
}
