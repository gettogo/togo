import Foundation
import Testing
@testable import TogoKit

struct TerminalNativeWireTests {
    @Test func oldScreenNeedsNoInteractionFields() throws {
        let raw = #"{"id":"models","primitive":"picker","title":"Models","fetchedAt":"2026-11-14T12:00:00Z","stale":false,"actions":[],"picker":{"items":[],"searchable":true,"onSelect":"model"}}"#
        let screen = try JSONDecoder().decode(HarnessScreen.self, from: Data(raw.utf8))
        #expect(screen.interaction == nil)
        #expect(screen.picker?.onSelectArgs == nil)
    }

    @Test func readerScreenKeepsOwnershipAndPartialState() throws {
        let raw = #"{"id":"terminal.menu/a","interaction":"a","primitive":"picker","title":"Models","fetchedAt":"2026-11-14T12:00:00Z","stale":false,"actions":[],"picker":{"items":[],"searchable":true,"onSelect":"terminal.select","onSelectArgs":{"interaction":"a"},"presentation":{"state":"partial","total":500}}}"#
        let screen = try JSONDecoder().decode(HarnessScreen.self, from: Data(raw.utf8))
        #expect(screen.interaction == "a")
        #expect(screen.picker?.onSelectArgs?["interaction"] == .string("a"))
        #expect(screen.picker?.presentation?.state == "partial")
        #expect(!screen.stale)
        let roundTrip = try JSONDecoder().decode(HarnessScreen.self, from: JSONEncoder().encode(screen))
        #expect(roundTrip == screen)
    }
}
