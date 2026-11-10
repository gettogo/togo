import Foundation
import Testing
@testable import TogoTerminal

/// The screen as rows of text with the cursor's place and each row's look: what the chat
/// composer crops to show an agent's own completion menu.
@MainActor
@Suite struct ScreenRowsTests {
    @Test func rowsCarryTheCursorAndTellAMarkedRowApart() async {
        let h = EngineHarness()
        // An input line, then a menu under it whose second item is drawn in reverse video,
        // then the cursor back at the end of the input line.
        h.feed("> /compact \r\n  soft\r\n  \(esc)[7mremote\(esc)[0m\r\n  snapcompact\(esc)[1;12H")
        let ok = await waitUntil { h.engine.screenRows()?.rows.count ?? 0 > 3 }
        #expect(ok)
        guard let screen = h.engine.screenRows() else {
            Issue.record("no screen")
            return
        }
        #expect(screen.cursorRow == 0)
        #expect(screen.cursorColumn == 11)
        #expect(screen.rows[0].text.hasPrefix("> /compact"))
        #expect(screen.rows[2].text.trimmingCharacters(in: .whitespaces) == "remote")
        #expect(screen.rows[1].look == screen.rows[3].look)
        #expect(screen.rows[2].look != screen.rows[1].look)
        #expect(screen.rows[5].look == 0)
    }
}
