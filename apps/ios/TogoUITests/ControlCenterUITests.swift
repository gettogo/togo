import XCTest

/// Launches the app on its scripted machines: Studio with sessions 01, 04 and scratch and a
/// request from 04, and Build box asleep with 02.
/// `arguments` go after the app's own, for system settings such as the text size.
@MainActor func launchTogo(firstRun: Bool = false, arguments: [String] = []) -> XCUIApplication {
    let app = XCUIApplication()
    app.launchArguments = ["-ui-fakes", "-ui-skip-launch"] + (firstRun ? ["-ui-first-run"] : []) + arguments
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
    /// The same holds for the list dimmed around a confirmation popover.
    /// A disabled control is drawn faded on purpose and is exempt from contrast minimums, and so
    /// is one the keyboard covers whole.
    /// A contrast report on text or a button is checked against its pixels: the audit samples
    /// small text at 2x and can misread antialiased glyphs or a filled button. Within the
    /// element's own frame the background is the most common colour and the text is every
    /// pixel that stands apart from it; the glyphs' colour is the median of the most distinct
    /// half of those. When that meets 4.5:1 against the background the report is let through.
    /// Other elements are judged by the audit alone.
    /// `fileNames` are labels that are a file's name, such as a viewer's title: the audit reads
    /// "README.md" as not human-readable, and no other words would name the file.
    @MainActor func audit(
        _ app: XCUIApplication, glass: Set<String> = [], overList: Bool = false, fileNames: Set<String> = [],
        file: StaticString = #filePath, line: UInt = #line
    ) {
        let screen = app.screenshot().image
        // The keyboard covers the screen from its top edge down; its frame leaves out the strip
        // with the emoji and dictation keys at the bottom.
        let window = app.windows.firstMatch.frame
        let keyboard = app.keyboards.firstMatch.exists
            ? app.keyboards.firstMatch.frame.union(CGRect(x: window.minX, y: window.maxY - 1, width: window.width, height: 1))
            : nil
        do {
            try app.performAccessibilityAudit(for: .all.subtracting([.textClipped, .dynamicType])) { issue in
                if issue.auditType == .contrast, let label = issue.element?.label, glass.contains(label) {
                    return true
                }
                if overList, issue.auditType == .elementDetection, issue.element == nil {
                    return true
                }
                if issue.auditType == .contrast, let element = issue.element, !element.isEnabled {
                    return true
                }
                // A control wholly under the keyboard shows only as a blur through it, and nobody
                // reads it there. One partly in view is still audited; the point of slack is for
                // frames that round past the window's edge.
                if issue.auditType == .contrast, let element = issue.element, let keyboard,
                   keyboard.contains(element.frame.insetBy(dx: 1, dy: 1)) {
                    return true
                }
                if issue.auditType == .contrast, let element = issue.element,
                   [.staticText, .button].contains(element.elementType),
                   let ratio = screen.textContrast(in: element.frame), ratio >= 4.5 {
                    return true
                }
                if issue.auditType == .sufficientElementDescription, let label = issue.element?.label,
                   fileNames.contains(label) {
                    return true
                }
                let element = issue.element.map { "\($0.elementType.rawValue) '\($0.label)' at \($0.frame)" } ?? "screen"
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

    @MainActor func assertTappable(_ element: XCUIElement, file: StaticString = #filePath, line: UInt = #line) {
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

extension UIImage {
    /// The contrast of the text in `rect` against its background, in points. Nil when the rect
    /// holds no pixels or too few stand apart from the background to be text.
    func textContrast(in rect: CGRect) -> Double? {
        guard let cg = cgImage else { return nil }
        let pixels = CGRect(x: rect.minX * scale, y: rect.minY * scale, width: rect.width * scale, height: rect.height * scale)
            .integral.intersection(CGRect(x: 0, y: 0, width: cg.width, height: cg.height))
        guard !pixels.isEmpty, let crop = cg.cropping(to: pixels) else { return nil }
        let width = crop.width, height = crop.height
        var bytes = [UInt8](repeating: 0, count: width * height * 4)
        guard let context = CGContext(
            data: &bytes, width: width, height: height, bitsPerComponent: 8, bytesPerRow: width * 4,
            space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue
        ) else { return nil }
        context.draw(crop, in: CGRect(x: 0, y: 0, width: width, height: height))
        var luminances: [Double] = []
        luminances.reserveCapacity(width * height)
        var counts: [Double: Int] = [:]
        for index in stride(from: 0, to: bytes.count, by: 4) {
            let value = Self.luminance(bytes[index], bytes[index + 1], bytes[index + 2])
            luminances.append(value)
            counts[value, default: 0] += 1
        }
        guard let background = counts.max(by: { $0.value < $1.value })?.key else { return nil }
        func ratio(_ a: Double, _ b: Double) -> Double { (max(a, b) + 0.05) / (min(a, b) + 0.05) }
        // Glyph pixels, edges included, ordered from least to most distinct.
        let text = luminances.map { ratio($0, background) }.filter { $0 >= 1.5 }.sorted()
        guard text.count >= max(8, luminances.count / 100) else { return nil }
        let core = text[(text.count / 2)...]
        return core[core.startIndex + core.count / 2]
    }

    private static func luminance(_ r: UInt8, _ g: UInt8, _ b: UInt8) -> Double {
        func channel(_ value: UInt8) -> Double {
            let v = Double(value) / 255
            return v <= 0.039_28 ? v / 12.92 : pow((v + 0.055) / 1.055, 2.4)
        }
        return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b)
    }
}
