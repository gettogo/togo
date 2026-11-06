import Testing
@testable import TogoTerminal

@Suite struct TerminalAccessibilityTests {
    @Test func stripsBoxDrawing() {
        let out = TerminalAccessibility.lines(screen: ["╭──────╮", "│ hello │", "▐█▌ done ░▒▓", "╰──────╯"], cursorRow: 0)
        #expect(out.map(\.text) == ["hello", "done"])
    }

    @Test func collapsesSpaces() {
        let out = TerminalAccessibility.lines(screen: ["  a   b \t c  "], cursorRow: 0)
        #expect(out.map(\.text) == ["a b c"])
    }

    @Test func dropsEmptyRows() {
        let out = TerminalAccessibility.lines(screen: ["one", "   ", "", "two"], cursorRow: 3)
        #expect(out == [
            AccessibleLine(row: 0, text: "one"),
            AccessibleLine(row: 3, text: "two", isCursor: true),
        ])
    }

    @Test func stripsSpinnerGlyphs() {
        let out = TerminalAccessibility.lines(screen: ["⠋ Thinking", "⠙⠹"], cursorRow: 0)
        #expect(out.map(\.text) == ["Thinking"])
    }

    @Test func cursorLineNamesItsText() {
        #expect(TerminalAccessibility.cursorLine("╰─ $  git   status", words: .english) == "Cursor line, $ git status")
    }

    @Test func cursorLineOnEmptyRow() {
        #expect(TerminalAccessibility.cursorLine("   ", words: .english) == "Cursor line, blank")
        #expect(TerminalAccessibility.cursorLine(nil, words: .english) == "Cursor line, blank")
    }

    @Test func cursorLineUsesTheAppsWords() {
        let words = AccessibilityWords(cursorLine: "Ligne du curseur", blank: "vide")
        #expect(TerminalAccessibility.cursorLine("", words: words) == "Ligne du curseur, vide")
    }

    @Test func tickKeyIgnoresCountersAndGlyphs() {
        let key = TerminalAccessibility.tickKey
        #expect(key("✻ Clauding… (12s · ↑ 1.2k tokens · esc to interrupt)")
            == key("✶ Clauding… (13s · ↓ 1.4k tokens · esc to interrupt)"))
        #expect(key("Working (12s • esc to interrupt)") == key("Working (1m 3s • esc to interrupt)"))
        #expect(key("Downloading 45%") == key("Downloading 46%"))
        #expect(key("Edited Foo.swift") != key("Edited Bar.swift"))
    }
}
