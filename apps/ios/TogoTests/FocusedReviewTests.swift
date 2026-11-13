import Testing
import TogoKit
@testable import Togo

@MainActor struct FocusedReviewTests {
    @Test(arguments: [false, true], [ReviewTab.changes, .files, .ports])
    func shortcutsReviewTheFocusedPane(secondary: Bool, tab: ReviewTab) {
        var state = Samples.controlCenter
        state.machines[0].capabilities = [Capability.git, Capability.gitWrite, Capability.ports]
        let store = AppStore(services: FakeAppServices(), controlCenter: state)
        let primary = TabID(hostID: Samples.studio, sessionID: "s-01")
        let beside = TabID(hostID: Samples.studio, sessionID: "s-04")
        store.open(hostID: primary.hostID, sessionID: primary.sessionID)
        store.open(hostID: beside.hostID, sessionID: beside.sessionID)
        store.select(primary)
        store.setPanesVisible(true)
        store.openBeside(beside)
        store.focus(secondary ? .secondary : .primary)

        #expect(store.canOpenReview(tab))
        store.openReview(tab)

        #expect(store.reviewing?.tab == tab)
        #expect(store.reviewing?.subject.hostID == Samples.studio)
        #expect(store.reviewing?.subject.sessionID == (secondary ? "s-04" : "s-01"))
        #expect(store.reviewing?.subject.cwd == (secondary ? "/Users/sam/src/app" : "/Users/sam/src/api"))
    }

    @Test(arguments: [false, true])
    func reviewAvailabilityUsesTheFocusedMachinesCapabilities(secondary: Bool) {
        var state = Samples.controlCenter
        state.machines[0].capabilities = [Capability.git]
        state.machines[1].capabilities = [Capability.ports]
        state.machines[1].reachability = .online
        let store = AppStore(services: FakeAppServices(), controlCenter: state)
        store.open(hostID: Samples.studio, sessionID: "s-01")
        store.select(TabID(hostID: Samples.studio, sessionID: "s-01"))
        store.setPanesVisible(true)
        store.openBeside(TabID(hostID: Samples.build, sessionID: "s-02"))
        store.focus(secondary ? .secondary : .primary)

        #expect(store.canOpenReview(.changes) == !secondary)
        #expect(store.canOpenReview(.files) == !secondary)
        #expect(store.canOpenReview(.ports) == secondary)
        store.openReview(secondary ? .ports : .changes)
        #expect(store.reviewing?.subject.hostID == (secondary ? Samples.build : Samples.studio))
    }
}
