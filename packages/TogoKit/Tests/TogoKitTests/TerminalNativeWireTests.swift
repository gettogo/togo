import Foundation
import Testing
@testable import TogoKit

struct TerminalNativeWireTests {
    @Test func aTerminalPromptUsesTheExistingWriteOnlyField() throws {
        let raw = #"{"id":"terminal.input","label":"Enter a value","kind":"secret","value":null,"isSet":false,"action":"terminal.input","actionLabel":"Continue","presentation":"prompt"}"#
        let field = try JSONDecoder().decode(HarnessScreen.Settings.Field.self, from: Data(raw.utf8))
        #expect(field.kind == .secret && field.value == .null)
        #expect(field.presentation == "prompt" && field.actionLabel == "Continue")
        #expect(try JSONDecoder().decode(HarnessScreen.Settings.Field.self, from: JSONEncoder().encode(field)) == field)
    }

    @Test func commandCatalogNoticeUsesTheExistingEnvelope() throws {
        let raw = #"{"sessionId":"s_menu","kind":"commands","ts":"2026-11-14T12:00:00Z","epoch":"conversation"}"#
        let note = try JSONDecoder().decode(HarnessNotification.self, from: Data(raw.utf8))
        #expect(note.kind == .commands && note.items == nil && note.mission == nil)
        #expect(try JSONDecoder().decode(HarnessNotification.self, from: JSONEncoder().encode(note)) == note)
    }

    @Test func oldScreenNeedsNoInteractionFields() throws {
        let raw = #"{"id":"models","primitive":"picker","title":"Models","fetchedAt":"2026-11-14T12:00:00Z","stale":false,"actions":[],"picker":{"items":[],"searchable":true,"onSelect":"model"}}"#
        let screen = try JSONDecoder().decode(HarnessScreen.self, from: Data(raw.utf8))
        #expect(screen.interaction == nil)
        #expect(screen.picker?.onSelectArgs == nil)
    }

    @Test func readerScreenKeepsOwnershipAndPartialState() throws {
        let raw = #"{"id":"terminal.menu/a","interaction":"a","primitive":"picker","title":"Models","fetchedAt":"2026-11-14T12:00:00Z","stale":false,"actions":[],"picker":{"items":[],"searchable":true,"query":"deep","onSelect":"terminal.select","onSelectArgs":{"interaction":"a"},"presentation":{"state":"partial","total":500}}}"#
        let screen = try JSONDecoder().decode(HarnessScreen.self, from: Data(raw.utf8))
        #expect(screen.interaction == "a")
        #expect(screen.picker?.onSelectArgs?["interaction"] == .string("a"))
        #expect(screen.picker?.presentation?.state == "partial")
        #expect(screen.picker?.query == "deep")
        #expect(!screen.stale)
        let roundTrip = try JSONDecoder().decode(HarnessScreen.self, from: JSONEncoder().encode(screen))
        #expect(roundTrip == screen)
    }

    @Test func aPartialListCarriesItsOwnPagingAction() throws {
        let presentation = HarnessPickerPresentation(state: "partial",
            moreAction: .init(id: "terminal.more", label: "Show more", args: ["interaction": .string("panel")]))
        let decoded = try JSONDecoder().decode(HarnessPickerPresentation.self, from: JSONEncoder().encode(presentation))
        #expect(decoded == presentation)
        #expect(decoded.moreAction?.args?["interaction"] == .string("panel"))
    }
}
