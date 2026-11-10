import Foundation
import Testing
import SwiftTerm
import UIKit
@testable import TogoTerminal

/// Owns one engine laid out at a fixed frame and records what it emits.
@MainActor
final class EngineHarness {
    let engine: SwiftTermEngine
    var input: [Data] = []
    var resizes: [(cols: Int, rows: Int)] = []
    var clipboardWrites: [String] = []

    init(settings: TerminalSettings = TerminalSettings(), width: CGFloat = 480, height: CGFloat = 320) {
        engine = SwiftTermEngine(settings: settings, theme: BuiltInThemes.all[0])
        engine.view.frame = CGRect(x: 0, y: 0, width: width, height: height)
        engine.view.layoutIfNeeded()
        engine.onInput = { [unowned self] in self.input.append($0) }
        engine.onResize = { [unowned self] in self.resizes.append($0) }
        engine.onClipboardWrite = { [unowned self] in self.clipboardWrites.append($0) }
    }

    func feed(_ text: String) {
        engine.feed(Array(text.utf8)[...])
    }

    var joinedInput: Data { input.reduce(Data(), +) }
}

/// Polls on the main actor so main-queue hops inside SwiftTerm get to run.
@MainActor
func waitUntil(_ timeout: Duration = .seconds(3), _ condition: () -> Bool) async -> Bool {
    let clock = ContinuousClock()
    let deadline = clock.now + timeout
    while !condition() {
        if clock.now >= deadline { return false }
        try? await Task.sleep(for: .milliseconds(5))
    }
    return true
}

/// Lets queued main-actor work drain when a test asserts that something did not happen.
@MainActor
func settle() async {
    try? await Task.sleep(for: .milliseconds(250))
}

let esc = "\u{1b}"

@MainActor
@Suite struct EngineTests {
    @Test func feedRendersText() {
        let h = EngineHarness()
        h.feed("hello")
        #expect(h.engine.lineText(0).hasPrefix("hello"))
    }

    @Test func onResizeFollowsFrame() async {
        let h = EngineHarness()
        await settle()
        h.resizes.removeAll()
        let before = h.engine.size
        let cell = h.engine.cellSize
        #expect(cell.width > 0)

        var frame = h.engine.view.frame
        frame.size.width += cell.width * 10
        h.engine.view.frame = frame
        h.engine.view.layoutIfNeeded()
        await settle()

        #expect(h.resizes.count == 1)
        #expect(h.resizes.first?.cols == before.cols + 10)
        #expect(h.engine.size.cols == before.cols + 10)
        #expect(h.engine.size.rows == before.rows)
    }

    /// A frame narrower than a cell measures no columns, but the emulator keeps two and one
    /// row: the remote is never told of an empty grid.
    @Test func narrowFrameNeverReportsAnEmptyGrid() async {
        let h = EngineHarness()
        await settle()
        h.resizes.removeAll()
        var frame = h.engine.view.frame
        frame.size.width = 1
        h.engine.view.frame = frame
        h.engine.view.layoutIfNeeded()
        await settle()
        #expect(h.resizes.allSatisfy { $0.cols >= 2 && $0.rows >= 1 })
        #expect(h.engine.size.cols >= 2)

        h.engine.reportSize(cols: 0, rows: 0)
        #expect(h.engine.size.cols == 2 && h.engine.size.rows == 1)
        #expect(h.resizes.allSatisfy { $0.cols >= 2 && $0.rows >= 1 })
    }

    /// A fold can leave the host, for a moment, too small for one row: the grid, and the
    /// scrollback that would reflow to two columns, keep their size until there is room again.
    @Test func hostTooSmallForAGridKeepsTheGrid() async {
        let h = EngineHarness()
        let controller = TerminalViewController(engine: h.engine)
        controller.view.frame = CGRect(x: 0, y: 0, width: 480, height: 320)
        controller.refit()
        await settle()
        let before = h.engine.size
        h.resizes.removeAll()

        for width in [CGFloat(1), 0, 2] {
            controller.view.frame = CGRect(x: 0, y: 0, width: width, height: 320)
            controller.refit()
        }
        controller.view.frame = CGRect(x: 0, y: 0, width: 480, height: 2)
        controller.refit()
        await settle()
        #expect(h.engine.size.cols == before.cols && h.engine.size.rows == before.rows)
        #expect(h.resizes.isEmpty)

        controller.view.frame = CGRect(x: 0, y: 0, width: 480, height: 320)
        controller.refit()
        await settle()
        #expect(h.engine.size.cols == before.cols && h.engine.size.rows == before.rows)
    }

