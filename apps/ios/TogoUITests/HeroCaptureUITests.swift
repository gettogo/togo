import XCTest

/// The website's hero recording: run by scripts/capture-hero.sh, which records the screen with
/// `simctl io recordVideo` and passes a folder for the handshake as `TEST_RUNNER_TOGO_HERO_DIR`;
/// without it the test skips, so ordinary UI test runs leave it alone.
///
/// The recorded OpenCode session on the status page (`replay-status`, the website's hero story,
/// with `-ui-demo-api` naming session 01 "status"). The prompt is drafted off camera up to its
/// last sentence. On camera: that sentence is typed and sent, the chat streams, the first two
/// shell requests are allowed once in the chat, the Home Screen comes up, the Dynamic Island asks to
/// run `npm test`, Approve is tapped there, and the finished notification arrives.
///
/// `testHeroStills` plays the same session and saves the app's own frames the hero rests on
/// (the list, the chat streaming, the npm test request in the chat, the finished chat, the terminal,
/// Changes and the render.ts diff). `testHeroStillsLock` saves the Lock Screen's: the phone locks
/// while OpenCode works, the Live Activity asks to run npm test, Approve is tapped there, and the
/// turn ends; it never returns to the app, whose resume lock would ask for a passcode.
/// into the handshake folder, for the website's fidelity tests.
@MainActor final class HeroCaptureUITests: XCTestCase {
    private let app = XCUIApplication()
    private var springboard: XCUIApplication { XCUIApplication(bundleIdentifier: "com.apple.springboard") }

    static let last = " Add tests, then run npm test."
    static let prompt = "Add an incident history timeline under the services: each incident has a time, service, severity (minor/major) and a short note, coloured by severity, newest first. Read incidents from data/incidents.json with a few examples. Add tests, then run npm test."
    static let launchArguments = ["-ui-fakes", "-ui-skip-launch", "-ui-one-machine", "-ui-demo-api", "-ui-replay", "replay-status"]

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

