import Foundation
import Testing
import TogoKit
@testable import Togo

/// The roster: machines in the order they were added, sessions in bib order, a headline that
/// counts what needs the user and a tally of the rest, and a selection only beside a terminal.
@MainActor struct RosterModelTests {
    private let services = FakeAppServices(hosts: Samples.hosts)
    private let one = TabID(hostID: Samples.studio, sessionID: "s-01")
    private let four = TabID(hostID: Samples.studio, sessionID: "s-04")

    private func store(_ state: ControlCenterState = Samples.controlCenter) -> AppStore {
        AppStore(services: services, controlCenter: state)
    }

    private func machine(_ rows: [SessionRow]) -> MachineSection {
        MachineSection(
            id: Samples.studio, name: "Studio", symbol: .desktop, reachability: .online,
            helper: .present(version: "0.3.0", live: true), sessions: rows, lastSeen: Samples.now
        )
    }

    // MARK: Grouping and order

    @Test func groupsByMachineInTheOrderTheyWereAdded() {
        let roster = store().roster
        #expect(roster.groups.map(\.id) == [Samples.studio, Samples.build])
        #expect(roster.groups.map(\.name) == ["Studio", "Build box"])
        #expect(roster.groups.map(\.count) == [3, 1])
    }

    @Test func linesKeepBibOrderWhateverTheirStatus() {
        var state = Samples.controlCenter
        state.machines[0].sessions[0].status = .needsYou
        state.machines[0].sessions[1].status = .done
        #expect(store(state).roster.groups[0].lines.map(\.row.id) == ["s-01", "s-04", "s-scratch"])
    }

    /// A helper that lists a session twice, or a machine reported twice, still shows each once.
    @Test func repeatsCollapseToOneLine() {
        var state = Samples.controlCenter
        state.machines[0].sessions.append(state.machines[0].sessions[0])
        state.machines.append(state.machines[1])
        let roster = store(state).roster
        let ids = roster.lines.map(\.id)
        #expect(Set(ids).count == ids.count)
        #expect(roster.groups.map(\.id) == [Samples.studio, Samples.build])
    }

    /// The same session id on two machines is two lines.
    @Test func sameSessionIDOnTwoMachinesIsTwoLines() {
        var state = Samples.controlCenter
        state.machines[1].sessions.append(Samples.row(hostID: Samples.build, bib: "01", name: "api", cwd: "/home/sam"))
        let ids = store(state).roster.lines.map(\.id)
        #expect(Set(ids).count == ids.count)
        #expect(ids.filter { $0.sessionID == "s-01" }.count == 2)
    }

    @Test func renameUpdatesTheGroupName() {
        let store = store()
        #expect(store.roster.groups.first?.name == "Studio")
        services.hosts[0].name = "Studio Mac"
        #expect(store.roster.groups.first?.name == "Studio Mac")
    }

    /// The phone being offline is said once above the roster, not under every machine.
    @Test func deviceOfflineIsOneBanner() {
        var first = machine([])
        var second = machine([])
        #expect(!RosterModel.make(machines: [first, second], requests: []).deviceOffline)
        first.reachability = .deviceOffline
        second.reachability = .deviceOffline
        #expect(RosterModel.make(machines: [first, second], requests: []).deviceOffline)
    }

    // MARK: Needs you, headline and tally

    @Test func aPendingRequestMarksItsLine() {
        let roster = RosterModel.make(
            machines: [machine([Samples.row(bib: "01", name: "api", status: .working, cwd: "/a")])],
            requests: [Samples.request(id: "r", bib: "01", title: "Run npm test?")]
        )
        #expect(roster.lines[0].needsYou)
        #expect(roster.lines[0].status == .needsYou)
        #expect(roster.waiting == 1)
        #expect(roster.tally == RosterModel.Tally())
    }

    @Test func anAnsweredRequestNoLongerCounts() {
        var card = Samples.request(id: "r", bib: "01", title: "Run npm test?")
        card.phase = .done(.approved)
        let roster = RosterModel.make(
            machines: [machine([Samples.row(bib: "01", name: "api", status: .working, cwd: "/a")])],
            requests: [card]
        )
        #expect(!roster.lines[0].needsYou)
        #expect(roster.waiting == 0)
        #expect(roster.headline == "All clear")
    }

    /// A line with both a request and a needs-you status counts once; a request with no listed
    /// session counts on its own.
    @Test func headlineCountsEachThingThatWaitsOnce() {
        let roster = store().roster
        #expect(roster.waiting == 1)
        #expect(roster.headline == "1 needs you")

        var state = Samples.controlCenter
        state.requests.append(Samples.request(id: "r9", bib: nil, title: "Allow network?"))
        state.requests.append(Samples.request(id: "r2", bib: "01", title: "Edit routes.ts?"))
        let more = store(state).roster
        #expect(more.waiting == 3)
        #expect(more.headline == "3 need you")
    }

    @Test func tallyCountsTheRestInOrderAndLeavesOutEmptyStates() {
        let roster = store().roster
        // api working, scratch idle, release done; app needs you and is not in the tally.
        #expect(roster.tally == RosterModel.Tally(working: 1, idle: 1, done: 1))
        #expect(roster.tallyText == "1 working  1 idle  1 done")

        let quiet = RosterModel.make(
            machines: [machine([
                Samples.row(bib: "01", name: "a", status: .working, cwd: "/a"),
                Samples.row(bib: "02", name: "b", status: .working, cwd: "/b"),
                Samples.row(bib: "03", name: "c", status: .ended, cwd: "/c"),
                Samples.row(bib: "04", name: "d", status: .failed, cwd: "/d"),
            ])],
            requests: []
        )
        #expect(quiet.tallyText == "2 working  2 done")
        #expect(quiet.headline == "All clear")
        #expect(RosterModel.empty.tallyText.isEmpty)
    }

    // MARK: Selection

    @Test func selectionOnlyBesideATerminalAndOnlyForListedLines() {
        let roster = store().roster
        #expect(RosterModel.selection(focused: four, wide: true, in: roster) == four)
        #expect(RosterModel.selection(focused: four, wide: false, in: roster) == nil)
        #expect(RosterModel.selection(focused: nil, wide: true, in: roster) == nil)
        let gone = TabID(hostID: Samples.studio, sessionID: "s-gone")
        #expect(RosterModel.selection(focused: gone, wide: true, in: roster) == nil)
    }

    @Test func selectedLineFollowsTheFocusedPane() {
        let store = store()
        store.setWorkspaceInline(true)
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        let focused = store.workspaceOnScreen ? store.focusedTab?.id : nil
        #expect(RosterModel.selection(focused: focused, wide: true, in: store.roster) == one)
        store.monitorShown = true
        #expect(!store.workspaceOnScreen)
    }
}
