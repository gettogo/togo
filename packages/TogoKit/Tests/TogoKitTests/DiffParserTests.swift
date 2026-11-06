import Foundation
import Testing
@testable import TogoKit

struct DiffParserTests {
    private func parse(_ text: String, truncated: Bool = false, binary: [String] = []) -> [DiffFile] {
        DiffParser.parse(GitDiff(diff: text, truncated: truncated, binary: binary))
    }

    @Test func parsesModifiedRenamedAddedDeleted() {
        let text = """
        diff --git a/Sources/App.swift b/Sources/App.swift
        index 3b18e51..a9c4d2f 100644
        --- a/Sources/App.swift
        +++ b/Sources/App.swift
        @@ -1,3 +1,3 @@ struct App {
         import Foundation
        -let count = 1
        +let total = 1
         print("done")
        diff --git a/old name.txt b/new name.txt
        similarity index 87%
        rename from old name.txt
        rename to new name.txt
        index 1111111..2222222 100644
        --- a/old name.txt\t
        +++ b/new name.txt\t
        @@ -1 +1 @@
        -alpha
        +beta
        diff --git a/README.md b/README.md
        new file mode 100644
        index 0000000..e69de29
        --- /dev/null
        +++ b/README.md
        @@ -0,0 +1,2 @@
        +# Title
        +Body
        diff --git a/gone.txt b/gone.txt
        deleted file mode 100644
        index e69de29..0000000
        --- a/gone.txt
        +++ /dev/null
        @@ -1 +0,0 @@
        -bye
        diff --git a/run.sh b/run.sh
        old mode 100644
        new mode 100755

        """
        let files = parse(text)
        #expect(files.count == 5)

        #expect(files[0].change == .modified)
        #expect(files[0].oldPath == "Sources/App.swift")
        #expect(files[0].newPath == "Sources/App.swift")
        #expect(files[0].additions == 1)
        #expect(files[0].deletions == 1)
        #expect(files[0].hunks.count == 1)
        #expect(files[0].hunks[0].section == "struct App {")
        #expect(files[0].hunks[0].lines.map(\.kind) == [.context, .removed, .added, .context])

        #expect(files[1].change == .renamed(similarity: 87))
        #expect(files[1].oldPath == "old name.txt")
        #expect(files[1].newPath == "new name.txt")

        #expect(files[2].change == .added)
        #expect(files[2].oldPath == nil)
        #expect(files[2].newPath == "README.md")
        #expect(files[2].additions == 2)

        #expect(files[3].change == .deleted)
        #expect(files[3].oldPath == "gone.txt")
        #expect(files[3].newPath == nil)
        #expect(files[3].deletions == 1)

        #expect(files[4].change == .modeOnly)
        #expect(files[4].newPath == "run.sh")
        #expect(files[4].hunks.isEmpty)
    }

    @Test func parsesQuotedPathsWithSpaces() {
        let text = """
        diff --git "a/tab\\there.txt" "b/tab\\there.txt"
        index 1111111..2222222 100644
        --- "a/tab\\there.txt"
        +++ "b/tab\\there.txt"
        @@ -1 +1 @@
        -x
        +y
        diff --git a/with space/caf\\303\\251.txt b/with space/caf\\303\\251.txt
        new file mode 100644
        --- /dev/null
        +++ "b/with space/caf\\303\\251 \\"q\\".txt"
        @@ -0,0 +1 @@
        +z
        diff --git a/dir with space/f b/dir with space/f
        old mode 100644
        new mode 100755

        """
        let files = parse(text)
        #expect(files.count == 3)
        #expect(files[0].oldPath == "tab\there.txt")
        #expect(files[0].newPath == "tab\there.txt")
        #expect(files[1].newPath == "with space/café \"q\".txt")
        #expect(files[2].oldPath == "dir with space/f")
        #expect(files[2].newPath == "dir with space/f")
    }

    @Test func binaryFromHeaderAndList() {
        let text = """
        diff --git a/logo.png b/logo.png
        index 1111111..2222222 100644
        Binary files a/logo.png and b/logo.png differ
        diff --git a/data.bin b/data.bin
        index 1111111..2222222 100644
        --- a/data.bin
        +++ b/data.bin
        @@ -1 +1 @@
        -a
        +b

        """
        let files = parse(text, binary: ["data.bin", "font.otf"])
        #expect(files.count == 3)
        #expect(files[0].isBinary)
        #expect(files[0].change == .modified)
        #expect(files[0].hunks.isEmpty)
        #expect(files[1].isBinary)
        #expect(files[2].newPath == "font.otf")
        #expect(files[2].isBinary)
    }