    /// Session 01's line in the list.
    private var sessionRow: XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(
            format: "label BEGINSWITH %@ AND (elementType == %d OR elementType == %d)",
            "01, status, ", XCUIElement.ElementType.button.rawValue, XCUIElement.ElementType.cell.rawValue
        )).firstMatch
    }

    /// OpenCode's permission request: a question card in the chat, answered "Allow once".
    private var request: XCUIElement { app.descendants(matching: .any).matching(identifier: "chat.needs.question").firstMatch }

    /// Allow once on the request card in the chat, brought into view first if it is off screen.
    private func allowInChat() {
        let button = request.buttons["Allow once"].firstMatch
        XCTAssertTrue(button.waitForExistence(timeout: 5), "No Allow once on the request")
        let jump = app.buttons["chat.needsJump"]
        if jump.exists, !button.isHittable { jump.tap(); hold(0.8) }
        button.tap()
    }

    /// The whole screen, saved into the handshake folder as `<name>.png`.
    private func shoot(_ name: String, settle: TimeInterval = 2.5) {
        guard let dir else { return }
        hold(settle)
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: dir.appendingPathComponent("\(name).png"))
    }

    func testHero() throws {
        guard dir != nil else { throw XCTSkip("Runs only from scripts/capture-hero.sh") }
        hideRunnerIcon()
        app.launchArguments = Self.launchArguments + ["-ui-replay-pace", "0.15", "-ui-demo-live"]
        app.launch()
        let allow = springboard.alerts.buttons["Allow"]
        if allow.waitForExistence(timeout: 8) { allow.tap() }

        XCTAssertTrue(sessionRow.waitForExistence(timeout: 20), "No session 01")
        sessionRow.tap()
        XCTAssertTrue(composer.waitForExistence(timeout: 15), "No chat")

        // Off camera: the prompt drafted up to its last sentence, the keyboard down so the chat
        // fills the screen when the recording starts.
        composer.tap()
        composer.typeText(String(Self.prompt.dropLast(Self.last.count)))
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
        if composer.value as? String != Self.prompt { mark("draft-mismatch") }
        send.tap()

        // The first two shell requests, answered in the chat.
        for _ in 0..<2 {
            XCTAssertTrue(sparse(request, timeout: 90), "No request in the chat")
            hold(1.2)
            allowInChat()
            hold(1.5)
        }
        XCUIDevice.shared.press(.home)
        hold(2.0)

        // The island asks to run npm test; if the alert did not open it, a long press does.
        let approve = springboard.buttons.matching(NSPredicate(format: "label IN %@", ["Approve", "Allow once"])).firstMatch
        let asked = springboardText("Needs you")
        _ = sparse(asked, timeout: 60)
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
        let finished = springboardText("Done")
        _ = sparse(finished, timeout: 60, every: 1.5)
        hold(2.6)
        mark("done")
        hold(1)
    }

    /// The app's frames the website's hero rests on, as stills (no recording, loops held still).
    func testHeroStills() throws {
        guard dir != nil else { throw XCTSkip("Runs only from scripts/capture-hero.sh") }
        app.launchArguments = Self.launchArguments + ["-ui-replay-pace", "0.15", "-ui-no-keyboard"]
        app.launch()
        let allow = springboard.alerts.buttons["Allow"]
        if allow.waitForExistence(timeout: 8) { allow.tap() }

        XCTAssertTrue(sessionRow.waitForExistence(timeout: 20), "No session 01")
        shoot("hero-roster")
        sessionRow.tap()
        XCTAssertTrue(composer.waitForExistence(timeout: 15), "No chat")

        composer.tap()
        composer.typeText(Self.prompt)
        send.tap()
        hold(4)
        if app.keyboards.count > 0 { lowerKeyboard() }
        shoot("hero-chat", settle: 1.5)

        // Three shell requests, each allowed once in the chat; the last asks to run npm test.
        for n in 1...3 {
            XCTAssertTrue(request.waitForExistence(timeout: 120), "No request \(n)")
            if n == 3 { shoot("hero-needs-you-app") }
            hold(1)
            allowInChat()
            hold(2)
        }

        // The turn's end: Send back, the composer empty, nothing waiting.
        XCTAssertTrue(waitFor(180) { send.exists && send.isHittable && !request.buttons["Allow once"].exists }, "The turn never ended")
        hold(1)
        if app.keyboards.count > 0 { lowerKeyboard() }
        hold(3)
        shoot("hero-finished")

        // The session's terminal, once the turn is done (leaving the chat earlier would close the
        // replay's feed): the recorded OpenCode screen.
        let terminal = app.buttons["Terminal"].firstMatch
        if terminal.waitForExistence(timeout: 3) {
            terminal.tap()
            shoot("hero-terminal")
            let chat = app.buttons["Chat"].firstMatch
            if chat.waitForExistence(timeout: 3) { chat.tap() }
            hold(1)
        }

        let changes = app.descendants(matching: .any).matching(identifier: "workspace.changes").firstMatch
        XCTAssertTrue(changes.waitForExistence(timeout: 10), "No Changes in the bar")
        changes.tap()
        let render = app.descendants(matching: .any).matching(NSPredicate(
            format: "label BEGINSWITH %@ AND (elementType == %d OR elementType == %d)",
            "render.ts, ", XCUIElement.ElementType.button.rawValue, XCUIElement.ElementType.cell.rawValue
        )).firstMatch
        XCTAssertTrue(render.waitForExistence(timeout: 15), "No render.ts in Changes")
        shoot("hero-changes")
        render.tap()
        XCTAssertTrue(app.buttons["Next change"].waitForExistence(timeout: 10), "No diff")
        shoot("hero-diff")
        mark("done")
    }

    func testHeroStillsLock() throws {
        guard dir != nil else { throw XCTSkip("Runs only from scripts/capture-hero.sh") }
        app.launchArguments = Self.launchArguments + ["-ui-replay-pace", "0.15", "-ui-no-keyboard", "-ui-demo-live"]
        app.launch()
        let allow = springboard.alerts.buttons["Allow"]
        if allow.waitForExistence(timeout: 8) { allow.tap() }

        XCTAssertTrue(sessionRow.waitForExistence(timeout: 20), "No session 01")
        sessionRow.tap()
        XCTAssertTrue(composer.waitForExistence(timeout: 15), "No chat")
        composer.tap()
        composer.typeText(Self.prompt)
        send.tap()
        for n in 1...2 {
            XCTAssertTrue(request.waitForExistence(timeout: 120), "No request \(n)")
            hold(1)
            allowInChat()
            hold(n == 2 ? 0.2 : 1.5)
        }

        // The phone locks while the agent works; the Lock Screen wakes with the Live Activity.
        let device = XCUIDevice.shared
        let lock = NSSelectorFromString("pressLockButton")
        XCTAssertTrue(device.responds(to: lock), "No lock button on this device")
        device.perform(lock)
        hold(1.2)
        // A second press wakes the Lock Screen; Home would unlock (the simulator has no passcode).
        device.perform(lock)
        // A fresh simulator asks once whether Togo may show Live Activities; it may.
        let allowLive = springboard.buttons["Allow"].firstMatch
        if allowLive.waitForExistence(timeout: 4) { allowLive.tap() }
        shoot("hero-lock-working", settle: 1.0)

        let approve = springboard.buttons["Approve"].firstMatch
        XCTAssertTrue(sparse(approve, timeout: 60), "The Lock Screen never offered Approve")
        shoot("hero-lock-needs-you", settle: 1.5)
        approve.tap()
        shoot("hero-lock-approved", settle: 1.2)
        let finished = springboardText("Done")
        _ = sparse(finished, timeout: 60, every: 1.5)
        shoot("hero-lock-done", settle: 2.0)
        mark("done")
        device.press(.home)
    }
}
