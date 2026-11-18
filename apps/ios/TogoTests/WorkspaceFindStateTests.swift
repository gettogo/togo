import Testing
@testable import Togo

@MainActor struct WorkspaceFindStateTests {
    @Test func focusMovingCannotRetargetCleanup() {
        var cleared: [String] = []
        var searched: [String] = []
        func terminal(_ id: String) -> TerminalSearch {
            TerminalSearch(find: { _, _ in searched.append(id); return true }, clear: { cleared.append(id) })
        }
        var focused = terminal("A")
        var find = WorkspaceFindState()
        find.open(focused)
        focused = terminal("B")
        #expect(find.search?.find("needle", false) == true)
        find.close()
        #expect(searched == ["A"])
        #expect(cleared == ["A"])
        #expect(!find.isPresented)
        find.close()
        #expect(cleared == ["A"])
        find.open(focused)
        #expect(find.search?.find("other", false) == true)
        find.close()
        #expect(cleared == ["A", "B"])
    }

    @Test func replacingAnActiveFindClearsItsOriginalOwner() {
        var cleared: [String] = []
        var find = WorkspaceFindState()
        find.open(TerminalSearch(find: { _, _ in true }, clear: { cleared.append("A") }))
        find.open(TerminalSearch(find: { _, _ in true }, clear: { cleared.append("B") }))
        #expect(cleared == ["A"])
        find.close()
        #expect(cleared == ["A", "B"])
    }
}
