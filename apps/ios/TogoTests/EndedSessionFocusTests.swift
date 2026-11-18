import Testing
import TogoKit
@testable import Togo

@MainActor struct EndedSessionFocusTests {
    @Test(arguments: [false, true])
    func losingTheFocusedSessionReturnsToTheListBeforeAnotherTerminalAppears(_ split: Bool) throws {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        let one = TabID(hostID: Samples.studio, sessionID: "s-01")
        let four = TabID(hostID: Samples.studio, sessionID: "s-04")
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.open(hostID: four.hostID, sessionID: four.sessionID)
        if split {
            store.setWorkspaceInline(true)
            store.setPanesVisible(true)
            store.openBeside(one)
            store.focus(.secondary)
        }
        let ended = try #require(store.focusedTab).id
        let remainingIDs = Set(store.workspace.tabs.map(\.sessionID).filter { $0 != ended.sessionID })
        store.closeEndedTabs(on: ended.hostID, listed: remainingIDs)
        #expect(!store.workspaceOpen)
        #expect(store.focusedTab == nil)
        #expect(!store.workspace.tabs.isEmpty)
        #expect(store.notice?.text.contains("ended") == true)
        let remaining = store.workspace.tabs[0]
        store.open(hostID: remaining.hostID, sessionID: remaining.sessionID)
        #expect(store.workspaceOpen)
    }

    @Test func anotherSessionEndingDoesNotInterruptTheFocusedOne() {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.open(hostID: Samples.studio, sessionID: "s-01")
        store.open(hostID: Samples.studio, sessionID: "s-04")
        store.closeEndedTab(TabID(hostID: Samples.studio, sessionID: "s-01"))
        #expect(store.workspaceOpen)
        #expect(store.focusedTab?.sessionID == "s-04")
        #expect(store.notice == nil)
    }
}