    @Test func noNewlineMarker() {
        let text = """
        diff --git a/a.txt b/a.txt
        --- a/a.txt
        +++ b/a.txt
        @@ -1,2 +1,2 @@
         one
        -two
        \\ No newline at end of file
        +two
        +
        """
        let lines = parse(text)[0].hunks[0].lines
        #expect(lines.map(\.kind) == [.context, .removed, .noNewline, .added])
        #expect(lines[2].oldNumber == nil && lines[2].newNumber == nil)
        #expect(lines[2].text == "No newline at end of file")
        #expect(lines[3].newNumber == 2)
    }

    @Test func lineNumbersAcrossHunks() {
        let text = """
        diff --git a/a.txt b/a.txt
        --- a/a.txt
        +++ b/a.txt
        @@ -1,2 +1,3 @@
         a
        +b
         c
        @@ -10,3 +11,2 @@ func tail()
         x
        -y
         z

        """
        let hunks = parse(text)[0].hunks
        #expect(hunks.count == 2)
        #expect(hunks[0].lines.map(\.oldNumber) == [1, nil, 2])
        #expect(hunks[0].lines.map(\.newNumber) == [1, 2, 3])
        #expect(hunks[1].oldStart == 10 && hunks[1].oldCount == 3)
        #expect(hunks[1].newStart == 11 && hunks[1].newCount == 2)
        #expect(hunks[1].section == "func tail()")
        #expect(hunks[1].lines.map(\.oldNumber) == [10, 11, 12])
        #expect(hunks[1].lines.map(\.newNumber) == [11, nil, 12])
    }

    @Test func truncatedMidHunkParses() {
        let text = """
        diff --git a/a.txt b/a.txt
        --- a/a.txt
        +++ b/a.txt
        @@ -1,50 +1,50 @@
         one
        -two
        +tw
        """
        let files = parse(text, truncated: true)
        #expect(files.count == 1)
        #expect(files[0].hunks[0].lines.map(\.text) == ["one", "two", "tw"])
        #expect(files[0].hunks[0].oldCount == 50)
        let withNewline = parse(text + "\n", truncated: true)
        #expect(withNewline[0].hunks[0].lines.map(\.text) == ["one", "two", "tw"])

        // Every prefix of a diff parses without trapping.
        let full = Array(Self.sample.utf8)
        for cut in stride(from: 0, through: full.count, by: 3) {
            _ = parse(String(decoding: full[..<cut], as: UTF8.self), truncated: true)
        }
    }

    @Test func longLineClipped() throws {
        let long = String(repeating: "x", count: 5_000)
        let text = "diff --git a/m.js b/m.js\n--- a/m.js\n+++ b/m.js\n@@ -0,0 +1 @@\n+\(long)\n"
        let line = try #require(parse(text).first?.hunks.first?.lines.first)
        #expect(line.text.utf16.count == 2_000)
        #expect(line.clipped)

        // A surrogate pair straddling the limit is not split.
        let emoji = String(repeating: "a", count: 1_999) + "😀😀"
        let other = "diff --git a/e b/e\n--- a/e\n+++ b/e\n@@ -0,0 +1 @@\n+\(emoji)\n"
        let cut = try #require(parse(other).first?.hunks.first?.lines.first)
        #expect(cut.text.utf16.count == 1_999)
        #expect(cut.clipped)
    }

    @Test func crlfAndInvalidUTF8() throws {
        let text = "diff --git a/w.txt b/w.txt\r\n--- a/w.txt\r\n+++ b/w.txt\r\n@@ -1 +1 @@\r\n-caf\u{FFFD}\r\n+café\r\n"
        let lines = try #require(parse(text).first?.hunks.first?.lines)
        #expect(lines.map(\.text) == ["caf\u{FFFD}", "café"])
        #expect(parse(text).first?.newPath == "w.txt")
    }

    @Test func lineEndingOnlyChangeMarked() throws {
        let text = "diff --git a/w b/w\n--- a/w\n+++ b/w\n@@ -1,2 +1,2 @@\n-same\r\n-let count = 1\r\n+same\n+let total = 1\r\n"
        let lines = try #require(parse(text).first?.hunks.first?.lines)
        #expect(lines.map(\.endsWithCR) == [true, true, false, true])
        #expect(lines[0].lineEndingChanged && lines[2].lineEndingChanged)
        #expect(lines[0].changes.isEmpty && lines[2].changes.isEmpty)
        #expect(!lines[1].lineEndingChanged && !lines[3].lineEndingChanged)
        #expect(lines[1].changes == [4..<9] && lines[3].changes == [4..<9])

        let unchanged = "diff --git a/w b/w\n--- a/w\n+++ b/w\n@@ -1 +1 @@\n-a\r\n+b\r\n"
        let pair = try #require(parse(unchanged).first?.hunks.first?.lines)
        #expect(pair.allSatisfy { $0.endsWithCR && !$0.lineEndingChanged })
    }

