import XCTest

/// End-to-end flows against a real helper on the Mac running the simulator, over SSH to
/// 127.0.0.1. They need xcodebuild to pass the machine through `TEST_RUNNER_` variables:
/// `TOGO_E2E_KEY` (base64 OpenSSH private key), `TOGO_E2E_HOSTKEYS`, `TOGO_E2E_USER`, and
/// `TOGO_E2E_DIR` (a folder on the Mac to start sessions in). Without them every test skips.
@MainActor final class NativeFlowsUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        let env = ProcessInfo.processInfo.environment
        guard env["TOGO_E2E_KEY"] != nil else { throw XCTSkip("No E2E machine configured") }
        app = XCUIApplication()
        app.launchArguments = ["-e2e-host"]
        for key in ["TOGO_E2E_KEY", "TOGO_E2E_HOSTKEYS", "TOGO_E2E_USER"] { app.launchEnvironment[key] = env[key] }
        app.launch()
    }

    private var dir: String { ProcessInfo.processInfo.environment["TOGO_E2E_DIR"] ?? "~" }

    // MARK: Probes

    /// Prints the element tree, for learning labels while a flow is written.
    func dump(_ step: String) {
        print("=== DUMP \(step) ===\n\(app.debugDescription)\n=== END \(step) ===")
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = step
        shot.lifetime = .keepAlways
        add(shot)
    }

    /// Waits for `element`, dumping the tree and failing with `what` when it never shows.
    @discardableResult
    func expect(_ element: XCUIElement, _ what: String, timeout: TimeInterval = 15,
                file: StaticString = #filePath, line: UInt = #line) -> XCUIElement {
        if !element.waitForExistence(timeout: timeout) {
            dump(what)
            XCTFail("Never showed: \(what)", file: file, line: line)
        }
        return element
    }

    func any(_ predicate: String, _ args: CVarArg...) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: predicate, argumentArray: args)).firstMatch
    }

    func button(containing text: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label CONTAINS[c] %@", text)).firstMatch
    }

    /// Starts a session for `agent` (its name in the agent list) in the E2E folder.
    func startSession(_ agent: String) {
        expect(app.buttons["roster.settings"], "roster", timeout: 30)
        app.pickFromRosterMenu("New session")
        expect(app.buttons[dir], "the E2E folder").tap()
        let row = app.buttons.matching(NSPredicate(format: "label ==[c] %@ OR label BEGINSWITH[c] %@", agent, agent + ",")).firstMatch
        expect(row, "the \(agent) agent").tap()
        let start = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Start")).firstMatch
        expect(start, "Start").tap()
        expect(app.buttons["Session actions"], "the workspace", timeout: 20)
    }

    /// The composer's text field.
    var composer: XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(
            format: "(elementType == %d OR elementType == %d) AND (placeholderValue BEGINSWITH 'Message' OR label BEGINSWITH 'Message')",
            XCUIElement.ElementType.textField.rawValue, XCUIElement.ElementType.textView.rawValue
        )).firstMatch
    }

    func send(_ text: String, file: StaticString = #filePath, line: UInt = #line) {
        let field = expect(composer, "the composer")
        for _ in 0..<4 where !hasFocus(field) {
            field.tap()
            usleep(500_000)
        }
        guard hasFocus(field) else {
            dump("composer focus")
            XCTFail("The composer never took the keyboard", file: file, line: line)
            return
        }
        field.typeText(text)
        tapVisible(["Send", "Queue"])
    }

    func hasFocus(_ element: XCUIElement) -> Bool {
        (element.value(forKey: "hasKeyboardFocus") as? Bool) ?? false
    }

    /// Somewhere harmless, to close a menu.
    func tapAway() {
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.03)).tap()
    }

    /// Taps the on-screen button with one of these labels: other windows hold hidden twins.
    func tapVisible(_ labels: [String], file: StaticString = #filePath, line: UInt = #line) {
        let deadline = Date().addingTimeInterval(10)
        while Date() < deadline {
            for label in labels {
                if let b = app.visibleButton(label), b.isEnabled { b.tap(); return }
            }
            usleep(300_000)
        }
        dump("tap \(labels)")
        XCTFail("No enabled \(labels) on screen", file: file, line: line)
    }

    // MARK: Flow 1: omp chat

    func testOmpChatStreamsAndFolds() {
        startSession("omp")
        expect(composer, "the chat composer", timeout: 40)
        dump("omp-chat-open")
        send("hello from the phone")
        let reply = any("label CONTAINS %@", "Echo: hello from the phone")
        expect(reply, "the streamed reply", timeout: 60)
        dump("omp-after-reply")
        send("RUN: echo folded-step-ok")
        expect(any("label CONTAINS %@", "Done. The command printed"), "the tool turn's answer", timeout: 60)
        let work = any("label BEGINSWITH %@", "Worked")
        expect(work, "the folded work line")
        dump("omp-folded")
        work.tap()
        sleep(2)
        dump("omp-expanded")
    }

    // MARK: Flow 2: queue, take back, interrupt

    func testOmpQueueTakeBackInterrupt() {
        startSession("omp")
        expect(composer, "the chat composer", timeout: 40)
        send("SLOW first")
        expect(app.buttons["Interrupt"], "Interrupt while working", timeout: 30)
        send("keep me")
        send("take me back")
        let takeBack = app.buttons.matching(identifier: "Take back")
        XCTAssertTrue(takeBack.element(boundBy: 1).waitForExistence(timeout: 10), "Both queued prompts offer Take back")
        dump("queued")
        takeBack.element(boundBy: 1).tap()
        XCTAssertTrue(any("label CONTAINS %@", "take me back").waitForNonExistence(timeout: 10), "Taken back")
        expect(any("label CONTAINS %@", "Echo: keep me"), "the queued prompt ran after the turn", timeout: 90)
        sleep(5)
        XCTAssertFalse(any("label CONTAINS %@", "Echo: take me back").exists, "The taken-back prompt never ran")
        dump("after-queue")
        send("SLOW second")
        expect(app.buttons["Interrupt"], "Interrupt on the second turn", timeout: 30).tap()
        XCTAssertTrue(app.buttons["Interrupt"].waitForNonExistence(timeout: 20), "Interrupt ended the turn")
        dump("interrupted")
    }

    func testProbeSendTiming() {
        startSession("omp")
        expect(composer, "the chat composer", timeout: 40)
        let field = composer
        field.tap(); usleep(500_000)
        field.typeText("first ping")
        tapVisible(["Send", "Queue"])
        for t in [1, 3, 8] {
            sleep(t == 1 ? 1 : (t == 3 ? 2 : 5))
            dump("send-\(t)s")
        }
    }

    // MARK: Flow 5: omp model picker

    /// Opens the palette, then a command's native screen.
    func openCommandScreen(_ command: String) {
        tapVisible(["Commands"])
        let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "/\(command),")).firstMatch
        expect(row, "/\(command) in the palette", timeout: 40).tap()
        expect(app.buttons["Open"], "Open on the /\(command) page").tap()
    }

    // MARK: Flows 4, 6, 7: mission control, palette and mention, terminal layer

    func testOmpMissionPaletteMentionAndTerminal() {
        startSession("omp")
        expect(composer, "the chat composer", timeout: 40)
        let file = "made-\(Int(Date().timeIntervalSince1970) % 100000).txt"
        send("RUN: echo hi > \(file)")
        expect(any("label CONTAINS %@", "Done. The command printed"), "the tool turn", timeout: 60)

        // Mission control: sections in order, the new file under Changes.
        app.buttons["mission.pill"].tap()
        expect(app.navigationBars["Mission control"], "mission control")
        // The sheet opens at half height: scroll it so every section is laid out.
        let sections = ["Agents", "Background", "Plan", "Changes", "Ports", "Status"]
        var seen = Set(sections.filter { app.staticTexts[$0].exists })
        for _ in 0..<4 where seen.count < sections.count {
            app.scrollViews.containing(.staticText, identifier: "Agents").firstMatch.swipeUp()
            seen.formUnion(sections.filter { app.staticTexts[$0].exists })
        }
        let missing = sections.filter { !seen.contains($0) }
        if !missing.isEmpty { dump("mission-sections") }
        XCTAssertEqual(missing, [], "Mission control lacks sections")
        expect(any("label CONTAINS %@", file), "the new file under Changes", timeout: 30)
        dump("mission-changes")

        // Status, then Compaction, then Snapcompact (asks first).
        let compaction = app.buttons["Compaction"]
        expect(compaction, "the Compaction screen in Status").tap()
        expect(app.navigationBars["Compaction"], "the Compaction screen", timeout: 20)
        dump("compaction")
        tapVisible(["Snapcompact"])
        let sheet = app.alerts.firstMatch
        if sheet.waitForExistence(timeout: 5) {
            sheet.buttons.allElementsBoundByIndex.first { $0.label != "Cancel" }?.tap()
        }
        sleep(3)
        dump("after-snapcompact")
        // Back to the chat: close the screen and mission control.
        for _ in 0..<3 where !composer.isHittable { tapVisible(["Done", "Close"]); sleep(1) }
        expect(any("label CONTAINS[c] %@", "compact"), "a compaction notice in the chat", timeout: 90)

        // @ mention.
        tapVisible(["Mention a file"])
        expect(app.buttons["notes.txt"], "notes.txt in the mention menu", timeout: 15).tap()
        XCTAssertTrue((composer.value as? String ?? "").contains("@notes.txt"), "The mention went into the field")
        tapVisible(["Send", "Queue"])
        expect(any("label CONTAINS %@", "Echo: @notes.txt"), "the mention reached the agent", timeout: 60)

        // Terminal and back.
        tapVisible(["Terminal"])
        expect(app.textViews["Terminal"], "the terminal layer", timeout: 15)
        tapVisible(["Chat"])
        expect(composer, "the chat again", timeout: 15)

        // Show in terminal from a reply's menu.
        let reply = any("label BEGINSWITH %@", "Agent: Echo: @notes.txt")
        reply.press(forDuration: 1.2)
        expect(app.buttons["Show in terminal"], "Show in terminal in the reply's menu").tap()
        expect(app.textViews["Terminal"], "the terminal after Show in terminal", timeout: 15)
        sleep(1)
        dump("show-in-terminal")
    }

    /// Gives `role` to the first model of `provider` from its context menu, confirming.
    func setRole(_ role: String, provider: String) {
        let chip = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "\(provider),")).firstMatch
        expect(chip, "the \(provider) chip").tap()
        let row = app.cells.buttons.matching(NSPredicate(
            format: "label CONTAINS %@ AND NOT label BEGINSWITH %@", "context", "\(provider),"
        )).firstMatch
        expect(row, "a \(provider) model").press(forDuration: 1.2)
        // The menu's role items carry a circle (or a check on the role's holder); the chips do not.
        let item = app.buttons.matching(NSPredicate(format: "label == %@", role))
            .containing(.image, identifier: "circle").firstMatch
        expect(item, "\(role) in the context menu").tap()
        // The confirmation names the role; its confirming button is the last one offered.
        let sheet = app.sheets.firstMatch.exists ? app.sheets.firstMatch : app.alerts.firstMatch
        if sheet.waitForExistence(timeout: 5) {
            dump("role-confirm")
            sheet.buttons.allElementsBoundByIndex.first { $0.label != "Cancel" }?.tap()
        } else {
            dump("role-no-confirm")
        }
    }

    func testOmpModelPickerSetsAndRestoresARole() {
        startSession("omp")
        expect(composer, "the chat composer", timeout: 40)
        openCommandScreen("model")
        expect(app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Smol,")).firstMatch, "the role chips", timeout: 20)
        setRole("Smol", provider: "apple")
        let moved = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Smol, Apple")).firstMatch
        expect(moved, "the Smol chip naming the Apple model", timeout: 15)
        dump("role-set")
        sleep(6)
        setRole("Smol", provider: "llama.cpp")
        let back = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Smol, Qwen")).firstMatch
        expect(back, "the Smol chip back on Qwen", timeout: 15)
    }

    func testProbeModelPicker() {
        startSession("omp")
        expect(composer, "the chat composer", timeout: 40)
        openCommandScreen("model")
        sleep(5)
        dump("picker")
        let chip = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "apple,")).firstMatch
        expect(chip, "the apple provider chip").tap()
        sleep(2)
        dump("picker-search")
        let row = app.cells.buttons.matching(NSPredicate(format: "label CONTAINS %@ AND NOT label BEGINSWITH %@", "context", "apple,")).firstMatch
        if row.waitForExistence(timeout: 5) {
            row.press(forDuration: 1.2)
            sleep(2)
            dump("picker-menu")
        }
    }

    /// Opens each native surface once and dumps it, to learn labels.
    func testProbeSurfaces() {
        startSession("omp")
        expect(composer, "the chat composer", timeout: 40)
        app.buttons["Session actions"].tap(); sleep(2); dump("session-actions")
        tapAway(); sleep(1)
        app.buttons["mission.pill"].tap(); sleep(3); dump("mission")
        tapVisible(["Done"]); sleep(1)
        tapVisible(["Commands"]); sleep(4); dump("commands")
        app.swipeDown(); sleep(1)
        tapVisible(["Mention a file"]); sleep(4); dump("mention")
    }

    func testProbeRoster() {
        sleep(8)
        dump("roster")
        app.pickFromRosterMenu("New session")
        sleep(3)
        dump("new-session")
    }
}
