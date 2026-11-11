import Testing
@testable import TogoKit

/// Tables, task lists and nested lists as agents write them (owner: a reply's table showed as
/// raw pipes while the agent's terminal drew a table).
struct MarkdownTableTests {
    /// Claude Code's status table, as it sent it.
    private let reply = """
    Here's where things stand:

    | Component | Status | Notes |
    |-----------|:------:|------:|
    | Diff Viewer | ✅ Ready | uses `DiffModel` |
    | **Chat** tables | 🚧 In progress | needs `a \\| b` |
    | Relay | ❌ Blocked |

    Next I'll wire the rest.
    """

    @Test func aTableIsItsOwnBlockBetweenProse() throws {
        let blocks = MarkdownBlocks.parse(reply).blocks
        #expect(blocks.count == 3)
        #expect(blocks.first == .prose("Here's where things stand:"))
        #expect(blocks.last == .prose("Next I'll wire the rest."))
        guard case .table(let table) = blocks[1] else {
            Issue.record("no table in \(blocks)")
            return
        }
        #expect(table.header == ["Component", "Status", "Notes"])
        #expect(table.alignments == [.leading, .center, .trailing])
        #expect(table.rows.count == 3)
        #expect(table.rows[0] == ["Diff Viewer", "✅ Ready", "uses `DiffModel`"])
        // Inline formatting stays for the view; an escaped pipe is a pipe in the cell.
        #expect(table.rows[1] == ["**Chat** tables", "🚧 In progress", "needs `a | b`"])
        // A short row is filled out to the header's width.
        #expect(table.rows[2] == ["Relay", "❌ Blocked", ""])
    }

    @Test func aPipeInsideCodeSplitsNothing() {
        #expect(MarkdownTable.cells("| `a|b` | c |") == ["`a|b`", "c"])
        #expect(MarkdownTable.cells("no pipes here") == nil)
        #expect(MarkdownTable.alignments("|---|---|") == [.leading, .leading])
        #expect(MarkdownTable.alignments("| - | not |") == nil)
        #expect(MarkdownTable.alignments("---") == nil)
    }

    @Test func pipesWithoutADelimiterRowStayProse() {
        let blocks = MarkdownBlocks.parse("a | b\nc | d")
        #expect(blocks.blocks == [.prose("a | b\nc | d")])
    }

    @Test func aStreamedTableBecomesATableOnceItsDelimiterArrives() {
        let first = MarkdownBlocks.parse("Intro\n\n| A | B |")
        #expect(first.blocks == [.prose("Intro\n\n| A | B |")])
        let next = first.continued(to: "Intro\n\n| A | B |\n|---|---|\n| 1 | 2 |")
        #expect(next.blocks == MarkdownBlocks.parse(next.text).blocks)
        #expect(next.blocks.last == .table(MarkdownTable(header: ["A", "B"], alignments: [.leading, .leading], rows: [["1", "2"]])))
    }

    @Test func aTableEndsAtABlankLineOrAFence() {
        let blocks = MarkdownBlocks.parse("| A |\n|---|\n| 1 |\n```\ncode\n```")
        #expect(blocks.blocks.count == 2)
        #expect(blocks.blocks.last == .code(language: nil, text: "code"))
    }

    @Test func aTableCopiesAsMarkdownTabsAndSpokenRows() throws {
        guard case .table(let table) = MarkdownBlocks.parse(reply).blocks[1] else { return }
        #expect(table.markdown.hasPrefix("| Component | Status | Notes |\n| --- | :---: | ---: |\n| Diff Viewer |"))
        #expect(table.markdown.contains("needs `a \\| b`"))
        #expect(table.tabSeparated().split(separator: "\n").first == "Component\tStatus\tNotes")
        #expect(table.spokenRows().first == "Component: Diff Viewer, Status: ✅ Ready, Notes: uses `DiffModel`")
        #expect(table.spokenRows().last == "Component: Relay, Status: ❌ Blocked")
    }

    // MARK: Lists

    @Test func taskListsShowTheirBoxes() {
        let blocks = MarkdownBlocks.parse("- [ ] write tests\n- [x] ship it\n* [X] done too\n1. [ ] numbered task")
        #expect(blocks.blocks == [.prose("\u{2610} write tests\n\u{2611} ship it\n\u{2611} done too\n1. \u{2610} numbered task")])
    }

    @Test func nestedAndOrderedListsKeepTheirShape() {
        let blocks = MarkdownBlocks.parse("3. third\n4. fourth\n   - nested\n     + deeper\n- [link](https://x.y)")
        #expect(blocks.blocks == [.prose("3. third\n4. fourth\n   \u{2022} nested\n     \u{2022} deeper\n\u{2022} [link](https://x.y)")])
    }

    @Test func strikethroughStaysInlineMarkdown() {
        #expect(MarkdownBlocks.parse("~~old~~ new").blocks == [.prose("~~old~~ new")])
    }

    // MARK: The rest of the audit

    @Test func quotesReadAsBarsPerLevel() {
        #expect(MarkdownBlocks.parse("> said\n> > nested").blocks == [.prose("\u{2503} said\n\u{2503} \u{2503} nested")])
    }

    @Test func aRuleReadsAsALineAndATitleOverOneIsAHeading() {
        #expect(MarkdownBlocks.parse("one\n\n---\n\ntwo").blocks == [.prose("one\n\n" + MarkdownBlocks.rule + "\n\ntwo")])
        #expect(MarkdownBlocks.parse("Title\n=====\nbody").blocks == [.heading("Title"), .prose("body")])
        #expect(MarkdownBlocks.parse("Sub\n---").blocks == [.heading("Sub")])
        // A list item over dashes stays a list item.
        #expect(MarkdownBlocks.parse("- item\n\n***").blocks.count == 1)
    }

    @Test func tildeFencesAreCodeAndOnlyTheirOwnFenceCloses() {
        let blocks = MarkdownBlocks.parse("~~~python\nprint(1)\n```\n~~~\nafter").blocks
        #expect(blocks == [.code(language: "python", text: "print(1)\n```"), .prose("after")])
    }

    @Test func imagesBecomeLinksAndFootnotesReadAsNumbers() {
        #expect(MarkdownBlocks.parse("see ![diagram](https://x.y/d.png) and ![](https://x.y/e.png)").blocks
            == [.prose("see [Image: diagram](https://x.y/d.png) and [Image](https://x.y/e.png)")])
        #expect(MarkdownBlocks.parse("true[^1]\n\n[^1]: the source").blocks == [.prose("true[1]\n\n[1]: the source")])
        // Inside code nothing changes.
        #expect(MarkdownBlocks.parse("`![x](y)` and `[^2]`").blocks == [.prose("`![x](y)` and `[^2]`")])
    }
}
