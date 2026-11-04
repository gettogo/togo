import Foundation
import Testing
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

    @Test func insertTextEmitsInput() async {
        let h = EngineHarness()
        h.engine.textInputView.insertText("a")
        #expect(await waitUntil { !h.input.isEmpty })
        #expect(h.joinedInput == Data("a".utf8))
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
        #expect(h.joinedInput == Data("\(esc)[200~xrm -rf ~\r\(esc)[201~".utf8))
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
}
