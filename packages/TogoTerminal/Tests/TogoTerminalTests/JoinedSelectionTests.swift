import Foundation
import Testing
@testable import TogoTerminal

@MainActor
@Suite struct JoinedSelectionTests {
    @Test func wrappedSelectionJoins() async {
        let h = EngineHarness()
        let cols = h.engine.size.cols
        let long = String(repeating: "a", count: cols) + String(repeating: "b", count: 10)
        h.feed("\(long)\r\nnext")
        #expect(h.engine.lineText(1).hasPrefix("bbbb"))
        h.engine.selectAll()
        h.engine.copySelection()
        #expect(h.clipboardWrites == ["\(long)\nnext"])
        #expect(!h.engine.isSelecting)
    }

    @Test func copyWithoutSelectionWritesNothing() {
        let h = EngineHarness()
        h.feed("text")
        h.engine.copySelection()
        #expect(h.clipboardWrites.isEmpty)
    }
}
