import XCTest

/// Smoke flows over a replayed helper (`-ui-replay`, recordings in the app's Debug resources)
/// and the presentation coordinator's probe (`presentation.probe`: violations, how many modals
/// are stacked, what shows and what waits). Every check is on elements: no screenshots.
/// `scripts/check-ui.sh` runs them on one iPhone simulator, portrait for compact width and
/// landscape for regular.
@MainActor final class PresentationFlowsUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        XCUIDevice.shared.orientation = .portrait
    }

    override func tearDown() async throws {
        XCUIDevice.shared.orientation = .portrait
    }

    // MARK: Helpers

    @discardableResult
    private func launch(replay: String? = nil, _ extra: [String] = []) -> XCUIApplication {
        let app = XCUIApplication()
        // Loops held: the app goes idle between steps, so XCUITest does not wait out each spinner.
        app.launchArguments = ["-ui-fakes", "-ui-skip-launch", "-ui-probes", "-ui-still-loops"]
            + (replay.map { ["-ui-one-machine", "-ui-replay", "replay-\($0)"] } ?? []) + extra
        app.launch()
        self.app = app
        return app
    }

    private var probe: XCUIElement { app.descendants(matching: .any).matching(identifier: "presentation.probe").firstMatch }

    private var probeValue: String { probe.exists ? (probe.value as? String ?? "") : "" }

    private func waitFor(_ what: String, timeout: TimeInterval = 10, _ condition: () -> Bool) -> Bool {
        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            if condition() { return true }
            RunLoop.current.run(until: Date().addingTimeInterval(0.15))
        }
        return condition()
    }

    private func expectProbe(contains text: String, _ what: String, timeout: TimeInterval = 10,
                             file: StaticString = #filePath, line: UInt = #line) {
        let ok = waitFor(what, timeout: timeout) { probeValue.contains(text) }
        XCTAssertTrue(ok, "\(what): probe was “\(probeValue)”", file: file, line: line)
    }

    private func expectProbe(lacks text: String, _ what: String, timeout: TimeInterval = 10,
                             file: StaticString = #filePath, line: UInt = #line) {
        let ok = waitFor(what, timeout: timeout) { probe.exists && !probeValue.contains(text) }
        XCTAssertTrue(ok, "\(what): probe was “\(probeValue)”", file: file, line: line)
    }

    /// No presentation rule was broken, and at most one modal is up.
    private func expectClean(file: StaticString = #filePath, line: UInt = #line) {
        XCTAssertTrue(waitFor("probe", timeout: 5) { probe.exists }, "No presentation probe", file: file, line: line)
        let value = probeValue
        XCTAssertTrue(value.hasPrefix("violations=0 "), "Presentation rules broken: \(value)", file: file, line: line)
        XCTAssertFalse(value.contains("depth=2") || value.contains("depth=3"), "Sheets stacked: \(value)", file: file, line: line)
    }

    private var composer: XCUIElement { app.descendants(matching: .any).matching(identifier: "composer.field").firstMatch }

    private var terminal: XCUIElement { app.textViews["Terminal"] }

    private func element(_ id: String) -> XCUIElement { app.descendants(matching: .any).matching(identifier: id).firstMatch }

    private func text(containing value: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", value)).firstMatch
    }

    /// The on-screen button with this label (other windows and sheets hold hidden twins).
    private func visibleButton(_ label: String, timeout: TimeInterval = 10) -> XCUIElement? {
        var found: XCUIElement?
        _ = waitFor(label, timeout: timeout) {
            found = app.buttons.matching(identifier: label).allElementsBoundByIndex.last { $0.exists && $0.isHittable }
            return found != nil
        }
        return found
    }

    /// Opens mission control: its bar item where the bar has one (regular width, a Duo's
    /// vertical bar), else its item in the session menu, where a compact bar keeps it.
    private func openMissionControl() -> Bool {
        if let item = visibleButton("mission.item", timeout: 3) {
            item.tap()
            return true
        }
        // The menu lists it once the session's mission has loaded: look again until it does.
        let deadline = Date().addingTimeInterval(15)
        while Date() < deadline {
            guard let menu = visibleButton("Session actions", timeout: 5) else { return false }
            menu.tap()
            // By its label: a menu item does not carry its view's identifier. The open menu's
            // item is the one on top; the screen under it may name mission control too.
            if let item = visibleButton("Mission control", timeout: 2) {
                item.tap()
                return true
            }
            // Closes the menu without picking anything.
            app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.95)).tap()
        }
        return false
    }

    /// The on-screen button whose label starts with `prefix` ("Sonnet, Fast and capable").
    private func visibleButton(beginningWith prefix: String, timeout: TimeInterval = 10) -> XCUIElement? {
        var found: XCUIElement?
        _ = waitFor(prefix, timeout: timeout) {
            found = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", prefix)).allElementsBoundByIndex
                .last { $0.exists && $0.isHittable }
            return found != nil
        }
        return found
    }

    /// Opens session 01 and waits for its chat, checking the terminal never shows on the way.
    private func openChat(file: StaticString = #filePath, line: UInt = #line) {
        let row = app.row("01", "api")
        XCTAssertTrue(row.waitForExistence(timeout: 15), "The session list never showed 01", file: file, line: line)
        row.tap()
        var flashed = false
        let opened = waitFor("chat", timeout: 15) {
            if terminal.exists, terminal.isHittable { flashed = true }
            return composer.exists && composer.isHittable
        }
        XCTAssertTrue(opened, "The chat never showed", file: file, line: line)
        XCTAssertFalse(flashed, "The terminal showed before the chat (owner finding 1)", file: file, line: line)
    }

    private func type(_ value: String, file: StaticString = #filePath, line: UInt = #line) {
        let field = composer
        field.tap()
        let focused = waitFor("focus", timeout: 3) { (field.value(forKey: "hasKeyboardFocus") as? Bool) ?? false }
        XCTAssertTrue(focused, "One tap did not focus the composer", file: file, line: line)
        field.typeText(value)
    }

    // MARK: Flows

    /// 1. Launch shows the session list, with nothing presented.
    func testLaunchShowsSessionList() {
        launch(replay: "chat")
        XCTAssertTrue(app.row("01", "api").waitForExistence(timeout: 15))
        XCTAssertTrue(app.buttons["roster.settings"].exists)
        expectClean()
        XCTAssertTrue(probeValue.contains("depth=0"), probeValue)
    }

    /// 2. Opening a session shows the chat; the terminal is never hittable on the way.
    func testOpenSessionShowsChatWithoutTerminalFlash() {
        launch(replay: "chat")
        openChat()
        XCTAssertFalse(terminal.exists && terminal.isHittable)
        XCTAssertTrue(text(containing: "Earlier answer 6").waitForExistence(timeout: 5), "The history loaded")
        expectClean()
    }

    /// 3. Sending shows the working line, and the turn's end clears it.
    func testSendShowsWorkingThenClears() {
        launch(replay: "chat")
        openChat()
        type("hello from the phone")
        guard let send = visibleButton("Send") else { return XCTFail("No Send button") }
        send.tap()
        XCTAssertTrue(element("chat.working").waitForExistence(timeout: 5), "The working line never showed")
        XCTAssertTrue(text(containing: "Echo: hello from the phone").waitForExistence(timeout: 10), "No reply")
        XCTAssertTrue(element("chat.working").waitForNonExistence(timeout: 10), "The working line stayed after the turn ended")
        expectClean()
    }

    /// 4. An approval arrives in view and hittable above the composer; approving takes it away.
    func testApprovalCardIsInViewAndApproving() {
        launch(replay: "approval")
        openChat()
        let card = element("chat.needs.approval")
        XCTAssertTrue(card.waitForExistence(timeout: 10), "No approval card")
        XCTAssertTrue(waitFor("hittable", timeout: 5) { card.isHittable }, "The card was not scrolled into view (owner finding 4)")
        let window = app.windows.firstMatch.frame
        XCTAssertTrue(window.contains(card.frame), "The card is cut off: \(card.frame) in \(window)")
        if composer.exists {
            XCTAssertLessThanOrEqual(card.frame.maxY, composer.frame.minY + 1, "The composer covers the card (owner finding 10)")
        }
        // Approve is the card's first answer, at its lower leading corner; the card reads as one
        // element to VoiceOver, so its buttons are reached by place.
        card.coordinate(withNormalizedOffset: CGVector(dx: 0, dy: 1))
            .withOffset(CGVector(dx: 56, dy: -30)).tap()
        XCTAssertTrue(card.waitForNonExistence(timeout: 10), "The card stayed after approving")
        XCTAssertTrue(text(containing: "All 42 tests pass").waitForExistence(timeout: 10), "The turn did not go on")
        XCTAssertFalse(element("chat.needs.failure").exists)
        expectClean()
    }

    /// 5. Two questions at once: one answer each, sent together.
    func testMultiQuestionAnswer() {
        launch(replay: "question")
        openChat()
        let card = element("chat.needs.question")
        XCTAssertTrue(card.waitForExistence(timeout: 10), "No question card")
        XCTAssertTrue(waitFor("hittable", timeout: 5) { card.isHittable }, "The question card is not in view")
        for option in ["Beta", "Skip"] {
            let button = card.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", option)).firstMatch
            XCTAssertTrue(button.waitForExistence(timeout: 5), "No \(option) option")
            button.tap()
        }
        let submit = card.buttons.matching(NSPredicate(format: "label IN %@", ["Send", "Answer", "Submit", "Send answers"])).firstMatch
        XCTAssertTrue(submit.waitForExistence(timeout: 5), "No way to send the answers: \(card.debugDescription)")
        XCTAssertTrue(waitFor("enabled", timeout: 3) { submit.isEnabled }, "Send stayed disabled with every question answered")
        submit.tap()
        XCTAssertTrue(text(containing: "Releasing to beta").waitForExistence(timeout: 10), "The answers did not reach the agent")
        XCTAssertFalse(element("chat.needs.failure").exists, "The answer failed (owner finding 10)")
        expectClean()
    }

    /// 6. The model menu: choosing Sonnet updates the state chip.
    func testModelPickerUpdatesChip() {
        launch(replay: "chat")
        openChat()
        let chip = element("composer.state")
        XCTAssertTrue(waitFor("chip", timeout: 10) { chip.exists && (chip.value as? String ?? "").contains("Opus") },
                      "The chip never showed the model: \(chip.value as? String ?? "nil")")
        chip.tap()
        // A short list is a submenu of the chip's menu; a long one opens the picker as a sheet.
        guard let model = visibleButton(beginningWith: "Model", timeout: 5) else { return XCTFail("No Model in the chip's menu") }
        model.tap()
        guard let sonnet = visibleButton(beginningWith: "Sonnet", timeout: 10) else {
            return XCTFail("No Sonnet in the model picker; probe “\(probeValue)”")
        }
        if probeValue.contains("composer.screen") {
            XCTAssertTrue(probeValue.contains("depth=1"), "The picker stacked on another sheet: \(probeValue)")
        }
        sonnet.tap()
        XCTAssertTrue(waitFor("chip", timeout: 10) { (chip.value as? String ?? "").contains("Sonnet") },
                      "The chip still says \(chip.value as? String ?? "nil")")
        expectProbe(lacks: "composer.screen", "The picker closed after choosing")
        expectClean()
    }

    /// 7. A machine that needs a password: one sign-in sheet, not stacked, not popped away; the
    /// outcome closes it.
    func testSignInSheetShowsOnceAndCloses() {
        launch(replay: nil, ["-ui-auth-needed"])
        guard let signIn = visibleButton("Sign in again", timeout: 15) else {
            return XCTFail("No sign-in action for the signed-out machine")
        }
        signIn.tap()
        expectProbe(contains: "shown=root.form", "The sign-in sheet")
        XCTAssertTrue(probeValue.contains("depth=1"), probeValue)
        // Nothing takes it away while it waits for the password (the login that popped and left).
        RunLoop.current.run(until: Date().addingTimeInterval(1.5))
        XCTAssertTrue(probeValue.contains("shown=root.form"), "The sign-in sheet went away by itself: \(probeValue)")
        let field = app.secureTextFields.firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 10), "No password field")
        field.tap()
        XCTAssertTrue(waitFor("focus", timeout: 3) { (field.value(forKey: "hasKeyboardFocus") as? Bool) ?? false },
                      "One tap did not focus the password field")
        field.typeText("hunter2\n")
        expectProbe(lacks: "root.form", "The sign-in sheet closed on success")
        expectClean()
    }

    /// 8. Mission control opens as a sheet on a compact width and closes with Done.
    func testMissionControlOpensAndCloses() {
        launch(replay: "chat")
        openChat()
        guard openMissionControl() else { return XCTFail("No mission control item") }
        expectProbe(contains: "mission.sheet", "Mission control as a sheet")
        XCTAssertTrue(app.navigationBars["Mission control"].waitForExistence(timeout: 5))
        guard let done = visibleButton("Done", timeout: 5) else { return XCTFail("No Done in mission control") }
        done.tap()
        expectProbe(lacks: "mission.sheet", "Mission control closed")
        XCTAssertTrue(composer.waitForExistence(timeout: 5))
        expectClean()
    }

    /// 9. Terminal and back to the chat.
    func testSwitchToTerminalAndBack() {
        launch(replay: "chat")
        openChat()
        guard let toTerminal = visibleButton("Terminal") else { return XCTFail("No Terminal switch") }
        toTerminal.tap()
        XCTAssertTrue(terminal.waitForExistence(timeout: 10), "The terminal layer never showed")
        guard let toChat = visibleButton("Chat") else { return XCTFail("No Chat switch") }
        toChat.tap()
        XCTAssertTrue(waitFor("chat", timeout: 10) { composer.exists && composer.isHittable }, "The chat did not come back")
        expectClean()
    }

    /// 9b. @ opens the file menu with the field still in view and a close control; closing it
    /// returns to the composer as it was, and deleting the "@" closes it too (owner finding:
    /// the menu once filled the screen with no way out).
    func testMentionOpensAndDismissReturnsToComposer() {
        launch(replay: "chat")
        openChat()
        guard let mention = visibleButton("Mention a file") else { return XCTFail("No Mention button") }
        mention.tap()
        let menu = element("composer.menu.files")
        XCTAssertTrue(menu.waitForExistence(timeout: 10), "The file menu never opened")
        let close = element("composer.menu.close")
        XCTAssertTrue(waitFor("close", timeout: 5) { close.exists && close.isHittable }, "No visible close control")
        XCTAssertTrue(composer.isHittable, "The field is hidden under the menu or the keyboard")
        let window = app.windows.firstMatch.frame
        XCTAssertTrue(window.contains(menu.frame), "The menu runs off screen: \(menu.frame) in \(window)")
        XCTAssertLessThanOrEqual(menu.frame.maxY, composer.frame.minY, "The menu covers the field")
        close.tap()
        XCTAssertTrue(menu.waitForNonExistence(timeout: 5), "Close left the menu up")
        XCTAssertFalse((composer.value as? String ?? "").contains("@"), "Close did not take back the bare @")

        // Typed "@" then deleted: the menu goes with it.
        type("@")
        XCTAssertTrue(menu.waitForExistence(timeout: 10), "Typing @ did not open the menu")
        composer.typeText(XCUIKeyboardKey.delete.rawValue)
        XCTAssertTrue(menu.waitForNonExistence(timeout: 5), "Deleting the @ left the menu up")
        XCTAssertTrue(composer.isHittable)
        expectClean()
    }

    /// 10. The same request, two widths: a sheet on a compact width, an inspector beside the
    /// chat on a regular one (the policy keys on width, never the device).
    func testRegularWidthPicksInspector() throws {
        launch(replay: "chat")
        XCUIDevice.shared.orientation = .landscapeLeft
        openChat()
        // An iPhone below the Max models keeps a compact-width window in landscape (its size
        // class), where mission control is rightly a sheet. The regular-width inspector is
        // checked on a Max or an iPad; check-ui.sh runs on the iPhone 18 Pro Max.
        let device = ProcessInfo.processInfo.environment["SIMULATOR_DEVICE_NAME"] ?? ""
        if !device.contains("Max"), !device.contains("iPad") {
            throw XCTSkip("Landscape on \(device) is compact width; the inspector is for regular width")
        }
        // It may already be open by default beside the chat; otherwise open it.
        if !probeValue.contains("mission.inspector") {
            guard openMissionControl() else { return XCTFail("No mission control item") }
        }
        expectProbe(contains: "mission.inspector", "Mission control as an inspector at regular width")
        XCTAssertFalse(probeValue.contains("mission.sheet"), probeValue)
        XCTAssertTrue(composer.isHittable, "The chat stays usable beside the inspector")
        expectClean()
        XCUIDevice.shared.orientation = .portrait
        // Back to compact: the inspector does not turn into a sheet nobody asked for.
        expectProbe(lacks: "mission.inspector", "The inspector left at compact width")
        XCTAssertFalse(probeValue.contains("mission.sheet"), probeValue)
        expectClean()
    }
}
