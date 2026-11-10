import Foundation
import Testing
@testable import TogoTerminal

struct KeySequenceTests {
    static let valid: [(String, [KeyStep])] = [
        ("Esc Esc", [.stroke(KeyStroke(.escape)), .stroke(KeyStroke(.escape))]),
        ("C-c", [.stroke(KeyStroke(.char("c"), .ctrl))]),
        ("C-M-x", [.stroke(KeyStroke(.char("x"), [.ctrl, .alt]))]),
        ("M-C-x", [.stroke(KeyStroke(.char("x"), [.ctrl, .alt]))]),
        ("S-Tab", [.stroke(KeyStroke(.tab, .shift))]),
        ("F12", [.stroke(KeyStroke(.function(12)))]),
        ("C--", [.stroke(KeyStroke(.char("-"), .ctrl))]),
        ("PgUp BS Del Ins", [.stroke(KeyStroke(.pageUp)), .stroke(KeyStroke(.backspace)),
                             .stroke(KeyStroke(.delete)), .stroke(KeyStroke(.insert))]),
        (#"text:"say \"hi\"""#, [.text(#"say "hi""#)]),
        (#"text:"a\\b c" Enter"#, [.text(#"a\b c"#), .stroke(KeyStroke(.enter))]),
        ("delay:50", [.delay(milliseconds: 50)]),
        ("Esc delay:2000 Esc", [.stroke(KeyStroke(.escape)), .delay(milliseconds: 2000), .stroke(KeyStroke(.escape))]),
    ]

    @Test(arguments: valid)
    func parseTable(_ text: String, _ expected: [KeyStep]) throws {
        #expect(try KeySequence.parse(text) == expected)
    }

    static let invalid: [(String, Int)] = [
        ("delay:0", 0), ("delay:2001", 0), ("C-", 0), ("Esc C-", 4), ("F13", 0),
        ("C-C-x", 0), ("Esc  Esc", 4), ("Esc ", 4), (#"text:"open"#, 0), (#"text:"\n""#, 6),
        (#"text:"a"b"#, 8), ("Escape", 0),
    ]

    @Test(arguments: invalid)
    func parseRejects(_ text: String, _ position: Int) {
        #expect {
            try KeySequence.parse(text)
        } throws: { error in
            (error as? KeySequenceError)?.position == position
        }
    }

    @Test func formatRoundTrips() throws {
        let presets = [KeyBarConfig.standard, .claude, .omp]
        for item in presets.flatMap(\.items) {
            guard case .steps(let steps) = item.action else { continue }
            #expect(!steps.isEmpty, "preset \(item.title) failed to parse")
            #expect(try KeySequence.parse(KeySequence.format(steps)) == steps)
        }
        let mixed: [KeyStep] = [
            .stroke(KeyStroke(.char("C"), [.ctrl, .alt, .shift])), .text(#"q"\"#), .delay(milliseconds: 1),
            .stroke(KeyStroke(.function(1))), .stroke(KeyStroke(.pageDown, .alt)),
        ]
        #expect(KeySequence.format(mixed) == #"C-M-S-C text:"q\"\\" delay:1 F1 M-PgDn"#)
        #expect(try KeySequence.parse(KeySequence.format(mixed)) == mixed)
    }

    @Test func configCodableRoundTrip() throws {
        for preset in [KeyBarConfig.standard, .claude, .omp] {
            let data = try JSONEncoder().encode(preset)
            #expect(try JSONDecoder().decode(KeyBarConfig.self, from: data) == preset)
        }
        let sparse = try JSONDecoder().decode(KeyBarConfig.self, from: Data("{}".utf8))
        #expect(sparse.items.isEmpty)
    }

    /// A bar synced from a newer build may hold a key this build does not know: that key is
    /// dropped and the rest of the bar stays.
    @Test func unknownKeyActionIsDropped() throws {
        let json = #"{"items":[{"title":"Hide","action":{"hideKeyboard":{}}},{"title":"Beam","action":{"teleport":{}}},"#
            + #"{"title":"Paste","action":{"paste":{}}}]}"#
        let config = try JSONDecoder().decode(KeyBarConfig.self, from: Data(json.utf8))
        #expect(config.items.map(\.title) == ["Hide", "Paste"])
    }

    /// The Compose key opened a composer the terminal no longer has. A bar saved with it comes
    /// back with Attach in its place, under the same id, so a customised bar keeps a key for
    /// pictures and nothing else moves.
    @Test func legacyComposeKeyComesBackAsAttach() throws {
        let id = UUID()
        let json = #"{"items":[{"id":"\#(id.uuidString)","title":"Compose","symbol":"square.and.pencil","action":{"compose":{}}},"#
            + #"{"title":"Paste","action":{"paste":{}}}]}"#
        let config = try JSONDecoder().decode(KeyBarConfig.self, from: Data(json.utf8))
        let attach = try #require(config.items.first)
        #expect(attach.id == id)
        #expect(attach.action == .attach)
        #expect(attach.title == KeyBarConfig.attach.title)
        #expect(attach.symbol == KeyBarConfig.attach.symbol)
        #expect(config.items.map(\.title) == ["Attach image", "Paste"])
        // Saved again, it stays Attach.
        let again = try JSONDecoder().decode(KeyBarConfig.self, from: JSONEncoder().encode(config))
        #expect(again == config)
    }

    /// No preset offers the old Compose key.
    @Test func presetsOfferAttachNotCompose() {
        for preset in [KeyBarConfig.standard, .claude, .omp] {
            #expect(preset.items.contains { $0.action == .attach })
            #expect(!preset.items.contains { $0.action == .compose })
        }
    }
}
