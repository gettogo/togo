import Foundation
import Testing
import UIKit
@testable import TogoTerminal

/// A paste reaches the pty with nothing added: every byte out is a byte the user copied, apart
/// from line endings made LF and invisible web characters made plain.
@MainActor
@Suite struct PasteTextTests {
    private let esc = "\u{1b}"

    @Test(arguments: [
        "https://example.com/a_b?q=1&r=%20x#frag",
        "https://github.com/org/repo/pull/123/files#diff-abc123",
        "git commit -m \"fix: it's done\" && make test",
        "for f in *.swift; do\n\techo \"$f\"\ndone",
        "SELECT *\nFROM t\nWHERE a = 'x';\n",
        "naïve 日本語 \u{1F600} مرحبا \u{1F468}\u{200D}\u{1F469}\u{200D}\u{1F467}",
        "“curly” — kept as copied",
    ])
    func textThatNeedsNoCleaningPassesUnchanged(_ text: String) {
        #expect(PasteText.terminal(text) == text)
        #expect(PasteText.payload(text, bracketed: false) == Array(text.utf8))
    }

    @Test func lineEndingsBecomeOneLineFeedEach() {
        #expect(PasteText.terminal("a\r\nb\r\n") == "a\nb\n")
        #expect(PasteText.terminal("a\rb\r") == "a\nb\n")
        #expect(PasteText.terminal("a\r\n\r\nb") == "a\n\nb")
        #expect(PasteText.terminal("a\n\rb") == "a\n\nb")
    }

    @Test func noTrailingNewlineIsAddedOrRemoved() {
        #expect(PasteText.terminal("ls -la") == "ls -la")
        #expect(PasteText.terminal("ls -la\n") == "ls -la\n")
    }

    @Test func webSpacesAndInvisiblesAreMadePlain() {
        #expect(PasteText.terminal("ls\u{00A0}-la") == "ls -la")
        #expect(PasteText.terminal("a\u{202F}b\u{2007}c") == "a b c")
        #expect(PasteText.terminal("\u{FEFF}npm\u{200B} i\u{2060}nstall\u{00AD}") == "npm install")
    }

    @Test func tabsStayTabs() {
        #expect(PasteText.terminal("a\tb\t\tc") == "a\tb\t\tc")
    }

    @Test func bracketedPayloadWrapsTheCleanBody() {
        let bytes = PasteText.payload("a\r\nb\(esc)[201~", bracketed: true)
        #expect(String(decoding: bytes, as: UTF8.self) == "\(esc)[200~a\nb[201~\(esc)[201~")
    }

    @Test func emptyPasteSendsNothing() {
        #expect(PasteText.payload("", bracketed: true).isEmpty)
        #expect(PasteText.payload("\(esc)\u{200B}", bracketed: true).isEmpty)
    }

    @Test func chunksKeepEveryByteInOrderAndNeverSplitACharacter() {
        let text = String(repeating: "héllo 日本 \u{1F600}\n", count: 900)
        let bytes = Array(text.utf8)
        let pieces = PasteText.chunks(bytes, size: 4096)
        #expect(pieces.count > 1)
        #expect(pieces.allSatisfy { $0.count <= 4096 })
        #expect(Array(pieces.joined()) == bytes)
        for piece in pieces {
            #expect(String(validating: piece, as: UTF8.self) != nil)
        }
    }

    @Test func smallPasteIsOneChunk() {
        #expect(PasteText.chunks(Array("hi".utf8)).count == 1)
        #expect(PasteText.chunks([]).isEmpty)
    }

    @Test func largePasteGoesOutInBoundedOrderedWrites() async {
        let h = EngineHarness()
        h.feed("\(esc)[?2004h")
        let line = "echo 'Ünïcode line'\r\n"
        let text = String(repeating: line, count: 2000)
        h.engine.paste(text)
        let expected = Data(PasteText.payload(text, bracketed: true))
        #expect(await waitUntil { h.joinedInput.count >= expected.count })
        await settle()
        #expect(h.input.count > 1)
        #expect(h.input.allSatisfy { $0.count <= PasteText.chunkSize })
        #expect(h.joinedInput == expected)
        #expect(h.joinedInput.starts(with: Data("\(esc)[200~".utf8)))
        #expect(!h.joinedInput.contains(0x0D))
    }

    @Test func linkOnlyPasteboardPastesTheExactLink() async {
        let h = EngineHarness()
        let board = UIPasteboard.withUniqueName()
        let url = URL(string: "https://example.com/path?a=1&b=two#x")!
        board.url = url
        h.engine.pasteFromClipboard(board)
        #expect(await waitUntil { !h.input.isEmpty })
        #expect(h.joinedInput == Data(url.absoluteString.utf8))
        UIPasteboard.remove(withName: board.name)
    }
}
