import Foundation
import Testing
@testable import TogoKit

@Suite(.serialized)
struct TokenizerTests {
    private func spans(_ text: String, _ language: Language) -> [TokenSpan] {
        var state = Tokenizer.State()
        return Tokenizer.line(text, language: language, state: &state)
    }

    private func span(_ range: Range<Int>, _ kind: TokenKind) -> TokenSpan {
        TokenSpan(range: range, kind: kind)
    }

    @Test func detectsByExtensionAndShebang() {
        #expect(Language.detect(path: "Makefile", firstLine: nil) == .shell)
        #expect(Language.detect(path: "src/x.tsx", firstLine: nil) == .typescript)
        #expect(Language.detect(path: "tool", firstLine: "#!/usr/bin/env python3") == .python)
        #expect(Language.detect(path: "run", firstLine: "#!/bin/bash") == .shell)
        #expect(Language.detect(path: "run", firstLine: "#!/usr/bin/env -S node --flag") == .javascript)
        #expect(Language.detect(path: "README", firstLine: nil) == .plain)
        #expect(Language.detect(path: "README", firstLine: "hello") == .plain)
        #expect(Language.detect(path: "a/b/Main.SWIFT", firstLine: nil) == .swift)
        #expect(Language.detect(path: ".gitignore", firstLine: nil) == .plain)
    }

