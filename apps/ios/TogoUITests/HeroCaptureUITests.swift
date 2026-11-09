import XCTest

/// The website's hero recording: run by scripts/capture-hero.sh, which records the screen with
/// `simctl io recordVideo` and passes a folder for the handshake as `TEST_RUNNER_TOGO_HERO_DIR`;
/// without it the test skips, so ordinary UI test runs leave it alone.
///
/// The replayed Claude Code session (`replay-demo`) answers its first turn before the recording
/// starts. On camera: the second prompt is typed and sent, the Home Screen comes up while the
/// agent works, the Dynamic Island asks to run `npm test`, Approve is tapped there, and the
/// finished notification arrives.
@MainActor final class HeroCaptureUITests: XCTestCase {
    private let app = XCUIApplication()
    private var springboard: XCUIApplication { XCUIApplication(bundleIdentifier: "com.apple.springboard") }

    static let first = "What does the /login route do right now?"
    static let last = " Then run npm test."
    static let second = "Add rate limiting to the /login route (5 attempts per minute per IP) and cover it with tests. No new dependencies. Then run npm test."

    override func setUp() {
        continueAfterFailure = false
    }

    private var dir: URL? {
        ProcessInfo.processInfo.environment["TOGO_HERO_DIR"].flatMap { $0.isEmpty ? nil : URL(fileURLWithPath: $0) }
    }

    private func mark(_ name: String) {
        guard let dir else { return }
        FileManager.default.createFile(atPath: dir.appendingPathComponent(name).path, contents: Data())
    }

    private func waitForFile(_ name: String, timeout: TimeInterval) -> Bool {
        guard let dir else { return false }
        let end = Date().addingTimeInterval(timeout)
        while Date() < end {
            if FileManager.default.fileExists(atPath: dir.appendingPathComponent(name).path) { return true }
            Thread.sleep(forTimeInterval: 0.1)
        }
        return false
    }

    private func hold(_ seconds: TimeInterval) {
        _ = XCTWaiter.wait(for: [XCTestExpectation(description: "hold")], timeout: seconds)
    }

    private func waitFor(_ timeout: TimeInterval, _ ok: () -> Bool) -> Bool {
        let end = Date().addingTimeInterval(timeout)
        while Date() < end {
            if ok() { return true }
            Thread.sleep(forTimeInterval: 0.1)
        }
        return false
    }

    /// Waits for an element with one query a second at most. Every query snapshots the app or the
    /// Home Screen, and a snapshot during an animation costs the recording frames, so on camera
    /// this stands in for `waitForExistence`, which queries without pause.
    private func sparse(_ element: XCUIElement, timeout: TimeInterval, every: TimeInterval = 1.0) -> Bool {
        let end = Date().addingTimeInterval(timeout)
        while Date() < end {
            if element.exists { return true }
            hold(every)
        }
        return false
    }

    private var composer: XCUIElement { app.descendants(matching: .any).matching(identifier: "composer.field").firstMatch }
    private var send: XCUIElement { app.buttons["Send"] }

    private func springboardText(_ text: String) -> XCUIElement {
        springboard.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
    }

    /// A drag down the transcript, the chat's own way of putting the keyboard away. The drag
    /// also scrolls the transcript up, as a finger would, so a swipe up brings it back to its
    /// foot: the latest answer shows above the composer when the recording starts.
    private func lowerKeyboard() {
        guard app.keyboards.count > 0 else { return }
        let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.22))
        from.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.7)))
        hold(0.8)
        let up = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
        up.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.12)))
    }

    /// Takes the test runner's icon off the Home Screen (it stays in the App Library), so the
    /// recording shows the Home Screen a phone has.
    private func hideRunnerIcon() {
        springboard.activate()
        let icon = springboard.icons.matching(NSPredicate(format: "label BEGINSWITH %@", "TogoUITests")).firstMatch
        guard icon.waitForExistence(timeout: 5) else { return }
        icon.press(forDuration: 1.2)
        let remove = springboard.buttons["Remove App"]
        guard remove.waitForExistence(timeout: 3) else { return }
        remove.tap()
        let fromHome = springboard.buttons["Remove from Home Screen"]
        if fromHome.waitForExistence(timeout: 3) { fromHome.tap() }
        hold(1)
    }

    /// Types the prompt at a reading pace and sends it.
    private func say(_ text: String, slowly: Bool) {
        composer.tap()
        XCTAssertTrue(waitFor(3) { (composer.value(forKey: "hasKeyboardFocus") as? Bool) ?? false }, "No focus")
        composer.typeText(text)
        if slowly { hold(0.6) }
        XCTAssertTrue(send.waitForExistence(timeout: 3), "No Send")
        send.tap()
    }

    func testHero() throws {
        guard dir != nil else { throw XCTSkip("Runs only from scripts/capture-hero.sh") }
        hideRunnerIcon()
        app.launchArguments = ["-ui-fakes", "-ui-skip-launch", "-ui-one-machine", "-ui-replay", "replay-demo", "-ui-replay-pace", "0.15", "-ui-demo-live"]
        app.launch()
        let allow = springboard.alerts.buttons["Allow"]
        if allow.waitForExistence(timeout: 8) { allow.tap() }

        let row = app.descendants(matching: .any).matching(NSPredicate(
            format: "label BEGINSWITH %@ AND (elementType == %d OR elementType == %d)",
            "01, api, ", XCUIElement.ElementType.button.rawValue, XCUIElement.ElementType.cell.rawValue
        )).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 20), "No session 01")
        row.tap()
        XCTAssertTrue(composer.waitForExistence(timeout: 15), "No chat")

        // Off camera: the first turn, played through to its answer.
        say(Self.first, slowly: false)
        hold(1)
        XCTAssertTrue(waitFor(60) { send.exists && send.isHittable && ((composer.value as? String) ?? "").isEmpty }, "The first turn never ended")
        hold(3)
        // The keyboard down, so the chat fills the screen when the recording starts.
        // The second prompt is drafted off camera up to its last sentence, which is typed on camera.
        composer.tap()
        composer.typeText(String(Self.second.dropLast(Self.last.count)))
        hold(0.5)
        lowerKeyboard()
        hold(1.2)
        mark("ready")
        XCTAssertTrue(waitForFile("go", timeout: 30), "The recording never started")

        // On camera. No query runs while something moves: each one snapshots the screen and the
        // simulator drops frames for it, so the holds below cover each animation first.
        hold(1.0)
        // The caret at the end of the draft: a tap at the end of its last line; the keyboard rises.
        composer.coordinate(withNormalizedOffset: CGVector(dx: 0.97, dy: 0.85)).tap()
        hold(1.0)
        composer.typeText(Self.last)
        hold(0.4)
        if composer.value as? String != Self.second { mark("draft-mismatch") }
        send.tap()
        hold(1.8)
        XCUIDevice.shared.press(.home)
        hold(2.0)

        // The island asks; if the alert did not open it, a long press does.
        let approve = springboard.buttons["Approve"]
        let asked = springboardText("Needs you")
        _ = sparse(asked, timeout: 30)
        hold(1.0)
        if !approve.exists {
            springboard.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.025)).press(forDuration: 0.9)
            XCTAssertTrue(approve.waitForExistence(timeout: 3), "The island never offered Approve")
            hold(1.0)
        }
        hold(0.6)
        approve.tap()
        hold(2.0)

        // Working again, then the turn's end.
        let finished = springboardText("5 attempts")
        _ = sparse(finished, timeout: 40, every: 1.5)
        hold(2.6)
        mark("done")
        hold(1)
    }
}
