import Testing
@testable import TogoKit

struct WordDiffTests {
    @Test func wordRangesOnRename() throws {
        let result = try #require(WordDiff.ranges(old: "let count = 1", new: "let total = 1"))
        #expect(result.old == [4..<9])
        #expect(result.new == [4..<9])
    }

    @Test func adjacentChangedTokensMerge() throws {
        let result = try #require(WordDiff.ranges(old: "call(a.b, c)", new: "call(x_y, c)"))
        #expect(result.old == [5..<8])
        #expect(result.new == [5..<8])

        let utf16 = try #require(WordDiff.ranges(old: "név = \"é😀\"", new: "név = \"ö😀\""))
        #expect(utf16.old == [7..<8])
        #expect(utf16.new == [7..<8])

        let same = try #require(WordDiff.ranges(old: "same", new: "same"))
        #expect(same.old.isEmpty && same.new.isEmpty)
    }

    @Test func wordDiffSkipsDissimilarPair() {
        #expect(WordDiff.ranges(old: "return parse(input)", new: "throw Failure.bad") == nil)
        #expect(WordDiff.ranges(old: "", new: "anything") == nil)
    }

    @Test func wordDiffSkipsLongLine() {
        let long = String(repeating: "a ", count: 251)
        #expect(long.utf16.count > ReviewLimits.wordDiffMaxLine)
        #expect(WordDiff.ranges(old: long, new: long + "b") == nil)
        #expect(WordDiff.ranges(old: "x", new: long) == nil)
    }

    @Test func unevenBlockPairsInOrder() {
        var hunk = DiffHunk(
            header: "@@ -1,3 +1,1 @@", oldStart: 1, oldCount: 3, newStart: 1, newCount: 1,
            lines: [
                DiffLine(kind: .removed, text: "let count = 1", oldNumber: 1),
                DiffLine(kind: .removed, text: "let other = 2", oldNumber: 2),
                DiffLine(kind: .removed, text: "let third = 3", oldNumber: 3),
                DiffLine(kind: .added, text: "let total = 1", newNumber: 1),
            ]
        )
        WordDiff.mark(&hunk)
        #expect(hunk.lines[0].changes == [4..<9])
        #expect(hunk.lines[3].changes == [4..<9])
        #expect(hunk.lines[1].changes.isEmpty)
        #expect(hunk.lines[2].changes.isEmpty)

        let file = DiffFile(oldPath: "a", newPath: "a", change: .modified, hunks: [hunk])
        let rows = SplitPairing.rows(file)
        #expect(rows.count == 4)
        #expect(rows[0].hunkHeader == "@@ -1,3 +1,1 @@")
        #expect(rows[1].left?.oldNumber == 1 && rows[1].right?.newNumber == 1)
        #expect(rows[2].left?.oldNumber == 2 && rows[2].right == nil)
        #expect(rows[3].left?.oldNumber == 3 && rows[3].right == nil)
    }

    @Test func splitRowsKeepContextAligned() {
        let text = """
        diff --git a/a b/a
        --- a/a
        +++ b/a
        @@ -1,4 +1,5 @@
         top
        -gone
        +new one
        +new two
         middle
        +inserted
         bottom
        \\ No newline at end of file

        """
        let file = DiffParser.parse(GitDiff(diff: text, truncated: false, binary: []))[0]
        let rows = SplitPairing.rows(file)
        #expect(rows.count == 8)
        #expect(rows[0].hunkHeader == "@@ -1,4 +1,5 @@")
        #expect(rows[1].left?.text == "top" && rows[1].right?.text == "top")
        #expect(rows[2].left?.text == "gone" && rows[2].right?.text == "new one")
        #expect(rows[3].left == nil && rows[3].right?.text == "new two")
        #expect(rows[4].left?.oldNumber == 3 && rows[4].right?.newNumber == 4)
        #expect(rows[5].left == nil && rows[5].right?.text == "inserted")
        #expect(rows[6].left?.text == "bottom" && rows[6].right?.text == "bottom")
        #expect(rows[7].left?.kind == .noNewline && rows[7].right?.kind == .noNewline)
    }
}
