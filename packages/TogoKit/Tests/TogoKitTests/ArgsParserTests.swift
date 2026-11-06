import Foundation
import Testing
import TogoKit

@Suite struct ArgsParserTests {
    @Test func splitsQuotedWords() throws {
        let args = try ArgsParser.split(#"--model "opus 4" -p 'a b'"#)
        #expect(args == ["--model", "opus 4", "-p", "a b"])
    }

    @Test func neverExpands() throws {
        #expect(try ArgsParser.split("$HOME ~ * `x`") == ["$HOME", "~", "*", "`x`"])
        #expect(try ArgsParser.split(#""$HOME ~ * `x` $(id)""#) == ["$HOME ~ * `x` $(id)"])
        #expect(try ArgsParser.split("'$HOME' ~/src ${PATH}") == ["$HOME", "~/src", "${PATH}"])
        #expect(try ArgsParser.split("a;b | c && d > e") == ["a;b", "|", "c", "&&", "d", ">", "e"])
    }

    @Test func backslashRules() throws {
        #expect(try ArgsParser.split(#"a\ b c"#) == ["a b", "c"])
        // Inside double quotes a backslash escapes only \ " $ and `; elsewhere it stays.
        #expect(try ArgsParser.split(#""a\"b\\c\$d\`e\nf""#) == [#"a"b\c$d`e\nf"#])
        #expect(try ArgsParser.split(#"'a\b'"#) == [#"a\b"#])
        #expect(try ArgsParser.split(#"x"y"'z'w"#) == ["xyzw"])
        #expect(try ArgsParser.split(#"'' """#) == ["", ""])
        #expect(try ArgsParser.split("   ") == [])
    }

    @Test func yoloFlagsRoundTripUnchanged() throws {
        for flag in ["--dangerously-skip-permissions", "--yolo"] {
            #expect(try ArgsParser.split(flag) == [flag])
            #expect(ArgsParser.join([flag]) == flag)
            #expect(try ArgsParser.split(ArgsParser.join([flag])) == [flag])
        }
        #expect(ArgsParser.join(["--dangerously-skip-permissions", "--model", "opus"])
            == "--dangerously-skip-permissions --model opus")
    }

    @Test func unbalancedQuoteThrows() {
        #expect(throws: ArgsError.unbalancedQuote) { try ArgsParser.split(#"--model "opus"#) }
        #expect(throws: ArgsError.unbalancedQuote) { try ArgsParser.split("-p 'a b") }
        #expect(throws: ArgsError.unbalancedQuote) { try ArgsParser.split(#"a\"#) }
        #expect(throws: ArgsError.unbalancedQuote) { try ArgsParser.split(#""a\""#) }
    }

    @Test func controlCharacterThrows() {
        #expect(throws: ArgsError.controlCharacter) { try ArgsParser.split("a\nb") }
        #expect(throws: ArgsError.controlCharacter) { try ArgsParser.split("a\tb") }
        #expect(throws: ArgsError.controlCharacter) { try ArgsParser.split("'a\u{0}b'") }
        #expect(throws: ArgsError.controlCharacter) { try ArgsParser.split("\"a\u{1b}[31m\"") }
        #expect(throws: ArgsError.controlCharacter) { try ArgsParser.split("a\u{85}") }
    }

    @Test func tooLongThrows() {
        #expect(throws: ArgsError.tooLong) { try ArgsParser.split(String(repeating: "a ", count: 65)) }
        #expect(throws: ArgsError.tooLong) { try ArgsParser.split(String(repeating: "é", count: 2049)) }
        let total = Array(repeating: String(repeating: "b", count: 4000), count: 5).joined(separator: " ")
        #expect(throws: ArgsError.tooLong) { try ArgsParser.split(total) }
        #expect((try? ArgsParser.split(String(repeating: "a ", count: 64)))?.count == 64)
    }

    @Test func joinQuotesOnlyWhatNeedsIt() {
        #expect(ArgsParser.join(["--model", "opus 4", "it's", "", "$HOME"]) == #"--model 'opus 4' 'it'\''s' '' '$HOME'"#)
    }

    @Test func joinRoundTrips() throws {
        var rng = SeededGenerator(seed: 0x5eed)
        let alphabet = Array(#"abcXYZ019-_=.,:/@%+ '"\$`~*?!;&|<>(){}[]#^é漢🙂"#) + Self.unusual
        for _ in 0..<200 {
            let count = Int.random(in: 0...6, using: &rng)
            let argv: [String] = (0..<count).map { _ in
                let length = Int.random(in: 0...12, using: &rng)
                return String((0..<length).map { _ in alphabet.randomElement(using: &rng)! })
            }
            let line = ArgsParser.join(argv)
            #expect(try ArgsParser.split(line) == argv, "line: \(line)")
        }
    }
}

extension ArgsParserTests {
    /// No-break space, line separator, a combining accent and a right-to-left override: none
    /// is a control character, so each is an ordinary word character, as it is for the helper.
    static let unusual: [Character] = ["\u{A0}", "\u{2028}", "e\u{301}", "\u{202E}"]

    /// Every line `split` accepts, including ones with double quotes and bare backslashes,
    /// comes back to the same argv after a trip through `join`.
    @Test func splitJoinSplitIsStable() throws {
        var rng = SeededGenerator(seed: 0xb2)
        let alphabet = Array(#"ab -'"\$`~*;"#) + Self.unusual
        var accepted = 0
        for _ in 0..<2_000 {
            let length = Int.random(in: 0...16, using: &rng)
            let line = String((0..<length).map { _ in alphabet.randomElement(using: &rng)! })
            guard let argv = try? ArgsParser.split(line) else { continue }
            accepted += 1
            #expect(try ArgsParser.split(ArgsParser.join(argv)) == argv, "line: \(line)")
        }
        #expect(accepted > 200)
    }

    @Test func unusualCharactersAreWordCharacters() throws {
        #expect(try ArgsParser.split("a\u{A0}b c\u{202E}d") == ["a\u{A0}b", "c\u{202E}d"])
        #expect(try ArgsParser.split("a\u{2028}b") == ["a\u{2028}b"])
    }
}

/// A small deterministic generator so a failure reproduces.
struct SeededGenerator: RandomNumberGenerator {
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