    @Test func swiftKeywordsStringsComments() {
        let result = spans(#"let s = "a\"b" // c"#, .swift)
        #expect(result.contains(span(0..<3, .keyword)))
        #expect(result.contains(span(8..<14, .string)))
        #expect(result.contains(span(15..<19, .comment)))
        #expect(result.contains(span(6..<7, .punctuation)))
    }

    @Test func blockCommentCarriesAcrossLines() {
        var state = Tokenizer.State()
        let first = Tokenizer.line("a /* start", language: .go, state: &state)
        let middle = Tokenizer.line("still inside", language: .go, state: &state)
        let last = Tokenizer.line("end */ x := 1", language: .go, state: &state)
        #expect(first == [span(2..<10, .comment)])
        #expect(middle == [span(0..<12, .comment)])
        #expect(last.first == span(0..<6, .comment))
        #expect(last.contains(span(12..<13, .number)))
        #expect(state == Tokenizer.State())
    }

    @Test func pythonTripleQuoteCarries() {
        let result = Tokenizer.tokenize(["x = \"\"\"doc", "more 'text'", "end\"\"\" + y", "z = 1"], language: .python)
        #expect(result[0].contains(span(4..<10, .string)))
        #expect(result[1] == [span(0..<11, .string)])
        #expect(result[2].first == span(0..<6, .string))
        #expect(result[3].contains(span(4..<5, .number)))
    }

    @Test func goRawStringCarries() {
        let result = Tokenizer.tokenize(["s := `a\\", "b` + \"c\""], language: .go)
        #expect(result[0].contains(span(5..<8, .string)))
        #expect(result[1].first == span(0..<2, .string))
        #expect(result[1].contains(span(5..<8, .string)))
    }

    @Test func jsonKeysAreAttributes() {
        let result = spans(#"  "name": "togo", "n": 12, "ok": true"#, .json)
        #expect(result.contains(span(2..<8, .attribute)))
        #expect(result.contains(span(10..<16, .string)))
        #expect(result.contains(span(18..<21, .attribute)))
        #expect(result.contains(span(23..<25, .number)))
        #expect(result.contains(span(33..<37, .keyword)))
    }

    @Test func numbersHexAndExponent() {
        let result = spans("a = 0xFF_ee + 0b1010 + 1_000 + 1.5e-3 + 2E+8", .python)
        let numbers = result.filter { $0.kind == .number }.map(\.range)
        #expect(numbers == [4..<11, 14..<20, 23..<28, 31..<37, 40..<44])
    }

    @Test func unterminatedStringStopsAtLineEnd() {
        var state = Tokenizer.State()
        let first = Tokenizer.line("x = \"oops", language: .swift, state: &state)
        #expect(first.contains(span(4..<9, .string)))
        #expect(state == Tokenizer.State())
        let second = Tokenizer.line("let y = 1", language: .swift, state: &state)
        #expect(second.contains(span(0..<3, .keyword)))
        #expect(!second.contains { $0.kind == .string })
    }

    @Test func utf16RangesWithEmojiAndCJK() {
        let result = spans("let s = \"😀漢字\" // x", .swift)
        #expect(result.contains(span(8..<14, .string)))
        #expect(result.contains(span(15..<19, .comment)))
        let cjk = spans("漢字 = 1", .python)
        #expect(cjk.contains(span(5..<6, .number)))
    }

    @Test func attributesTypesAndDirectives() {
        let swift = spans("@MainActor final class Foo {}", .swift)
        #expect(swift.contains(span(0..<10, .attribute)))
        #expect(swift.contains(span(23..<26, .type)))
        #expect(spans("#include <stdio.h>", .c).first == span(0..<8, .attribute))
        #expect(spans("@app.route", .python).first == span(0..<4, .attribute))
    }

    @Test func markdownHeadingsAndCodeSpans() {
        let result = Tokenizer.tokenize(["# Title", "use `x` and ``y`z``", "```swift", "let a = 1", "```", "#nope"], language: .markdown)
        #expect(result[0] == [span(0..<7, .keyword)])
        #expect(result[1] == [span(4..<7, .string), span(12..<19, .string)])
        #expect(result[2] == [span(0..<8, .string)])
        #expect(result[3] == [span(0..<9, .string)])
        #expect(result[4] == [span(0..<3, .string)])
        #expect(result[5].isEmpty)
    }

    @Test func plainHasNoSpans() {
        #expect(Tokenizer.tokenize(["let x = 1", "y"], language: .plain) == [[], []])
    }

    @Test func longLinesAreScannedOnlyToTheLimit() {
        let line = String(repeating: "1 ", count: 5_000)
        let result = spans(line, .swift)
        #expect(result.count == ReviewLimits.maxColumns / 2)
        #expect((result.last?.range.upperBound ?? 0) <= ReviewLimits.maxColumns)
    }

    @Test func commentOpenedBeforeTheCapAndClosedAfterItDoesNotLeak() {
        let long = "x /* " + String(repeating: "a", count: 2_100) + " */ y"
        var state = Tokenizer.State()
        let result = Tokenizer.line(long, language: .go, state: &state)
        #expect(state == Tokenizer.State())
        #expect(result.contains { $0.kind == .comment })
        #expect((result.last?.range.upperBound ?? 0) <= ReviewLimits.maxColumns)
        #expect(Tokenizer.line("z := 1", language: .go, state: &state).contains(span(5..<6, .number)))
    }

    @Test func commentOpenedAfterTheCapCarriesToTheNextLine() {
        let long = String(repeating: "a ", count: 1_100) + "/* open"
        var state = Tokenizer.State()
        _ = Tokenizer.line(long, language: .go, state: &state)
        #expect(state != Tokenizer.State())
        #expect(Tokenizer.line("still comment", language: .go, state: &state) == [span(0..<13, .comment)])
    }

    @Test func capDoesNotSplitSurrogatePairs() {
        let line = String(repeating: "A", count: ReviewLimits.maxColumns - 1) + "😀😀"
        let result = spans(line, .swift)
        #expect(result.last?.range.upperBound == ReviewLimits.maxColumns - 1)
    }

    @Test func rustLifetimesAreNotStrings() {
        let result = spans("fn f<'a>(x: &'a str) -> char { 'z' }", .rust)
        #expect(result.contains(span(0..<2, .keyword)))
        #expect(result.filter { $0.kind == .string } == [span(31..<34, .string)])
        #expect(spans("let c = '\\n';", .rust).contains(span(8..<12, .string)))
    }

    @Test func everyLanguageHasRules() {
        for language in Language.allCases {
            let result = spans("let x = \"a\" // c 12 @Foo #if", language)
            for item in result {
                #expect(item.range.lowerBound >= 0 && item.range.upperBound <= 28)
            }
            _ = LanguageRules.rules(for: language)
        }
    }

    @Test func hostileInputNeverTrapsAndSpansStayOrdered() {
        let alphabet = Array("\"'`\\/*#@<>-!:{}[]()$ \t.0xe+-_AzéΩ😀漢\r\n=,;|&%^~?".map(String.init))
        var seed: UInt64 = 0x9E3779B97F4A7C15
        func next() -> Int {
            seed = seed &* 6364136223846793005 &+ 1442695040888963407
            return Int(seed >> 33)
        }
        for language in Language.allCases {
            var state = Tokenizer.State()
            for _ in 0..<300 {
                let text = (0..<(next() % 80)).map { _ in alphabet[next() % alphabet.count] }.joined()
                let result = Tokenizer.line(text, language: language, state: &state)
                var previousEnd = 0
                for item in result {
                    #expect(item.range.lowerBound >= previousEnd)
                    #expect(item.range.upperBound > item.range.lowerBound)
                    #expect(item.range.upperBound <= text.utf16.count)
                    previousEnd = item.range.upperBound
                }
            }
        }
    }

    @Test func hugeAdversarialLinesFinishQuickly() {
        let clock = ContinuousClock()
        let samples = [
            String(repeating: "\\", count: 250_000),
            String(repeating: "/*", count: 125_000),
            String(repeating: "\"'`", count: 85_000),
            String(repeating: "` `` ``` ", count: 25_000),
            String(repeating: "0e+", count: 85_000),
        ]
        let elapsed = clock.measure {
            for language in Language.allCases {
                for sample in samples {
                    var state = Tokenizer.State()
                    _ = Tokenizer.line(sample, language: language, state: &state)
                }
            }
        }
        #expect(elapsed < .seconds(5))
    }

    @Test func tokenizes10kLinesQuickly() {
        let sample = [
            "    func load(_ id: Int, name: String) async throws -> [Item] { // fetch",
            "    let url = \"https://example.com/\\(id)\" + \"/items?limit=0x10\"",
            "    /* note */ if let value = cache[id], value.count > 1_000 { return value }",
            "    @discardableResult private static var shared: Store = Store()",
        ]
        let lines = (0..<10_000).map { sample[$0 % sample.count] }
        let clock = ContinuousClock()
        var result: [[TokenSpan]] = []
        let elapsed = clock.measure { result = Tokenizer.tokenize(lines, language: .swift) }
        #expect(result.count == 10_000)
        #expect(elapsed < .milliseconds(250))
    }
}
