import XCTest

/// The review screens on the scripted machines. Session 04 (`app`) has three changes: App.swift
/// not staged, and two files staged already. Session 01 (`api`) has the full sample repository,
/// untracked files, history, a file tree and three listening ports.
@MainActor final class ReviewUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
    }

    // MARK: Budgets

    /// A fast sideways fling over a diff of 2,000-column lines, with wrapping off. The hitch
    /// ratio of each fling is measured by the system's scrolling signposts; `make ios-scroll-perf`
    /// holds it under 5 ms/s from the result bundle, which XCTest does not expose to the test.
    /// Only a device reports hitches; a simulator run measures the fling's duration alone.
    func testWideDiffFlingUnderHitchBudget() throws {
        try skipBudgetIfOverloaded("Scrolling")
        let app = launchTogo(arguments: ["-ui-wide-diff"])
        openChanges(app, bib: "01", name: "api", count: 9)
        let file = changeRow(app, "table.json")
        XCTAssertTrue(reveal(file, in: app), "table.json is not listed")
        file.tap()
        let code = app.collectionViews["code-lines"]
        XCTAssertTrue(code.waitForExistence(timeout: 10), "The diff never showed its code")

        let options = XCTMeasureOptions()
        options.iterationCount = 5
        options.invocationOptions = [.manuallyStop]
        measure(metrics: [XCTOSSignpostMetric.scrollingAndDecelerationMetric], options: options) {
            code.swipeLeft(velocity: .fast)
            stopMeasuring()
            code.swipeRight(velocity: .fast)
        }
    }

    // MARK: Flows

    func testStageAndCommitFromWorkspace() {
        stageAndCommit(launchTogo())
    }

    /// The same flow at the largest accessibility size, where row actions and header buttons
    /// must still be full-size targets.
    func testStageAndCommitAtAccessibilitySize() {
        let app = launchTogo(arguments: ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXXXL"])
        stageAndCommit(app, largeType: true)
    }

    func testDiscardAsksFirst() {
        let app = launchTogo()
        openChanges(app, bib: "01", name: "api", count: 9)

        let notes = changeRow(app, "notes.txt")
        XCTAssertTrue(reveal(notes, in: app), "The untracked file is not listed")

        // Cancel leaves the file where it was.
        swipeToDelete(notes, in: app)
        let named = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "notes.txt")).firstMatch
        XCTAssertTrue(named.waitForExistence(timeout: 5), "The dialog does not name the file")
        auditScreen(app, overList: true)
        cancelDialog(app)
        XCTAssertTrue(notes.waitForExistence(timeout: 5), "Cancel removed the file")

        swipeToDelete(notes, in: app)
        XCTAssertTrue(named.waitForExistence(timeout: 5))
        guard let delete = confirmButton(app, besides: notes) else { return XCTFail("The dialog offers no Delete") }
        delete.tap()
        XCTAssertTrue(notes.waitForNonExistence(timeout: 5), "Delete left the file listed")
        // Back to the top, so no row sits under the opaque tab picker, where the audit would
        // measure its text against the picker's band.
        app.swipeDown(velocity: .fast)
        auditScreen(app)
    }

    func testDiffModesOnIPad() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .pad, "Side by side needs an iPad's width")
        let app = launchTogo()
        openChanges(app, bib: "04", name: "app", count: 3)

        // App.swift changes in two places, so there is a next change to go to.
        let file = changeRow(app, "App.swift")
        XCTAssertTrue(reveal(file, in: app), "App.swift is not listed")
        file.tap()
        let next = app.buttons["Next change"]
        XCTAssertTrue(next.waitForExistence(timeout: 5), "The diff has no Next change")
        XCTAssertTrue(file.isSelected, "The sidebar does not mark the file open beside it")
        auditScreen(app, glass: ["Stage", "Unstage"], fileNames: ["App.swift"])

        // Side by side needs 700 points: beside the sidebar of a 13-inch iPad the diff has less,
        // so the sidebar goes first.
        let hide = app.buttons["Hide Sidebar"]
        if hide.exists { hide.tap() }
        openOptions(app)
        let sideBySide = app.buttons["Side by side"]
        XCTAssertTrue(sideBySide.waitForExistence(timeout: 5), "The options offer no Side by side")
        sideBySide.tap()
        XCTAssertTrue(next.waitForExistence(timeout: 5))
        auditScreen(app, glass: ["Stage", "Unstage"], fileNames: ["App.swift"])

        XCTAssertTrue(next.isEnabled, "A diff with two changes can't move to the next one")
        next.tap()
        XCTAssertTrue(app.buttons["Previous change"].isEnabled, "Next change did not move")

        openOptions(app)
        let wrap = app.switches["Wrap lines"].exists ? app.switches["Wrap lines"] : app.buttons["Wrap lines"]
        XCTAssertTrue(wrap.waitForExistence(timeout: 5), "The options have no Wrap lines")
        wrap.tap()
        XCTAssertTrue(next.waitForExistence(timeout: 5))
        auditScreen(app, glass: ["Stage", "Unstage"], fileNames: ["App.swift"])
    }

    func testHistoryToFileAtCommit() {
        let app = launchTogo()
        openChanges(app, bib: "01", name: "api", count: 9)
        selectTab(app, "History")

        let commit = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "Return 404 for unknown ids")).firstMatch
        XCTAssertTrue(commit.waitForExistence(timeout: 5), "The history lists no commits")
        auditScreen(app)
        // The newest commit's short hash, as the list prints it under its subject.
        let hash = app.staticTexts.matching(NSPredicate(format: "label MATCHES %@", "[0-9a-f]{7}")).firstMatch
        XCTAssertTrue(hash.exists, "The history shows no short hash")
        let short = hash.label
        commit.tap()

        let browse = app.buttons["Browse files at this commit"]
        XCTAssertTrue(browse.waitForExistence(timeout: 5), "The commit offers no way to its files")
        auditScreen(app)
        browse.tap()

        let readme = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "README.md")).firstMatch
        XCTAssertTrue(readme.waitForExistence(timeout: 5), "The commit's tree lists no README.md")
        auditScreen(app)
        readme.tap()

        let banner = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "At ")).firstMatch
        XCTAssertTrue(banner.waitForExistence(timeout: 5), "The file does not say which commit it shows")
        XCTAssertEqual(banner.label, "At \(short)", "The banner does not name the commit opened")
        auditScreen(app, fileNames: ["README.md"])

        // The commit's folder, where its banner offers the way back: beside the file in a split
        // view, under it on a phone.
        let back = app.buttons["Back to working tree"]
        if !isPad { app.navigationBars.buttons["BackButton"].firstMatch.tap() }
        XCTAssertTrue(back.waitForExistence(timeout: 5), "The commit's folder offers no way back")
        auditScreen(app, fileNames: ["README.md"])
        back.tap()
        XCTAssertTrue(back.waitForNonExistence(timeout: 5), "Back to working tree still shows the commit")
        XCTAssertTrue(
            app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "README.md")).firstMatch.waitForExistence(timeout: 5),
            "Back to working tree does not list the working tree"
        )
        auditScreen(app, fileNames: ["README.md"])
    }

    func testPreviewOpensPort() {
        let app = launchTogo()
        openChanges(app, bib: "01", name: "api", count: 9)
        selectTab(app, "Ports")

        let open = app.buttons["Open 3000"]
        XCTAssertTrue(open.waitForExistence(timeout: 5), "Ports lists no 3000")
        auditScreen(app)
        open.tap()

        let page = app.webViews.staticTexts["Running on 3000"]
        XCTAssertTrue(page.waitForExistence(timeout: 10), "The preview never shows the server's page")
        auditScreen(app)
        guard let done = app.visibleButton("Done") else { return XCTFail("The preview has no Done") }
        done.tap()
        XCTAssertTrue(page.waitForNonExistence(timeout: 5), "Done left the preview up")
        // Closing the preview stops its forward, so the row offers Open again.
        XCTAssertTrue(open.waitForExistence(timeout: 5), "Closing the preview left its forward open")
        XCTAssertFalse(app.buttons["Show 3000"].exists)
        auditScreen(app)
    }

    func testControlCenterRowOpensChanges() {
        let app = launchTogo()
        let row = app.row("04", "app")
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        row.press(forDuration: 1)

        let changes = app.buttons["Changes"]
        XCTAssertTrue(changes.waitForExistence(timeout: 5), "The row's menu has no Changes")
        changes.tap()

        XCTAssertTrue(changeRow(app, "App.swift").waitForExistence(timeout: 5), "Changes did not open on the session's files")
        auditScreen(app)
    }

    // MARK: Steps

    /// Audits the screen once it has stopped moving, with a screenshot kept for a failure. Lists
    /// cross-fade when their rows change; text caught halfway through reads as low contrast, so
    /// the audit waits until the screen draws the same twice in a row.
    /// While the review is a sheet over the dimmed workspace or control center, as on iPad,
    /// `overList` covers the screen around it and the sheet's Close and the workspace's Sessions
    /// count as glass. The sheet is detected, not assumed: the bar button of the screen under
    /// it, Sessions or Settings, is there but covered.
    private func auditScreen(
        _ app: XCUIApplication, glass: Set<String> = [], overList: Bool = false, fileNames: Set<String> = [],
        file: StaticString = #filePath, line: UInt = #line
    ) {
        add(XCTAttachment(screenshot: settledScreenshot(app)))
        let overWorkspace = isPad && ["Sessions", "roster.more"].contains { name in
            let button = app.buttons[name]
            return button.exists && !button.isHittable
        }
        audit(
            app, glass: overWorkspace ? glass.union(["Close", "Sessions"]) : glass, overList: overList || overWorkspace,
            fileNames: fileNames, file: file, line: line
        )
    }

    private var isPad: Bool { UIDevice.current.userInterfaceIdiom == .pad }

    /// The screen once it has drawn the same pixels for `still` seconds: no row sliding or
    /// fading, and no reply about to land after a pause. A blinking caret never settles, so after
    /// `timeout` the latest capture is used.
    private func settledScreenshot(_ app: XCUIApplication, still: TimeInterval = 0.4, timeout: TimeInterval = 4) -> XCUIScreenshot {
        let deadline = Date().addingTimeInterval(timeout)
        var last = app.screenshot()
        var since = Date()
        while Date() < deadline {
            RunLoop.current.run(until: Date().addingTimeInterval(1.0 / 30))
            let next = app.screenshot()
            if next.pngRepresentation != last.pngRepresentation {
                since = Date()
            } else if Date().timeIntervalSince(since) >= still {
                return next
            }
            last = next
        }
        return last
    }

    private func stageAndCommit(_ app: XCUIApplication, largeType: Bool = false) {
        openChanges(app, bib: "04", name: "app", count: 3, largeType: largeType)

        let appSwift = changeRow(app, "App.swift")
        XCTAssertTrue(reveal(appSwift, in: app), "App.swift is not listed")
        XCTAssertTrue(appSwift.label.hasSuffix("not staged"), appSwift.label)
        auditScreen(app)

        appSwift.swipeRight()
        guard let stage = app.visibleButton("Stage") else { return XCTFail("Swiping App.swift offers no Stage") }
        if largeType { assertTappable(stage) }
        stage.tap()
        let staged = changeRow(app, "App.swift", staged: true)
        XCTAssertTrue(staged.waitForExistence(timeout: 5), "Swiping did not stage App.swift")

        let commit = app.buttons["Commit"]
        XCTAssertTrue(commit.waitForExistence(timeout: 5))
        if largeType { assertTappable(commit) }
        let listCommit = commit.frame
        commit.tap()

        let message = app.textFields["Commit message"].exists ? app.textFields["Commit message"] : app.textViews["Commit message"]
        XCTAssertTrue(message.waitForExistence(timeout: 5), "The commit sheet has no message field")
        message.tap()
        message.typeText("Show the store in the app")

        let amend = app.switches["Amend the last commit"]
        XCTAssertTrue(amend.exists, "The commit sheet has no amend switch")
        XCTAssertEqual(amend.value as? String, "0", "Amend starts on")
        // Cancel is a Liquid Glass bar button: see `audit`.
        auditScreen(app, glass: ["Cancel"])

        guard let confirm = sheetButton("Commit", besides: listCommit, in: app) else {
            return XCTFail("The sheet has no Commit button")
        }
        if largeType { assertTappable(confirm) }
        confirm.tap()

        XCTAssertTrue(app.staticTexts["No changes"].waitForExistence(timeout: 5), "Committing left changes listed")
        auditScreen(app)
    }

    /// Opens a session from the control center, then its changes from the workspace header.
    private func openChanges(
        _ app: XCUIApplication, bib: String, name: String, count: Int, largeType: Bool = false
    ) {
        let row = app.row(bib, name)
        XCTAssertTrue(app.rosterMenu.waitForExistence(timeout: 10), "The roster never loaded")
        XCTAssertTrue(reveal(row, in: app), "The control center has no session \(bib)")
        row.tap()

        let header = app.buttons["\(count) changes"]
        XCTAssertTrue(header.waitForExistence(timeout: 10), "The workspace header has no \(count) changes")
        if largeType {
            assertTappable(header)
            if app.buttons["Preview"].exists { assertTappable(app.buttons["Preview"]) }
            assertTappable(app.buttons["Session actions"])
        }
        header.tap()
        XCTAssertTrue(app.buttons["Close"].waitForExistence(timeout: 5), "Changes did not open")
    }

    /// A row on the changes list, found by how VoiceOver reads it: the file name first.
    private func changeRow(_ app: XCUIApplication, _ name: String, staged: Bool? = nil) -> XCUIElement {
        var format = "label BEGINSWITH %@"
        var arguments: [Any] = ["\(name), "]
        if let staged {
            format += " AND label ENDSWITH %@"
            arguments.append(staged ? ", staged" : ", not staged")
        }
        return app.buttons.matching(NSPredicate(format: format, argumentArray: arguments)).firstMatch
    }

    private func selectTab(_ app: XCUIApplication, _ name: String) {
        let tab = app.buttons[name]
        XCTAssertTrue(tab.waitForExistence(timeout: 5), "The review has no \(name) tab")
        tab.tap()
    }

    private func openOptions(_ app: XCUIApplication) {
        let options = app.buttons["View options"]
        XCTAssertTrue(options.waitForExistence(timeout: 5), "The diff has no View options")
        options.tap()
    }

    /// Scrolls the list up until `element` is wholly on screen, above the Commit bar when there is
    /// one: lists build rows lazily, so one below the fold does not exist until scrolled to.
    @discardableResult private func reveal(_ element: XCUIElement, in app: XCUIApplication) -> Bool {
        func shown() -> Bool {
            guard element.exists, element.isHittable else { return false }
            let commit = app.buttons["Commit"]
            let bottom = commit.exists ? commit.frame.minY : app.windows.firstMatch.frame.maxY
            return element.frame.maxY <= bottom
        }
        // The list on top: screens under a sheet keep theirs, and in a split view the list is
        // the sidebar, not the middle of the screen.
        func list() -> XCUIElement {
            app.collectionViews.allElementsBoundByIndex.last { $0.isHittable } ?? app
        }
        for _ in 0..<10 {
            if element.waitForExistence(timeout: 1), shown() { return true }
            list().swipeUp(velocity: .slow)
        }
        return shown()
    }

    private func swipeToDelete(_ row: XCUIElement, in app: XCUIApplication) {
        row.swipeLeft()
        guard let delete = app.visibleButton("Delete") else { return XCTFail("Swiping the row offers no Delete") }
        assertTappable(delete)
        delete.tap()
    }

    /// A button of the sheet on top, not the one of the same name on the screen under it, which
    /// sat at `under`. At large sizes the sheet's form scrolls to bring it above the keyboard.
    private func sheetButton(_ label: String, besides under: CGRect, in app: XCUIApplication) -> XCUIElement? {
        func find() -> XCUIElement? {
            app.buttons.matching(identifier: label).allElementsBoundByIndex.first {
                $0.exists && $0.isHittable && $0.frame != under
            }
        }
        for _ in 0..<6 {
            if let button = find() { return button }
            app.swipeUp(velocity: .slow)
        }
        return find()
    }

    /// Cancel on a sheet-style dialog; a dialog shown as a popover cancels by a tap outside it.
    private func cancelDialog(_ app: XCUIApplication) {
        if let cancel = app.visibleButton("Cancel") {
            cancel.tap()
        } else {
            app.otherElements["PopoverDismissRegion"].firstMatch.tap()
        }
    }

    /// The dialog's destructive button, not the swipe action of the same name still open on `row`.
    private func confirmButton(_ app: XCUIApplication, besides row: XCUIElement) -> XCUIElement? {
        let rowFrame = row.frame
        return app.buttons.matching(identifier: "Delete").allElementsBoundByIndex.first {
            $0.exists && $0.isHittable && !$0.frame.intersects(rowFrame)
        }
    }
}
