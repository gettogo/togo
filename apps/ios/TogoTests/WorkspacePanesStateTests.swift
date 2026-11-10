import CoreGraphics
import Foundation
import SwiftUI
import Testing
@testable import Togo

@MainActor struct WorkspacePanesStateTests {
    private let services = FakeAppServices()
    private let one = TabID(hostID: Samples.studio, sessionID: "s-01")
    private let four = TabID(hostID: Samples.studio, sessionID: "s-04")
    private let two = TabID(hostID: Samples.studio, sessionID: "s-02")

    private func store() -> AppStore {
        let store = AppStore(services: services, controlCenter: Samples.controlCenter)
        store.setPanesVisible(true)
        return store
    }

    @Test func collapsedSecondaryShowsItsRequests() {
        var state = Samples.controlCenter
        state.requests = [Samples.request(id: "r-04", bib: "04", title: "Run the tests?")]
        let store = AppStore(services: services, controlCenter: state)
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.setPanesVisible(true)
        store.openBeside(four)
        #expect(store.incomingRequests.isEmpty)
        store.setPanesVisible(false)
        #expect(store.incomingRequests.map(\.id) == ["r-04"])
    }

    @Test func collapsingFocusesThePrimary() {
        let store = store()
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.open(hostID: two.hostID, sessionID: two.sessionID)
        store.open(hostID: four.hostID, sessionID: four.sessionID)
        store.select(one)
        store.openBeside(two)
        #expect(store.workspace.focused == .secondary)
        store.setPanesVisible(false)
        #expect(store.workspace.focused == .primary)
        #expect(store.focusedTab?.id == one)

        store.selectTab(number: 3)
        #expect(store.workspace.selected == four)
        #expect(store.workspace.secondary == two)

        store.perform(.detach)
        #expect(!store.workspace.tabs.contains { $0.id == four })
        #expect(store.workspace.tabs.contains { $0.id == two })
    }

    @Test func sameTabNeverFillsBothPanes() {
        let store = store()
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.open(hostID: four.hostID, sessionID: four.sessionID)
        #expect(store.workspace.selected == four)

        store.openBeside(four)
        #expect(store.workspace.selected == one)
        #expect(store.workspace.secondary == four)

        store.place(four, in: .primary)
        #expect(store.workspace.selected == four)
        #expect(store.workspace.secondary == one)

        store.select(one)
        #expect(store.workspace.selected != store.workspace.secondary)

        let lonely = self.store()
        lonely.open(hostID: one.hostID, sessionID: one.sessionID)
        lonely.openBeside(one)
        #expect(lonely.workspace.selected == one)
        #expect(lonely.workspace.secondary == nil)
    }

    @Test func openBesideOpensTheTab() {
        let store = store()
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.openBeside(four)
        #expect(store.workspace.tabs.map(\.id) == [one, four])
        #expect(store.workspace.selected == one)
        #expect(store.workspace.secondary == four)
        #expect(store.workspace.focused == .secondary)
        #expect(store.focusedTab?.id == four)

        store.swapPanes()
        #expect(store.workspace.selected == four)
        #expect(store.workspace.secondary == one)
        #expect(store.focusedTab?.id == four)

        store.closeSecondary()
        #expect(store.workspace.secondary == nil)
        #expect(store.workspace.focused == .primary)
        #expect(store.workspace.tabs.count == 2)
    }

    @Test func closingPrimaryPromotesSecondary() {
        let store = store()
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.open(hostID: two.hostID, sessionID: two.sessionID)
        store.openBeside(four)
        store.close(two)
        #expect(store.workspace.selected == four)
        #expect(store.workspace.secondary == nil)
        #expect(store.workspace.focused == .primary)

        store.openBeside(one)
        store.close(one)
        #expect(store.workspace.selected == four)
        #expect(store.workspace.secondary == nil)
    }

    @Test func numberShortcutTargetsFocusedPane() {
        let store = store()
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.open(hostID: two.hostID, sessionID: two.sessionID)
        store.open(hostID: four.hostID, sessionID: four.sessionID)
        store.select(one)
        store.openBeside(two)
        #expect(store.workspace.focused == .secondary)

        store.selectTab(number: 3)
        #expect(store.workspace.secondary == four)
        #expect(store.workspace.selected == one)

        store.focus(.primary)
        store.selectTab(number: 2)
        #expect(store.workspace.selected == two)
        #expect(store.workspace.secondary == four)
    }

    @Test func closingATabLetsGoOfWhatWaitedForIt() {
        let store = store()
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.pendingInput[one] = "left behind"
        store.attaching = one
        store.close(one)
        #expect(store.pendingInput[one] == nil)
        #expect(store.attaching == nil)
    }

    @Test func secondarySurvivesCompactAndBack() {
        let store = store()
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.openBeside(four)
        let size = CGSize(width: 1032, height: 1376)
        let state = store.workspace
        let compact = PaneLayout.arrange(
            size: size, sizeClass: .compact, fold: .flat,
            hasSecondary: state.secondary != nil, primaryFraction: state.primaryFraction
        )
        #expect(compact == .single)
        #expect(store.workspace.secondary == four)
        let regular = PaneLayout.arrange(
            size: size, sizeClass: .regular, fold: .flat,
            hasSecondary: store.workspace.secondary != nil, primaryFraction: state.primaryFraction
        )
        #expect(regular == .sideBySide(primaryWidth: 515.5, gap: nil))
    }
}
