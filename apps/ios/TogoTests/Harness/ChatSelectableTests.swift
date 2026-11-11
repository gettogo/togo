import Foundation
import SwiftUI
import Testing
import UIKit
import UniformTypeIdentifiers
import TogoKit
@testable import Togo

/// Selecting part of a reply, an output or a capture (owner: "text selection within a large
/// output … tapping and holding shouldn't just auto select the entire input/output"): the
/// selectable text holds exactly the words the chat shows, styled as before, and a selected
/// part copies as itself.
@MainActor
struct ChatSelectableTests {
    private let traits = UITraitCollection(preferredContentSizeCategory: .large)

    @Test func proseAndHeadingsShareATextUntilACodeBlock() {
        let blocks = MarkdownBlocks.parse("""
        ## Fix

        First paragraph.

        Second paragraph.

        ```sh
        make test
        ```

        After the code.
        """).blocks
        let segments = ChatSelectable.segments(blocks)
        #expect(segments.count == 3)
        guard case .text(let first) = segments[0], case .code(let code) = segments[1], case .text(let last) = segments[2] else {
            Issue.record("unexpected segments \(segments)")
            return
        }
        #expect(first.first == .heading("Fix"))
        let words = ChatSelectable.text(first, traits: traits).string
        #expect(words.hasPrefix("Fix\nFirst paragraph."))
        #expect(words.hasSuffix("Second paragraph."))
        #expect(code == "make test")
        #expect(last == [.prose("After the code.")])
    }

    @Test func blocksJoinOnALineBreakWithTheChatsSpacing() {
        let blocks: [MarkdownBlocks.Block] = [.heading("Fix"), .prose("one\ntwo"), .prose("three")]
        let text = ChatSelectable.text(blocks, traits: traits)
        #expect(text.string == "Fix\none\ntwo\nthree")
        func spacing(at index: Int) -> CGFloat {
            (text.attribute(.paragraphStyle, at: index, effectiveRange: nil) as? NSParagraphStyle)?.paragraphSpacing ?? -1
        }
        let ns = text.string as NSString
        // The heading's line and "two" end their blocks; "one" is inside one; the last has none.
        #expect(spacing(at: ns.range(of: "Fix").location) == ChatSelectable.blockSpacing)
        #expect(spacing(at: ns.range(of: "one").location) == 0)
        #expect(spacing(at: ns.range(of: "two").location) == ChatSelectable.blockSpacing)
        #expect(spacing(at: ns.range(of: "three").location) == 0)
    }

