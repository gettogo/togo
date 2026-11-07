import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct SidebarTests {
    private let one = TabID(hostID: Samples.studio, sessionID: "s-01")
    private let four = TabID(hostID: Samples.studio, sessionID: "s-04")
    private let scratch = TabID(hostID: Samples.studio, sessionID: "s-scratch")
    private let release = TabID(hostID: Samples.build, sessionID: "s-02")

    private func store() -> AppStore {
        let store = AppStore(services: FakeAppServices(hosts: Samples.hosts), controlCenter: Samples.controlCenter)
        store.setWorkspaceInline(true)
        return store
    }

    private var rows: [SessionRow] {
        Samples.controlCenter.machines.flatMap(\.sessions)
    }

    /// The roster carries the collapse, so an answered request still leaves after the bar
    /// showed its outcome.
    @Test func answeredRequestsLeave() async {
        let store = store()
        await store.answer("r1", .approve)
        #expect(store.answeredCardIDs == ["r1"])
        await store.collapseAnswered(store.answeredCardIDs, hold: .zero)
        #expect(store.controlCenter.requests.isEmpty)
        // What still counts is only the sessions that report needing the user themselves.
        #expect(store.roster.waiting == rows.filter { $0.status == .needsYou }.count)
    }

    /// ⌘K opened the search; closing it, by Escape or Cancel, hands focus back to the terminal.
    @Test func escapeFromShortcutSearchReturnsToTerminal() {
        var keys = SidebarKeyboard()
        let tapped = keys.searchClosed()
        #expect(!tapped)
        keys.searchRequested()
        let fromShortcut = keys.searchClosed()
        #expect(fromShortcut)
        // Only once: a later search the user opened by tapping stays in the sidebar.
        let again = keys.searchClosed()
        #expect(!again)
    }

    @Test func arrowsMoveTheCursorAndStopAtTheEnds() {
        let ids = rows.map { TabID(hostID: $0.hostID, sessionID: $0.id) }
        var keys = SidebarKeyboard()
        keys.move(1, through: ids)
        #expect(keys.cursor == one)
        keys.move(-1, through: ids)
        #expect(keys.cursor == one)
        keys.move(1, through: ids)
        keys.move(1, through: ids)
        #expect(keys.cursor == scratch)
        keys.move(1, through: ids)
        keys.move(1, through: ids)
        #expect(keys.cursor == release)

        var fromBottom = SidebarKeyboard()
        fromBottom.move(-1, through: ids)
        #expect(fromBottom.cursor == release)
        #expect(SidebarKeyboard().row(in: rows) == nil)
        #expect(fromBottom.row(in: rows)?.id == "s-02")
    }

    /// ⌘⌫ asks only where the row menu offers Kill.
    @Test func commandDeleteOnlyWhereKillIsOffered() {
        let machines = Samples.controlCenter.machines
        var keys = SidebarKeyboard()
        keys.cursor = four
        #expect(keys.rowToEnd(in: rows, machines: machines)?.id == "s-04")
        // Started outside Togo: no Kill in the menu.
        keys.cursor = scratch
        #expect(keys.rowToEnd(in: rows, machines: machines) == nil)
        // The machine is offline, so Kill is disabled.
        keys.cursor = release
        #expect(keys.rowToEnd(in: rows, machines: machines) == nil)
    }

    /// A narrowed store lists only the machines with matching sessions.
    @Test func filterNarrowsTheMachines() {
        let store = store()
        store.filter = .working
        #expect(store.listNarrowed)
        #expect(store.listMachines.map(\.id) == [Samples.studio])
        store.filter = .all
        #expect(store.listMachines.map(\.id) == [Samples.studio, Samples.build])
    }

    @Test func archiveIsReachableBesideTheTerminal() {
        var state = Samples.controlCenter
        state.archived = [Samples.row(hostID: Samples.build, bib: "07", name: "old", status: .done, cwd: "/tmp")]
        let store = AppStore(services: FakeAppServices(hosts: Samples.hosts), controlCenter: state)
        store.setWorkspaceInline(true)
        store.showArchive()
        #expect(store.path == [.archive])
    }
}