    @Test func holdsGridNeedsTwoColumnsAndARow() {
        let cell = CGSize(width: 8, height: 16)
        #expect(TerminalFit.holdsGrid(CGSize(width: 16, height: 16), cell: cell))
        #expect(!TerminalFit.holdsGrid(CGSize(width: 15, height: 400), cell: cell))
        #expect(!TerminalFit.holdsGrid(CGSize(width: 400, height: 15), cell: cell))
        #expect(!TerminalFit.holdsGrid(.zero, cell: .zero))
        #expect(TerminalFit.holdsGrid(CGSize(width: 1, height: 1), cell: .zero))
    }

    @Test func marginsSetTheGridsLeadingAndTopEdges() {
        let bounds = CGRect(x: 0, y: 0, width: 402, height: 700)
        let margins = UIEdgeInsets(top: 0, left: 16, bottom: 0, right: 16)
        let rect = TerminalFit.frame(bounds: bounds, safeArea: .zero, keyboard: nil, visible: bounds, margins: margins)
        #expect(rect == CGRect(x: 16, y: 0, width: 370, height: 700))
        let tiny = TerminalFit.frame(bounds: CGRect(x: 0, y: 0, width: 20, height: 20), safeArea: .zero, keyboard: nil,
                                     visible: bounds, margins: margins)
        #expect(tiny == .zero)
    }

    @Test func coveredTerminalKeepsItsGridThroughTheSwitch() async {
        let h = EngineHarness()
        let controller = TerminalViewController(engine: h.engine)
        controller.view.frame = CGRect(x: 0, y: 0, width: 480, height: 320)
        controller.refit()
        await settle()
        let before = h.engine.size
        controller.isCovered = true
        controller.view.frame = CGRect(x: 0, y: 0, width: 480, height: 200)
        controller.refit()
        #expect(h.engine.size.rows == before.rows)
        try? await Task.sleep(for: TerminalViewController.layerSettle + .milliseconds(100))
        await settle()
        #expect(h.engine.size.rows < before.rows)
    }

    @Test func insertTextEmitsInput() async {
        let h = EngineHarness()
        h.engine.textInputView.insertText("a")
        #expect(await waitUntil { !h.input.isEmpty })
        #expect(h.joinedInput == Data("a".utf8))
    }

    /// Dictation and text replacements arrive as one multi-character insert, which must not
    /// carry controls the way a paste must not.
    @Test func multiCharacterInsertDropsControls() async {
        let h = EngineHarness()
        h.engine.textInputView.insertText("ls\(esc)[31m -la\u{9b}\u{7f}\tx")
        #expect(await waitUntil { !h.input.isEmpty })
        await settle()
        #expect(h.joinedInput == Data("ls[31m -la\tx".utf8))
    }

    @Test func singleTypedControlPassesThrough() async {
        let h = EngineHarness()
        h.engine.textInputView.insertText("\t")
        #expect(await waitUntil { !h.input.isEmpty })
        #expect(h.joinedInput == Data("\t".utf8))
    }

    @Test func imageOnThePasteboardGoesToTheImageHook() {
        let h = EngineHarness()
        let board = UIPasteboard.withUniqueName()
        board.image = UIGraphicsImageRenderer(size: CGSize(width: 2, height: 2)).image { _ in }
        var pasted: Data?
        h.engine.onPasteImage = { pasted = $0 }
        h.engine.pasteFromClipboard(board)
        #expect(pasted != nil)
        #expect(h.input.isEmpty)
        UIPasteboard.remove(withName: board.name)
    }

    @Test func markedTextCommitsOnce() async {
        let h = EngineHarness()
        h.engine.textInputView.setMarkedText("に", selectedRange: NSRange(location: 1, length: 0))
        h.engine.textInputView.insertText("日")
        #expect(await waitUntil { !h.input.isEmpty })
        await settle()
        #expect(h.joinedInput == Data("日".utf8))
    }

    @Test func wideGlyphsTakeTwoCells() {
        let h = EngineHarness()
        h.feed("日本")
        #expect(h.engine.cursorPosition.col == 4)
    }

    @Test func sendUsesApplicationCursorMode() async {
        let h = EngineHarness()
        h.feed("\(esc)[?1h")
        h.engine.send(KeyStroke(.up))
        #expect(await waitUntil { !h.input.isEmpty })
        #expect(h.joinedInput == Data("\(esc)OA".utf8))
    }

    @Test func pasteBracketedWhenEnabled() async {
        let h = EngineHarness()
        h.feed("\(esc)[?2004h")
        h.engine.paste("a\nb")
        #expect(await waitUntil { !h.input.isEmpty })
        await settle()
        #expect(h.input == [Data("\(esc)[200~a\nb\(esc)[201~".utf8)])

        let plain = EngineHarness()
        plain.engine.paste("a\nb")
        #expect(await waitUntil { !plain.input.isEmpty })
        #expect(plain.joinedInput == Data("a\nb".utf8))
    }

