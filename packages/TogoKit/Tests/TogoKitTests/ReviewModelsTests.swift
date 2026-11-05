import Testing
@testable import TogoKit

struct ReviewModelsTests {
    private func line(_ text: String = "let x = 1") -> DiffLine {
        DiffLine(kind: .added, text: text, newNumber: 4, changes: [4..<5])
    }

    @Test func reviewModelsAreValueEqual() {
        let hunk = DiffHunk(
            header: "@@ -1,2 +1,3 @@ func f()", oldStart: 1, oldCount: 2, newStart: 1, newCount: 3,
            section: "func f()", lines: [line(), DiffLine(kind: .context, text: "}", oldNumber: 2, newNumber: 5)]
        )
        let file = DiffFile(oldPath: "a.swift", newPath: "a.swift", change: .modified, hunks: [hunk], additions: 1)
        let copy = DiffFile(oldPath: "a.swift", newPath: "a.swift", change: .modified, hunks: [hunk], additions: 1)
        #expect(file == copy)
        #expect(Set([file, copy]).count == 1)

        var edited = copy
        edited.hunks[0].lines[0].text = "let x = 2"
        #expect(edited != file)
        var clipped = line()
        clipped.clipped = true
        #expect(clipped != line())

        #expect(SplitRow(left: line(), right: nil) == SplitRow(left: line(), right: nil))
        #expect(SplitRow(hunkHeader: "@@") != SplitRow(left: line()))
        #expect(TokenSpan(range: 0..<3, kind: .keyword) == TokenSpan(range: 0..<3, kind: .keyword))
        #expect(TextFile(lines: ["a"]) == TextFile(lines: ["a"], lineEnding: .lf, clipped: []))
        #expect(DiffFile.Change.renamed(similarity: 90) != .renamed(similarity: nil))
    }

    @Test func fileIdentityPrefersNewPath() {
        #expect(DiffFile(oldPath: "a", newPath: "b", change: .renamed(similarity: 100)).id == "b")
        #expect(DiffFile(oldPath: "a", newPath: nil, change: .deleted).id == "a")
        #expect(DiffFile(oldPath: nil, newPath: nil, change: .modified).id == "")
    }

    @Test func limitsAndLanguages() {
        #expect(ReviewLimits.maxColumns == 2_000)
        #expect(ReviewLimits.wordDiffMaxLine == 500)
        #expect(ReviewLimits.tabWidth == 4)
        #expect(Language.allCases.count == 21)
        #expect(Language(rawValue: "swift") == .swift)
    }
}
