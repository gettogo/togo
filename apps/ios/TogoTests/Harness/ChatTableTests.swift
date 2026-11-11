import Foundation
import Testing
import UIKit
import UniformTypeIdentifiers
import TogoKit
@testable import Togo

/// A reply's table renders as a table block, copies as a table and reads by row; list items
/// hang under their words.
@MainActor
struct ChatTableTests {
    private let reply = """
    | Component | Status |
    |---|:---:|
    | Diff **Viewer** | `ready` |
    | Chat | [docs](https://example.com/d) |
    """
    private let traits = UITraitCollection(preferredContentSizeCategory: .large)

    private var table: MarkdownTable? {
        guard case .table(let table) = ChatSelectable.segments(ChatMarkdown.blocks(reply)).first else { return nil }
        return table
    }

    @Test func aTableIsItsOwnSegment() throws {
        let table = try #require(table)
        #expect(table.header == ["Component", "Status"])
        #expect(table.alignments == [.leading, .center])
    }

    @Test func copyTablePastesAsMarkdownTabsAndHTML() throws {
        let item = ChatPasteboard.items(table: try #require(table))
        #expect((item[UTType.utf8PlainText.identifier] as? String)?.hasPrefix("| Component | Status |") == true)
        #expect(item[UTType.utf8TabSeparatedText.identifier] as? String == "Component\tStatus\nDiff Viewer\tready\nChat\tdocs")
        let html = String(data: try #require(item[UTType.html.identifier] as? Data), encoding: .utf8) ?? ""
        #expect(html.contains("<th>Component</th><th style=\"text-align:center\">Status</th>"))
        #expect(html.contains("<td>Diff <b>Viewer</b></td>"))
        #expect(html.contains("<a href=\"https://example.com/d\">docs</a>"))
    }

    @Test func voiceOverReadsARowAtATime() {
        let spoken = RichMarkdown.spoken("Status:\n\n" + reply)
        #expect(spoken.contains("Component: Diff Viewer, Status: ready"))
        #expect(spoken.contains("Component: Chat, Status: docs"))
        #expect(!spoken.contains("|"))
    }

    @Test func aCellKeepsItsColumnsAlignment() {
        let header = ChatSelectable.attributed(AttributedString("Status"), style: .cell(.center, header: true), traits: traits)
        let paragraph = header.attribute(.paragraphStyle, at: 0, effectiveRange: nil) as? NSParagraphStyle
        #expect(paragraph?.alignment == .center)
        let trailing = ChatSelectable.attributed(AttributedString("9"), style: .cell(.trailing, header: false), traits: traits)
        #expect((trailing.attribute(.paragraphStyle, at: 0, effectiveRange: nil) as? NSParagraphStyle)?.alignment == .right)
    }

    @Test func listItemsHangUnderTheirWords() {
        let blocks = MarkdownBlocks.parse("- one\n  - nested item\n- [ ] task\n2. second\nplain").blocks
        let text = ChatSelectable.text(blocks, traits: traits)
        let ns = text.string as NSString
        func indent(_ word: String) -> CGFloat {
            (text.attribute(.paragraphStyle, at: ns.range(of: word).location, effectiveRange: nil) as? NSParagraphStyle)?.headIndent ?? -1
        }
        #expect(indent("one") > 0)
        #expect(indent("nested") > indent("one"))
        #expect(indent("task") > 0)
        #expect(indent("second") > 0)
        #expect(indent("plain") == 0)
    }
}
