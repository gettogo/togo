import Foundation
import Testing
@testable import TogoTerminal

@MainActor
@Suite struct ExtendedKeysTests {
    func sent(_ h: EngineHarness, _ stroke: KeyStroke) async -> String {
        h.input.removeAll()
        h.engine.send(stroke)
        _ = await waitUntil { !h.input.isEmpty }
        return String(decoding: h.joinedInput, as: UTF8.self)
    }

    @Test func shiftEnterModifyOtherKeys() async {
        let h = EngineHarness()
        h.feed("\(esc)[>4;2m")
        #expect(await sent(h, KeyStroke(.enter, .shift)) == "\(esc)[27;2;13~")
        #expect(await sent(h, KeyStroke(.enter)) == "\r")
    }

    @Test func shiftEnterKittyFlags() async {
        let h = EngineHarness()
        h.feed("\(esc)[>1u")
        _ = await waitUntil { h.engine.extendedKeyMode == .kitty }
        #expect(await sent(h, KeyStroke(.enter, .shift)) == "\(esc)[13;2u")
        #expect(await sent(h, KeyStroke(.char("c"), .ctrl)) == "\(esc)[99;5u")
        #expect(await sent(h, KeyStroke(.escape)) == "\(esc)[27u")
    }

    @Test func shiftEnterIsMetaEnterWithoutExtendedKeys() async {
        let h = EngineHarness()
        #expect(await sent(h, KeyStroke(.enter, .shift)) == "\(esc)\r")
    }

    @Test func modifyOtherKeysSurvivesSplitFeeds() async {
        let h = EngineHarness()
        h.feed("\(esc)[>")
        h.feed("4;")
        h.feed("2m")
        #expect(await sent(h, KeyStroke(.enter, .shift)) == "\(esc)[27;2;13~")
    }

    @Test func modifyOtherKeysResetRestoresLegacyKeys() async {
        let h = EngineHarness()
        h.feed("\(esc)[>4;2m")
        h.feed("\(esc)[>4m")
        #expect(await sent(h, KeyStroke(.enter, .shift)) == "\(esc)\r")
        h.feed("\(esc)[>4;2m\(esc)c")
        #expect(await sent(h, KeyStroke(.enter, .shift)) == "\(esc)\r")
    }

    @Test func kittyPopRestoresLegacyKeys() async {
        let h = EngineHarness()
        h.feed("\(esc)[>1u")
        _ = await waitUntil { h.engine.extendedKeyMode == .kitty }
        h.feed("\(esc)[<u")
        #expect(await waitUntil { h.engine.extendedKeyMode == .legacy })
        #expect(await sent(h, KeyStroke(.enter, .shift)) == "\(esc)\r")
    }

    @Test func shiftedPrintableStaysText() {
        let stroke = KeyStroke(.char("a"), .shift)
        #expect(KeyEncoder.encode(stroke, applicationCursor: false, mode: .modifyOtherKeys) == Array("A".utf8))
        #expect(KeyEncoder.encode(stroke, applicationCursor: false, mode: .kitty) == Array("A".utf8))
    }

    @Test func hardwareShiftEnterUsesModifyOtherKeys() {
        let bytes = KeyEncoder.extendedHardware(
            charactersIgnoringModifiers: "\r", modifiers: .shift, keyCode: 0x28, optionAsMeta: true,
            mode: .modifyOtherKeys)
        #expect(bytes == Array("\(esc)[27;2;13~".utf8))
        #expect(KeyEncoder.extendedHardware(
            charactersIgnoringModifiers: "\r", modifiers: .shift, keyCode: 0x28, optionAsMeta: true,
            mode: .legacy) == nil)
    }

    @Test(arguments: [("@", "q"), ("{", "8"), ("|", "7"), ("£", "3"), ("å", "a"), ("", "e")])
    func optionTextCompositionIsLeftToUIKit(characters: String, base: String) {
        #expect(KeyEncoder.extendedHardware(
            characters: characters, charactersIgnoringModifiers: base, modifiers: .alternate,
            keyCode: 4, optionAsMeta: false, mode: .modifyOtherKeys) == nil)
        #expect(KeyEncoder.hardware(characters: characters, charactersIgnoringModifiers: base,
                                    modifiers: .alternate, optionAsMeta: false) == nil)
    }

    @Test(arguments: [("@", "q", 64), (":", ";", 58), ("å", "a", 97)])
    func extendedMetaUsesTheSameCharacterAsLegacy(characters: String, base: String, code: Int) {
        #expect(KeyEncoder.extendedHardware(
            characters: characters, charactersIgnoringModifiers: base, modifiers: .alternate,
            keyCode: 4, optionAsMeta: true, mode: .modifyOtherKeys)
            == Array("\(esc)[27;3;\(code)~".utf8))
        #expect(KeyEncoder.hardware(characters: characters, charactersIgnoringModifiers: base,
                                    modifiers: .alternate, optionAsMeta: true) == [0x1b, UInt8(code)])
    }

    @Test(arguments: [false, true])
    func controlOptionKeepsControlWithEitherPolicy(meta: Bool) {
        #expect(KeyEncoder.extendedHardware(
            characters: "ç", charactersIgnoringModifiers: "c", modifiers: [.control, .alternate],
            keyCode: 6, optionAsMeta: meta, mode: .modifyOtherKeys)
            == Array("\(esc)[27;\(meta ? 7 : 5);99~".utf8))
    }
}
