import Foundation
import Testing
@testable import TogoTerminal

private let e = "\u{1b}"

struct KeyEncoderTests {
    struct Case: Sendable, CustomTestStringConvertible {
        let stroke: KeyStroke
        let applicationCursor: Bool
        let expected: String
        var testDescription: String { "\(stroke) app=\(applicationCursor)" }
    }

    @Test(arguments: [
        Case(stroke: KeyStroke(.up), applicationCursor: false, expected: "\(e)[A"),
        Case(stroke: KeyStroke(.up), applicationCursor: true, expected: "\(e)OA"),
        Case(stroke: KeyStroke(.char("c"), .ctrl), applicationCursor: false, expected: "\u{03}"),
        Case(stroke: KeyStroke(.char("x"), .alt), applicationCursor: false, expected: "\(e)x"),
        Case(stroke: KeyStroke(.tab, .shift), applicationCursor: false, expected: "\(e)[Z"),
        Case(stroke: KeyStroke(.up, .ctrl), applicationCursor: false, expected: "\(e)[1;5A"),
        Case(stroke: KeyStroke(.up, .ctrl), applicationCursor: true, expected: "\(e)[1;5A"),
        Case(stroke: KeyStroke(.function(1)), applicationCursor: false, expected: "\(e)OP"),
        Case(stroke: KeyStroke(.function(5)), applicationCursor: false, expected: "\(e)[15~"),
        Case(stroke: KeyStroke(.function(12), [.ctrl, .shift]), applicationCursor: false, expected: "\(e)[24;6~"),
        Case(stroke: KeyStroke(.enter), applicationCursor: false, expected: "\r"),
        Case(stroke: KeyStroke(.backspace), applicationCursor: false, expected: "\u{7f}"),
        Case(stroke: KeyStroke(.escape), applicationCursor: false, expected: e),
        Case(stroke: KeyStroke(.delete), applicationCursor: false, expected: "\(e)[3~"),
        Case(stroke: KeyStroke(.home), applicationCursor: true, expected: "\(e)OH"),
        Case(stroke: KeyStroke(.pageDown), applicationCursor: false, expected: "\(e)[6~"),
        Case(stroke: KeyStroke(.char("é")), applicationCursor: false, expected: "é"),
        Case(stroke: KeyStroke(.char("a"), [.ctrl, .alt]), applicationCursor: false, expected: "\(e)\u{01}"),
        Case(stroke: KeyStroke(.tab, [.alt, .shift]), applicationCursor: false, expected: "\(e)\(e)[Z"),
        Case(stroke: KeyStroke(.char("é"), .ctrl), applicationCursor: false, expected: ""),
        Case(stroke: KeyStroke(.char("!"), .ctrl), applicationCursor: false, expected: ""),
        Case(stroke: KeyStroke(.char("!"), [.ctrl, .alt]), applicationCursor: false, expected: ""),
        Case(stroke: KeyStroke(.char("1"), .shift), applicationCursor: false, expected: "1"),
        Case(stroke: KeyStroke(.function(20)), applicationCursor: false, expected: "\(e)[34~"),
    ])
    func keyEncoding(_ c: Case) {
        let bytes = KeyEncoder.encode(c.stroke, applicationCursor: c.applicationCursor)
        #expect(bytes == Array(c.expected.utf8))
    }

    @Test func keyStrokeCodableRoundTrip() throws {
        let strokes = [
            KeyStroke(.char("é")),
            KeyStroke(.function(12), [.ctrl, .shift]),
            KeyStroke(.escape),
        ]
        let data = try JSONEncoder().encode(strokes)
        let decoded = try JSONDecoder().decode([KeyStroke].self, from: data)
        #expect(decoded == strokes)

        let first = try JSONSerialization.jsonObject(with: JSONEncoder().encode(strokes[0])) as? [String: Any]
        #expect(first?["key"] as? String == "é")
    }

    @Test(arguments: [0, -1, 21])
    func functionKeysOutsideF1ToF20AreRejectedBothWays(_ n: Int) {
        #expect(throws: EncodingError.self) { try JSONEncoder().encode(KeyStroke(.function(n))) }
        #expect(throws: DecodingError.self) {
            try JSONDecoder().decode(KeyStroke.self, from: Data(#"{"key":"f\#(n)"}"#.utf8))
        }
    }

    @Test func f20RoundTrips() throws {
        let stroke = KeyStroke(.function(20), .alt)
        #expect(try JSONDecoder().decode(KeyStroke.self, from: JSONEncoder().encode(stroke)) == stroke)
    }

    @Test func keyStrokeDecodesWithoutModifiers() throws {
        let stroke = try JSONDecoder().decode(KeyStroke.self, from: Data(#"{"key":"tab"}"#.utf8))
        #expect(stroke == KeyStroke(.tab))
    }
}
