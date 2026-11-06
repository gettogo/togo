import Foundation
import Testing
import TogoKit
@testable import Togo

/// The sidebar list keys its rows by `SidebarRowID`. A repeated id, or a selection the list
/// does not have, makes the collection view's batch update inconsistent and crashes the app.
@MainActor struct SidebarIdentityTests {
    private let services = FakeAppServices(hosts: Samples.hosts)

    private func store(_ state: ControlCenterState = Samples.controlCenter) -> AppStore {
        let store = AppStore(services: services, controlCenter: state)
        store.setWorkspaceInline(true)
        return store
    }

    private func expectUnique(_ store: AppStore, sourceLocation: SourceLocation = #_sourceLocation) {
        let ids = store.sidebarRowIDs
        #expect(Set(ids).count == ids.count, "repeated row ids: \(ids)", sourceLocation: sourceLocation)
    }

    private func restorable(_ id: String) -> RestorableSession {
        RestorableSession(
            id: id, bib: "09", name: "old", agent: "claude", command: ["claude"], cwd: "/Users/sam",
            createdAt: Samples.now, endedAt: Samples.now, resumable: true
        )
    }

    /// The card waiting on the user and its session's row are two rows with two ids.
    @Test func cardAndItsSessionRowHaveDistinctIDs() {
        let store = store()
        let ids = store.sidebarRowIDs
        #expect(ids.contains(.card("r1")))
        #expect(ids.contains(.session(TabID(hostID: Samples.studio, sessionID: "s-04"))))
        expectUnique(store)
    }

    /// A helper that lists a session twice, a repeated card, and a restorable session that is
    /// already back live each show once.
    @Test func repeatsCollapseToOneRow() {
        var state = Samples.controlCenter
        state.machines[0].sessions.append(state.machines[0].sessions[0])
        state.machines[0].restorable = [restorable("s-01"), restorable("s-old"), restorable("s-old")]
        state.machines.append(state.machines[1])
        state.requests.append(state.requests[0])
        let store = store(state)

        expectUnique(store)
        #expect(store.listMachines.map(\.id) == [Samples.studio, Samples.build])
        #expect(store.listMachines[0].sessions.map(\.id) == ["s-01", "s-04", "s-scratch"])
        #expect(store.listMachines[0].restorable.map(\.id) == ["s-old"])
        #expect(store.listRequests.map(\.id) == ["r1"])
    }

    /// The same session id on two machines is two rows.
    @Test func sameSessionIDOnTwoMachinesIsTwoRows() {
        var state = Samples.controlCenter
        state.machines[1].sessions.append(Samples.row(hostID: Samples.build, bib: "01", name: "api", cwd: "/home/sam"))
        let store = store(state)
        expectUnique(store)
        #expect(store.sidebarRowIDs.filter { if case .session(let id) = $0 { id.sessionID == "s-01" } else { false } }.count == 2)
    }

    /// Ids of rows that stay do not change when a request comes and goes or a session changes
    /// state.
    @Test func idsStayStableAcrossUpdates() {
        let store = store()
        let before = store.sidebarRowIDs.filter { if case .card = $0 { false } else { true } }

        store.controlCenter.requests.append(Samples.request(id: "r2", bib: "01", title: "Edit routes.ts?"))
        expectUnique(store)
        store.controlCenter.requests.removeAll()
        #expect(store.sidebarRowIDs == before)

        store.controlCenter.machines[0].sessions[0].status = .needsYou
        store.controlCenter.machines[0].sessions[1].status = .done
        #expect(store.sidebarRowIDs == before)
        expectUnique(store)
    }

    /// A filter that hides every row leaves the board alone; the selection follows only rows
    /// the list has.
    @Test func selectionOnlyNamesListedRows() {
        let store = store()
        let four = TabID(hostID: Samples.studio, sessionID: "s-04")
        store.open(hostID: four.hostID, sessionID: four.sessionID)
        #expect(store.listedSidebarSelection == .session(four))

        store.filter = .working
        #expect(store.sidebarRowIDs.contains(.session(four)) == false)
        #expect(store.listedSidebarSelection == nil)
        expectUnique(store)

        store.filter = .all
        store.boardShown = true
        #expect(store.listedSidebarSelection == .board)
    }

    /// Renaming a machine renames its heading, though its section was built before.
    @Test func renameUpdatesTheHeading() {
        let store = store()
        #expect(store.listMachines.first?.name == "Studio")
        services.hosts[0].name = "Studio Mac"
        services.hosts[0].symbol = .laptop
        #expect(store.listMachines.first?.name == "Studio Mac")
        #expect(store.listMachines.first?.symbol == .laptop)
    }
}
