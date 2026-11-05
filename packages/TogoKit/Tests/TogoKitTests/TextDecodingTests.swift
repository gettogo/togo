import Testing
@testable import TogoKit

struct TextDecodingTests {
    private func read(_ content: String, encoding: FsReadResult.Encoding = .utf8) -> FsReadResult {
        FsReadResult(path: "f.txt", size: content.utf8.count, encoding: encoding, content: content, truncated: false)
    }

    @Test func crlfAndInvalidUTF8() throws {
        let crlf = try #require(TextDecoding.file(read("one\r\ntwo\r\n")))
        #expect(crlf.lines == ["one", "two"])
        #expect(crlf.lineEnding == .crlf)

        let mixed = try #require(TextDecoding.file(read("a\r\nb\nc\u{FFFD}\td")))
        #expect(mixed.lines == ["a", "b", "c\u{FFFD}\td"])
        #expect(mixed.lineEnding == .mixed)

        let lf = try #require(TextDecoding.file(read("x\ny")))
        #expect(lf.lineEnding == .lf)
        #expect(lf.lines == ["x", "y"])

        #expect(TextDecoding.file(read(""))?.lines == [""])
        #expect(TextDecoding.file(read("AAEC", encoding: .base64)) == nil)
        #expect(TextDecoding.file(read("x", encoding: .unknown("utf16"))) == nil)
    }

    @Test func nulContentIsBinary() {
        #expect(TextDecoding.file(read("PK\u{0}\u{3}rest")) == nil)
        let added = TextDecoding.addedFile(path: "blob", read("a\u{0}b"))
        #expect(added.isBinary && added.hunks.isEmpty)
    }

    @Test func longLinesClipped() throws {
        let text = try #require(TextDecoding.file(read("short\n" + String(repeating: "y", count: 5_000))))
        #expect(text.lines[1].utf16.count == ReviewLimits.maxColumns)
        #expect(text.clipped == [1])
    }

    @Test func untrackedFileAsAddedHunk() throws {
        let file = TextDecoding.addedFile(path: "notes/new file.md", read("# Hi\r\nbody\n"))
        #expect(file.change == .added)
        #expect(file.oldPath == nil)
        #expect(file.newPath == "notes/new file.md")
        #expect(file.additions == 2)
        let hunk = try #require(file.hunks.first)
        #expect(hunk.oldStart == 0 && hunk.oldCount == 0)
        #expect(hunk.newStart == 1 && hunk.newCount == 2)
        #expect(hunk.header == "@@ -0,0 +1,2 @@")
        #expect(hunk.lines.map(\.text) == ["# Hi", "body"])
        #expect(hunk.lines.map(\.newNumber) == [1, 2])
        #expect(hunk.lines.allSatisfy { $0.kind == .added })

        let noEOL = TextDecoding.addedFile(path: "a", read("x"))
        #expect(noEOL.hunks[0].lines.map(\.kind) == [.added, .noNewline])

        let empty = TextDecoding.addedFile(path: "e", read(""))
        #expect(empty.hunks.isEmpty && empty.change == .added)

        let binary = TextDecoding.addedFile(path: "b.png", read("iVBO", encoding: .base64))
        #expect(binary.isBinary && binary.hunks.isEmpty)
    }
}