    /// Guards against super-linear parsing, not against a slow machine: the 10,000-line diff is
    /// timed against a quarter-size one in the same test (linear work is 4x, quadratic 16x), and
    /// the absolute ceiling is only there to catch a hang.
    @Test func parses10kLineDiffQuickly() {
        func makeDiff(blocks: Int) -> GitDiff {
            var text = "diff --git a/big.swift b/big.swift\n--- a/big.swift\n+++ b/big.swift\n"
            for block in 0..<blocks {
                text += "@@ -\(block * 60 + 1),55 +\(block * 60 + 1),55 @@ func block\(block)()\n"
                for row in 0..<50 { text += "-    let value\(row) = compute(\(block), \(row)) // old note\n" }
                for row in 0..<50 { text += "+    let value\(row) = compute(\(block), \(row) + 1) // new note\n" }
                for row in 0..<5 { text += "     context line \(row)\n" }
            }
            return GitDiff(diff: text, truncated: false, binary: [])
        }
        // Best of several interleaved runs, so a burst of load from other suites hits both sizes.
        let clock = ContinuousClock()
        let smallDiff = makeDiff(blocks: 25), bigDiff = makeDiff(blocks: 100)
        var files: [DiffFile] = []
        var quarter = Duration.seconds(600), elapsed = Duration.seconds(600)
        for _ in 0..<5 {
            quarter = min(quarter, clock.measure { _ = DiffParser.parse(smallDiff) })
            elapsed = min(elapsed, clock.measure { files = DiffParser.parse(bigDiff) })
        }
        #expect(files[0].additions == 5_000 && files[0].deletions == 5_000)
        #expect(files[0].hunks.allSatisfy { $0.lines.contains { !$0.changes.isEmpty } })
        #expect(elapsed < quarter * 8 + .milliseconds(300), "10,000 changed lines took \(elapsed), a quarter of them \(quarter): not linear")
        #expect(elapsed < .seconds(30))
    }

    @Test func hostileInputNeverTraps() {
        var rng = SplitMix(seed: 0x7060_2026)
        let alphabet = Array("diff --git a/ b/\n@@ -+ ,0123456789\\\"\t\r rename from to similarity index%Binary files differ\u{FFFD}é😀".utf8)
        let clock = ContinuousClock()
        let elapsed = clock.measure {
            for _ in 0..<300 {
                let count = Int(rng.next() % 2_000)
                var bytes: [UInt8] = []
                bytes.reserveCapacity(count)
                for _ in 0..<count {
                    bytes.append(rng.next() % 4 == 0 ? UInt8(truncatingIfNeeded: rng.next()) : alphabet[Int(rng.next() % UInt64(alphabet.count))])
                }
                let files = parse(String(decoding: bytes, as: UTF8.self), truncated: true, binary: ["x"])
                for file in files { _ = SplitPairing.rows(file) }
            }
            for header in Self.hostileHeaders {
                let files = parse("diff --git a/x b/x\n--- a/x\n+++ b/x\n\(header)\n+a\n-b\n x\n\\ x\n")
                for file in files { _ = SplitPairing.rows(file) }
            }
        }
        #expect(elapsed < .seconds(5))
    }

    private static let hostileHeaders = [
        "@@ -99999999999999999999999,99999999999999999999 +9223372036854775807,9223372036854775807 @@",
        "@@ -1,-5 +1,0 @@", "@@ - + @@", "@@", "@@@ -1 +1 @@@", "@@ -0,0 +0,0 @@",
        "@@ -1,1 +1,1", "\"", "diff --git \"a/\\", "diff --git \"a/\\777\\0\" b",
        "--- \"", "+++ \"b/\\x", "rename from \"\\", "similarity index 99999999999999999999999%",
    ]

    static let sample = """
    diff --git a/a b/a
    --- a/a
    +++ b/a
    @@ -1,3 +1,3 @@ section
     keep
    -let count = 1
    +let total = 1
    \\ No newline at end of file
    diff --git "a/q\\"x" "b/q\\"x"
    similarity index 90%
    rename from "q\\"x"
    rename to "q\\"y"

    """
}

/// Deterministic generator so failures reproduce.
struct SplitMix {
    private var state: UInt64
    init(seed: UInt64) { state = seed }
    mutating func next() -> UInt64 {
        state &+= 0x9E37_79B9_7F4A_7C15
        var z = state
        z = (z ^ (z >> 30)) &* 0xBF58_476D_1CE4_E5B9
        z = (z ^ (z >> 27)) &* 0x94D0_49BB_1331_11EB
        return z ^ (z >> 31)
    }
}
