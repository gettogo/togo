import Foundation
import Testing
import UIKit
import UniformTypeIdentifiers
import TogoKit
@testable import Togo

/// Copying from the chat (owner: "I want to copy nicely formatted text and all kinds of
/// portions", "particular around links and multiline"): a reply goes as its markdown and as
/// rich text; code goes exactly; links keep their address.
@MainActor
struct ChatPasteboardTests {
    private let reply = """
    ## Fix

    Run **make test** with `--verbose`, then read [the guide](https://example.com/a_b?q=1&r=2#x).
    See https://github.com/org/repo/pull/12 too.
    - one
    - *two*

    ```sh
    \tgo test ./...\u{20}\u{20}
    echo "done"
    ```
    """

    @Test func plainTextIsTheMarkdownItself() {
        let item = ChatPasteboard.items(markdown: reply)
        #expect(item[UTType.utf8PlainText.identifier] as? String == reply)
        #expect(item[UTType.html.identifier] is Data)
        #expect(item[UTType.rtf.identifier] is Data)
    }

    @Test func htmlCarriesTheFormatting() {
        let html = RichMarkdown.html(reply)
        #expect(html.contains("<h3>Fix</h3>"))
        #expect(html.contains("<b>make test</b>"))
        #expect(html.contains("<code>--verbose</code>"))
        #expect(html.contains("<a href=\"https://example.com/a_b?q=1&amp;r=2#x\">the guide</a>"))
        #expect(html.contains("<a href=\"https://github.com/org/repo/pull/12\">https://github.com/org/repo/pull/12</a>"))
        #expect(html.contains("<i>two</i>"))
        #expect(html.contains("<pre><code>\tgo test ./...  \necho &quot;done&quot;</code></pre>"))
        #expect(!html.contains("**"))
        #expect(!html.contains("`"))
    }

    @Test func lineBreaksInAParagraphStayBreaks() {
        let html = RichMarkdown.html("first line\nsecond line")
        #expect(html.contains("<p>first line<br>second line</p>"))
        #expect(RichMarkdown.plain("first line\nsecond line") == "first line\nsecond line")
    }

    @Test func richTextReadsAsTheWordsWithoutMarks() {
        let text = RichMarkdown.attributed(reply, style: .pasteboard)
        #expect(text.string == RichMarkdown.plain(reply))
        #expect(!text.string.contains("**"))
        #expect(text.string.contains("Run make test with --verbose, then read the guide."))
        #expect(text.string.contains("\u{2022} one\n\u{2022} two"))
        #expect(text.string.hasSuffix("\tgo test ./...  \necho \"done\""))
    }

    @Test func richTextKeepsExactLinkAddresses() {
        let text = RichMarkdown.attributed(reply, style: .pasteboard)
        var links: [String: URL] = [:]
        text.enumerateAttribute(.link, in: NSRange(location: 0, length: text.length)) { value, range, _ in
            if let url = value as? URL { links[(text.string as NSString).substring(with: range)] = url }
        }
        #expect(links["the guide"]?.absoluteString == "https://example.com/a_b?q=1&r=2#x")
        #expect(links["https://github.com/org/repo/pull/12"]?.absoluteString == "https://github.com/org/repo/pull/12")
    }

    @Test func boldAndCodeAreRealFonts() {
        let text = RichMarkdown.attributed("plain **bold** `code`", style: .pasteboard)
        let ns = text.string as NSString
        let bold = text.attribute(.font, at: ns.range(of: "bold").location, effectiveRange: nil) as? UIFont
        let code = text.attribute(.font, at: ns.range(of: "code").location, effectiveRange: nil) as? UIFont
        #expect(bold?.fontDescriptor.symbolicTraits.contains(.traitBold) == true)
        #expect(code?.fontDescriptor.symbolicTraits.contains(.traitMonoSpace) == true)
    }

    @Test func theAppsOwnFileLinksStayWords() {
        let runs = RichMarkdown.runs("open [app.ts](togo-file://open?path=src/app.ts) and [x](shortcuts://run)")
        #expect(runs.allSatisfy { $0.link == nil })
        #expect(runs.map(\.text).joined() == "open app.ts and x")
    }

    @Test func htmlEscapesWhatWouldBeMarkup() {
        #expect(RichMarkdown.html("a < b && c > \"d\"").contains("<p>a &lt; b &amp;&amp; c &gt; &quot;d&quot;</p>"))
    }

    @Test func proseCharactersAreNotChanged() {
        let text = "It's -- \"quoted\" and 'single'\u{00A0}x"
        #expect(RichMarkdown.plain(text) == text)
    }

    @Test func codeCopiesExactlyWithNothingAdded() {
        let code = "\tif x {  \n\t\treturn 1\n}"
        let board = UIPasteboard.withUniqueName()
        ChatPasteboard.copy(exact: code, to: board)
        #expect(board.string == code)
        #expect(board.types == [UTType.utf8PlainText.identifier])
        UIPasteboard.remove(withName: board.name)
    }

    @Test func aReplyCopiesAsOneItemInEveryForm() throws {
        let board = UIPasteboard.withUniqueName()
        ChatPasteboard.copy(markdown: reply, to: board)
        #expect(board.numberOfItems == 1)
        #expect(board.string == reply)
        let html = try #require(board.data(forPasteboardType: UTType.html.identifier))
        #expect(String(decoding: html, as: UTF8.self).contains("<b>make test</b>"))
        #expect(board.data(forPasteboardType: UTType.rtf.identifier) != nil)
        UIPasteboard.remove(withName: board.name)
    }

    @Test func onlyRepliesAndThoughtsCopyAsMarkdown() {
        let reply = HarnessItem(id: "r", seq: 1, at: HarnessSamples.at, body: .assistant(.init(text: "**hi**", partial: false)))
        #expect(reply.copiesAsMarkdown)
        #expect(!HarnessSamples.queuedItem.copiesAsMarkdown)
    }
}