    @Test func inlineMarkdownBecomesFontsAndLinksWithTheSameWords() {
        let source = "Run **make** with `--verbose`, *see* [the guide](https://example.com/x) and ~~not~~ src/app.ts."
        let attributed = ChatLinks.markdown(source)
        let text = ChatSelectable.attributed(attributed, style: .prose, traits: traits)
        #expect(text.string == String(attributed.characters))
        let ns = text.string as NSString
        func font(_ word: String) -> UIFont? {
            text.attribute(.font, at: ns.range(of: word).location, effectiveRange: nil) as? UIFont
        }
        #expect(font("make")?.fontDescriptor.symbolicTraits.contains(.traitBold) == true)
        #expect(font("see")?.fontDescriptor.symbolicTraits.contains(.traitItalic) == true)
        #expect(font("--verbose")?.fontDescriptor.symbolicTraits.contains(.traitMonoSpace) == true)
        #expect(font("Run")?.pointSize == UIFont.preferredFont(forTextStyle: .body, compatibleWith: traits).pointSize)
        #expect(text.attribute(.strikethroughStyle, at: ns.range(of: "not").location, effectiveRange: nil) != nil)
        #expect(text.attribute(.link, at: ns.range(of: "the guide").location, effectiveRange: nil) as? URL
            == URL(string: "https://example.com/x"))
        // A file reference stays a link inside the app, as the chat had it.
        let file = text.attribute(.link, at: ns.range(of: "src/app.ts").location, effectiveRange: nil) as? URL
        #expect(file?.scheme == ChatLinks.fileScheme)
        #expect(text.attribute(.foregroundColor, at: 0, effectiveRange: nil) as? UIColor == .togoSnow)
    }

    @Test func codeKeepsEveryCharacterAndNeverWraps() {
        let code = "\tgo test ./...  \n    echo \"done\"\r\n"
        let text = ChatSelectable.attributed(ChatLinks.plain(code), style: .code, traits: traits)
        #expect(text.string == code)
        let paragraph = text.attribute(.paragraphStyle, at: 0, effectiveRange: nil) as? NSParagraphStyle
        #expect(paragraph?.lineBreakMode == .byClipping)
        let font = text.attribute(.font, at: 0, effectiveRange: nil) as? UIFont
        #expect(font?.fontDescriptor.symbolicTraits.contains(.traitMonoSpace) == true)
    }

    @Test func dynamicTypeSetsTheSize() {
        let large = UITraitCollection(preferredContentSizeCategory: DynamicTypeSize.accessibility3.contentSizeCategory)
        let body = ChatSelectable.font(.prose, traits: traits)
        let bigger = ChatSelectable.font(.prose, traits: large)
        #expect(bigger.pointSize > body.pointSize)
        #expect(DynamicTypeSize.xSmall.contentSizeCategory == .extraSmall)
        #expect(DynamicTypeSize.accessibility5.contentSizeCategory == .accessibilityExtraExtraExtraLarge)
    }

    @Test func aListItemHangsUnderItsWords() {
        let item = ChatSelectable.item(marker: "1.", AttributedString("first"), style: .prompt, traits: traits)
        #expect(item.string == "1. first")
        let paragraph = item.attribute(.paragraphStyle, at: 0, effectiveRange: nil) as? NSParagraphStyle
        #expect((paragraph?.headIndent ?? 0) > 0)
        #expect(item.attribute(.foregroundColor, at: 0, effectiveRange: nil) as? UIColor == .togoMute)
    }

    // MARK: Copying a selection

    @Test func aSelectedPartCopiesItsWordsAndOnlyLinksThatLeaveTheApp() throws {
        let attributed = ChatLinks.markdown("See **the guide** at https://example.com/a and `web/LICENSE:1` now")
        let text = ChatSelectable.attributed(attributed, style: .prose, traits: traits)
        let ns = text.string as NSString
        let from = ns.range(of: "the guide").location
        let to = NSMaxRange(ns.range(of: "LICENSE:1"))
        let selection = text.attributedSubstring(from: NSRange(location: from, length: to - from))
        let item = ChatPasteboard.items(selection: selection)
        #expect(item[UTType.utf8PlainText.identifier] as? String == "the guide at https://example.com/a and web/LICENSE:1")
        let rtf = try #require(item[UTType.rtf.identifier] as? Data)
        let pasted = try NSAttributedString(data: rtf, options: [.documentType: NSAttributedString.DocumentType.rtf], documentAttributes: nil)
        var links: [String] = []
        pasted.enumerateAttribute(.link, in: NSRange(location: 0, length: pasted.length)) { value, _, _ in
            if let url = value as? URL { links.append(url.absoluteString) } else if let string = value as? String { links.append(string) }
        }
        #expect(links == ["https://example.com/a"])
    }

    @Test func aSelectionsColourStaysInTheApp() {
        let text = ChatSelectable.attributed(AttributedString("words"), style: .prose, traits: traits)
        let item = ChatPasteboard.items(selection: text)
        guard let rtf = item[UTType.rtf.identifier] as? Data,
              let pasted = try? NSAttributedString(data: rtf, options: [.documentType: NSAttributedString.DocumentType.rtf], documentAttributes: nil)
        else {
            Issue.record("no RTF")
            return
        }
        let colour = pasted.attribute(.foregroundColor, at: 0, effectiveRange: nil) as? UIColor
        #expect(colour == nil || colour == .black)
    }

    // MARK: Capture

    @Test func theCapturesMarkedLineIsFoundInUTF16() {
        let capture = CaptureText(lines: ["$ ls 📁", "error: missing", "done"], marked: 1)
        let range = capture.markedRange
        #expect(range.map { (capture.string as NSString).substring(with: $0) } == "error: missing")
        let text = capture.attributed(traits: traits)
        #expect(text.string == capture.string)
        #expect(text.attribute(.backgroundColor, at: range?.location ?? 0, effectiveRange: nil) != nil)
        #expect(text.attribute(.backgroundColor, at: 0, effectiveRange: nil) == nil)
        #expect(CaptureText(lines: ["one"], marked: 4).markedRange == nil)
        #expect(CaptureText(lines: ["one"], marked: nil).markedRange == nil)
    }
}
