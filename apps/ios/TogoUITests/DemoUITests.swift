import XCTest

/// The path App Review takes: the first run's demo machine, its "status" session from prompt to
/// answered requests, the seven changed files and a diff, the terminal, and back out to the
/// first run. Offline, on the demo's own staged session.
@MainActor final class DemoUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    private func wait(_ element: XCUIElement, until format: String, timeout: TimeInterval, _ why: String) {
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate(format: format), object: element)
        XCTAssertEqual(XCTWaiter().wait(for: [expectation], timeout: timeout), .completed, why)
    }

    /// Taps the on-screen, enabled button named `label` once one shows up.
    private func tapVisible(_ label: String, in app: XCUIApplication, timeout: TimeInterval = 10) {
        let end = Date().addingTimeInterval(timeout)
        while Date() < end {
            if let button = app.visibleButton(label), button.isEnabled {
                button.tap()
                return
            }
            RunLoop.current.run(until: Date().addingTimeInterval(0.3))
        }
        XCTFail("No enabled \(label) on screen")
    }

    func testAppReviewDemoPath() {
        let app = launchTogo(firstRun: true)
        let explore = app.buttons["Explore with a demo machine"]
        XCTAssertTrue(explore.waitForExistence(timeout: 10), "The first run offers no demo machine")
        explore.tap()

        let done = app.buttons["demo.done"]
        XCTAssertTrue(done.waitForExistence(timeout: 10), "The demo roster has no Done")
        let row = app.row("01", "status")
        XCTAssertTrue(row.waitForExistence(timeout: 10), "The demo machine has no status session")
        row.tap()

        // The prompt is already in the composer: Send is the reviewer's first tap.
        let field = app.descendants(matching: .any)["composer.field"]
        XCTAssertTrue(field.waitForExistence(timeout: 10), "The session opens with no composer")
        wait(field, until: "value BEGINSWITH 'Add an incident history timeline'", timeout: 10, "The prompt is not in the composer")
        tapVisible("Send", in: app)

        // The agent asks three times to run a command; each card is answered with Allow once.
        for request in 1...3 {
            let card = app.descendants(matching: .any)["chat.needs.question"]
            XCTAssertTrue(card.waitForExistence(timeout: request == 1 ? 45 : 20), "Request \(request) never showed")
            let jump = app.buttons["chat.needsJump"]
            if jump.exists, !card.buttons["Allow once"].isHittable { jump.tap() }
            app.waitForAnswersToArm()
            card.buttons["Allow once"].tap()
            XCTAssertTrue(card.waitForNonExistence(timeout: 10), "Request \(request) stayed open")
        }

        // Changes counts the turn's seven files once it is done, and lists each.
        let changes = app.buttons["workspace.changes"]
        XCTAssertTrue(changes.waitForExistence(timeout: 10), "The bar has no Changes")
        wait(changes, until: "value BEGINSWITH '7 '", timeout: 30, "Changes does not count 7 files")
        changes.tap()
        XCTAssertTrue(app.buttons["Close"].waitForExistence(timeout: 5), "Changes did not open")
        let files = [
            ("incidents.json", "data"), ("style.css", "public"), ("app.ts", "src"), ("incidents.ts", "src"),
            ("render.ts", "src"), ("app.test.ts", "test"), ("incidents.test.ts", "test"),
        ]
        for (name, folder) in files {
            let file = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "\(name), in \(folder), ")).firstMatch
            if !file.waitForExistence(timeout: 3) { app.swipeUp() }
            XCTAssertTrue(file.waitForExistence(timeout: 3), "\(folder)/\(name) is not listed")
        }

        // A diff, then back to the list and out of the review.
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "render.ts, in src, ")).firstMatch.tap()
        XCTAssertTrue(app.buttons["View options"].waitForExistence(timeout: 5), "The diff did not open")
        app.navigationBars.buttons["BackButton"].firstMatch.tap()
        tapVisible("Close", in: app, timeout: 5)

        // The same session as a terminal.
        tapVisible("Terminal", in: app, timeout: 5)
        XCTAssertTrue(app.buttons["Chat"].waitForExistence(timeout: 5), "The session did not switch to its terminal")

        // Back to the demo's sessions, then Done ends the demo on the first run.
        tapVisible("Sessions", in: app, timeout: 5)
        XCTAssertTrue(done.waitForExistence(timeout: 5))
        done.tap()
        XCTAssertTrue(app.buttons["Add a machine"].waitForExistence(timeout: 5), "Done did not return to the first run")
        XCTAssertTrue(explore.exists)
    }
}
