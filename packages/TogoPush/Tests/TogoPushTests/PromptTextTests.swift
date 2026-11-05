import Foundation
import Testing
import TogoPush

struct PromptTextTests {
    struct Case: Decodable {
        var name: String
        var `in`: String
        var want: String
    }

    struct Table: Decodable { var cases: [Case] }

    @Test func stripsBoxDrawing() {
        #expect(PromptText.clean("│ rm -rf ./build │") == "rm -rf ./build")
        let boxed = """
        ╭──────────────────────────╮
        │ Bash command             │
        │   npm test               │
        ╰──────────────────────────╯
        """
        #expect(PromptText.clean(boxed) == "Bash command\nnpm test")
        #expect(PromptText.clean("▐▛███▜▌ Claude Code ▝▜█████▛▘") == "Claude Code")
    }

    @Test func stripsMenuCursor() {
        #expect(PromptText.clean("❯ 1. Yes") == "1. Yes")
        #expect(PromptText.clean("› 2. No") == "2. No")
        #expect(PromptText.clean("▸ Allow once") == "Allow once")
        #expect(PromptText.clean("> Deny") == "Deny")
        #expect(PromptText.clean("│ ❯ 1. Yes │") == "1. Yes")
        #expect(PromptText.clean("a > b") == "a > b", "only a leading cursor is removed")
    }

    @Test func collapsesAlignment() {
        #expect(PromptText.clean("Allow    Bash\t\tnpm   test   ") == "Allow Bash npm test")
        #expect(PromptText.clean("  one\n\n\n   two  ") == "one\ntwo")
        #expect(PromptText.clean("a\r\nb") == "a\nb")
    }

    @Test func capsLines() {
        #expect(PromptText.clean("1\n2\n3\n4\n5") == "1\n2\n3")
        #expect(PromptText.clean("1\n\n2\n\n3") == "1\n2\n3", "blank lines do not count")
        #expect(PromptText.clean("1\n2", maxLines: 1) == "1")
        #expect(PromptText.clean("╭──╮\n│  │\n╰──╯") == "")
    }

    @Test func keepsNonASCIIPath() {
        #expect(PromptText.clean("~/Проекты/app") == "~/Проекты/app")
        #expect(PromptText.clean("café/日本語 ✓") == "café/日本語 ✓")
    }

    @Test func controlCharactersBecomeSpaces() {
        #expect(PromptText.clean("a\u{07}b\u{85}c") == "a b c")
        #expect(PromptText.clean("\u{1B}[31mrm -rf x\u{1B}[0m") == "rm -rf x")
    }

    /// U+202E makes "rm -rf ~/x<RLO>gnp.txt" display as "rm -rf ~/xtxt.png": the user would
    /// approve a different path from the one that runs.
    @Test func rloCannotReverseACommand() {
        #expect(PromptText.clean("rm -rf ~/x\u{202E}gnp.txt") == "rm -rf ~/xgnp.txt")
        #expect(PromptText.clean("npm\u{200B} \u{200C}test\u{200D}\u{2060}\u{FEFF}") == "npm test")
        #expect(PromptText.clean("\u{2066}ls\u{2069} \u{200E}-la\u{200F} \u{61C}x") == "ls -la x")
    }

    @Test func subjectRefusesInvisibleCharacters() {
        for text in ["rm -rf ~/x\u{202E}gnp.txt", "npm\u{200B}test", "\u{2066}ls\u{2069}", "\u{FEFF}ls", "a\u{61C}b"] {
            let json = Data(#"{"kind":"command","text":"\#(text)"}"#.utf8)
            #expect(throws: DecodingError.self) { try JSONDecoder().decode(PushSubject.self, from: json) }
            #expect(!PushSubject(kind: "command", text: text).isValid)
        }
        #expect(PushSubject(kind: "command", text: "npm test").isValid)
    }

    /// The same table drives the helper's CleanLine test.
    @Test func cleanTableMatchesGo() throws {
        var url = URL(filePath: #filePath)
        for _ in 0..<5 { url.deleteLastPathComponent() }
        url.append(path: "protocol/fixtures/render/clean.json")
        let table = try JSONDecoder().decode(Table.self, from: Data(contentsOf: url))
        #expect(table.cases.count >= 20)
        for c in table.cases {
            #expect(PromptText.clean(c.in) == c.want, "\(c.name)")
        }
    }
}