    @Test func pastedEndMarkerCannotCloseBracket() async {
        let h = EngineHarness()
        h.feed("\(esc)[?2004h")
        h.engine.paste("x\(esc)[201~rm -rf ~\r")
        #expect(await waitUntil { !h.input.isEmpty })
        #expect(h.joinedInput == Data("\(esc)[200~x[201~rm -rf ~\r\(esc)[201~".utf8))
    }

    @Test func nestedEndMarkerCannotReassemble() async {
        let h = EngineHarness()
        h.feed("\(esc)[?2004h")
        h.engine.paste("\(esc)[20\(esc)[201~1~echo owned\r")
        #expect(await waitUntil { !h.input.isEmpty })
        #expect(h.joinedInput == Data("\(esc)[200~[20[201~1~echo owned\r\(esc)[201~".utf8))
    }

    @Test func pasteDropsEscapeDeleteAndC1Controls() {
        #expect(PasteText.sanitize("a\(esc)b") == "ab")
        #expect(PasteText.sanitize("\u{9B}31mred") == "31mred")
        #expect(PasteText.sanitize("x\u{7F}\u{00}\u{07}\u{85}y") == "xy")
    }

    @Test func pasteKeepsTabsNewlinesAndUnicode() {
        let text = "col1\tcol2\nline two\r\nnaïve 日本語 \u{1F600} مرحبا"
        #expect(PasteText.sanitize(text) == text)
    }

    @Test func editMenuPasteEndsTheSelection() async {
        let h = EngineHarness()
        h.feed("some output")
        h.engine.selectAll()
        #expect(h.engine.isSelecting)
        // Nothing survives the filter, so no input goes out to end the selection on the way.
        h.engine.terminalView.pasteboardText = { "\(esc)" }
        h.engine.terminalView.paste(nil)
        #expect(await waitUntil { !h.engine.isSelecting })
    }

    @Test func editMenuPasteTakesTheSanitisedPath() async {
        let h = EngineHarness()
        h.feed("\(esc)[?2004h")
        h.engine.terminalView.pasteboardText = { "hi\(esc)[201~\r" }
        h.engine.terminalView.paste(nil)
        #expect(await waitUntil { !h.input.isEmpty })
        #expect(h.joinedInput == Data("\(esc)[200~hi[201~\r\(esc)[201~".utf8))
    }

    @Test func plainPasteIsSanitisedToo() async {
        let h = EngineHarness()
        h.engine.paste("ls\(esc)[A\r")
        #expect(await waitUntil { !h.input.isEmpty })
        #expect(h.joinedInput == Data("ls[A\r".utf8))
    }

    @Test func longTitleIsTruncated() async {
        let h = EngineHarness()
        var titles: [String] = []
        h.engine.onTitle = { titles.append($0) }
        h.feed("\(esc)]2;" + String(repeating: "t", count: 10_000) + "\u{07}")
        #expect(await waitUntil { !titles.isEmpty })
        #expect(titles.first?.count == SwiftTermEngine.maximumTitleLength)
    }

    @Test func findInBuffer() {
        let h = EngineHarness()
        h.feed("alpha\r\nbeta")
        #expect(h.engine.find("beta", caseSensitive: false, regex: false, backwards: false))
        #expect(!h.engine.find("gamma", caseSensitive: false, regex: false, backwards: false))
        h.engine.clearFind()
    }

    @Test func titleReachesCallback() async {
        let h = EngineHarness()
        var titles: [String] = []
        h.engine.onTitle = { titles.append($0) }
        h.feed("\(esc)]2;build\u{07}")
        #expect(await waitUntil { !titles.isEmpty })
        #expect(titles == ["build"])
    }

    @Test func readNewOutputFollowsFocusAndSetting() {
        let h = EngineHarness()
        let feed = h.engine.outputFeed
        feed.voiceOverRunning = { true }
        feed.focused = { true }
        feed.refresh()
        #expect(feed.isActive)
        #expect(h.engine.terminalView.notifyUpdateChanges)
        var settings = TerminalSettings()
        settings.readNewOutput = false
        h.engine.apply(settings)
        #expect(!feed.enabled)
        #expect(!feed.isActive)
        #expect(!h.engine.terminalView.notifyUpdateChanges)
    }

    @Test func liveScreenReportsTheAlternateScreen() async {
        let h = EngineHarness()
        h.feed("$ ls")
        #expect(await waitUntil { h.engine.liveScreen()?.rows.first == "$ ls" })
        #expect(h.engine.liveScreen()?.isAlternate == false)
        h.feed("\u{1b}[?1049h~")
        #expect(await waitUntil { h.engine.liveScreen()?.isAlternate == true })
    }

    @Test func cursorLineIsNotNamedWhileTheCursorIsHidden() async {
        let h = EngineHarness()
        h.feed("$ ls")
        #expect(await waitUntil { h.engine.terminalView.accessibilityValue == "Cursor line, $ ls" })
        h.feed("\u{1b}[?25l")
        #expect(await waitUntil { h.engine.terminalView.accessibilityValue == nil })
    }
}
