import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct AppStoreTests {
    private let services = FakeAppServices()
    private let now = Samples.now

    private func store(_ state: ControlCenterState = Samples.controlCenter) -> AppStore {
        AppStore(services: services, controlCenter: state)
    }

    private func snapshot(
        _ host: HostID = Samples.studio, sessions: [SessionRow], requests: [RequestCard] = [], at: Date? = nil
    ) -> HostViewSnapshot {
        HostViewSnapshot(
            hostID: host, reachability: .online, helper: .present(version: "0.3.0", live: true),
            sessions: sessions, requests: requests, at: at ?? now
        )
    }

    private func bibs(_ store: AppStore, _ host: HostID = Samples.studio) -> [String] {
        store.controlCenter.machines.first { $0.id == host }?.sessions.map { $0.bib ?? $0.name } ?? []
    }

    @Test func searchMatchesNameAgentPathAndBib() {
        let store = store()
        let cases: [(query: String, expected: [String])] = [
            ("api", ["01"]),
            ("shell", ["scratch"]),
            ("src/app", ["04"]),
            ("02", ["02"]),
            ("CLAUDE", ["01", "04", "02"]),
        ]
        for (query, expected) in cases {
            store.query = query
            let found = store.visibleMachines.flatMap(\.sessions).map { $0.bib ?? $0.name }
            #expect(found == expected, "query \(query)")
        }
    }

    @Test func filterNeedsYouShowsOnlyNeedsYou() {
        let store = store()
        store.filter = .needsYou
        let machines = store.visibleMachines
        #expect(machines.map(\.id) == [Samples.studio])
        #expect(machines.flatMap(\.sessions).map(\.status) == [.needsYou])
    }

    @Test func doneOlderThanSixHoursArchived() {
        let store = store(ControlCenterState())
        let sixHours = AppStore.archiveAfter
        store.apply(snapshot(sessions: [
            Samples.row(bib: "01", name: "old", status: .done, cwd: "/srv", since: now.addingTimeInterval(-sixHours - 1)),
            Samples.row(bib: "02", name: "ended", status: .exited, cwd: "/srv", since: now.addingTimeInterval(-sixHours - 60)),
            Samples.row(bib: "03", name: "recent", status: .done, cwd: "/srv", since: now.addingTimeInterval(-sixHours + 60)),
            Samples.row(bib: "04", name: "idle", status: .idle, cwd: "/srv", since: now.addingTimeInterval(-sixHours * 3)),
        ]))
        #expect(bibs(store) == ["03", "04"])
        #expect(store.controlCenter.archived.compactMap(\.bib) == ["01", "02"])
    }

    @Test func rowsKeepBibOrderAcrossStatusChange() {
        let store = store(ControlCenterState())
        let rows = [
            Samples.row(bib: nil, name: "notes", status: .idle, cwd: "/srv"),
            Samples.row(bib: "10", name: "ten", status: .working, cwd: "/srv"),
            Samples.row(bib: "02", name: "two", status: .working, cwd: "/srv"),
            Samples.row(bib: nil, name: "aux", status: .idle, cwd: "/srv"),
        ]
        store.apply(snapshot(sessions: rows))
        #expect(bibs(store) == ["02", "10", "aux", "notes"])

        var changed = rows
        changed[1].status = .needsYou
        changed[2].status = .done
        store.apply(snapshot(sessions: changed.reversed()))
        #expect(bibs(store) == ["02", "10", "aux", "notes"])
    }

    @Test func applyReplacesOnlyThatMachine() {
        let store = store()
        let buildBefore = store.controlCenter.machines.first { $0.id == Samples.build }
        store.apply(snapshot(
            sessions: [Samples.row(bib: "07", name: "fresh", cwd: "/srv")],
            requests: [Samples.request(id: "r9", bib: "07", title: "Delete build/?")]
        ))
        #expect(bibs(store) == ["07"])
        #expect(store.controlCenter.machines.first { $0.id == Samples.build } == buildBefore)
        #expect(store.controlCenter.requests.map(\.id) == ["r9"])
    }

    @Test func openTwiceSelectsExistingTab() {
        let store = store()
        store.open(hostID: Samples.studio, sessionID: "s-01")
        store.open(hostID: Samples.studio, sessionID: "s-04")
        store.open(hostID: Samples.studio, sessionID: "s-01")
        #expect(store.workspace.tabs.map(\.sessionID) == ["s-01", "s-04"])
        #expect(store.workspace.selected?.sessionID == "s-01")
        #expect(store.workspaceOpen)

        store.close(TabID(hostID: Samples.studio, sessionID: "s-01"))
        #expect(store.workspace.selected?.sessionID == "s-04")
        store.close(TabID(hostID: Samples.studio, sessionID: "s-04"))
        #expect(!store.workspaceOpen)
    }

    @Test func answerConflictBecomesAnsweredElsewhere() async {
        let store = store()
        services.failure = HelperError.conflict("already answered")
        await store.answer("r1", .approve)
        #expect(store.controlCenter.requests.first?.phase == .done(.answeredElsewhere))
        #expect(store.notice == nil)
        #expect(services.calls == [.answer(Samples.studio, "r1", .approve)])
    }

    @Test func requestClosedAtTheDeskSaysSo() {
        let store = store()
        var update = snapshot(sessions: Samples.controlCenter.machines[0].sessions)
        update.closed = ["r1": .answeredElsewhere]
        store.apply(update)
        #expect(store.controlCenter.requests.map(\.phase) == [.done(.answeredElsewhere)])
    }

    @Test func requestThatTimedOutSaysExpired() {
        let store = store()
        var update = snapshot(sessions: Samples.controlCenter.machines[0].sessions)
        update.closed = ["r1": .expired]
        store.apply(update)
        #expect(store.controlCenter.requests.map(\.phase) == [.done(.expired)])
    }

    @Test func answeredCardOutlivesTheNextSnapshotUntilDismissed() async {
        let store = store()
        await store.answer("r1", .approve)
        store.apply(snapshot(sessions: Samples.controlCenter.machines[0].sessions))
        #expect(store.controlCenter.requests.map(\.phase) == [.done(.approved)])

        store.dismissAnswered(["r1"])
        #expect(store.controlCenter.requests.isEmpty)
    }

    @Test func openCardIsNotDismissed() {
        let store = store()
        store.dismissAnswered(["r1"])
        #expect(store.controlCenter.requests.map(\.id) == ["r1"])
    }

    @Test func snapshotForARemovedMachineIsDropped() {
        let store = store()
        // As in the live services, the machine leaves `hosts` before its screens are told.
        services.hosts.removeAll { $0.id == Samples.studio }
        store.forgetMachine(Samples.studio)
        store.apply(snapshot(sessions: [Samples.row(bib: "07", name: "late", cwd: "/srv")]))
        #expect(!store.controlCenter.machines.contains { $0.id == Samples.studio })
    }

    @Test func machineAddedBackUnderItsIDShowsAgain() {
        let store = store()
        let hosts = services.hosts
        services.hosts = []
        store.forgetMachine(Samples.studio)
        services.hosts = hosts
        store.apply(snapshot(sessions: [Samples.row(bib: "07", name: "back", cwd: "/srv")]))
        #expect(bibs(store) == ["07"])
    }

    @Test func denyingOptionReadsDenied() async {
        let store = store()
        await store.answer("r1", .option(2))
        #expect(store.controlCenter.requests.first?.phase == .done(.denied))
        let other = self.store()
        await other.answer("r1", .option(1))
        #expect(other.controlCenter.requests.first?.phase == .done(.approved))
    }

    @Test func helperErrorNoticeIsPlainSentence() async {
        let store = store()
        services.failure = HelperError.rpc(RPCErrorObject(code: RPCErrorCode.notFound, message: "not_found"))
        // Ending one that already ended is no error (WorkspaceTests); renaming it is.
        await store.rename(Samples.controlCenter.machines[0].sessions[0], to: "other")
        #expect(store.notice?.text.contains("This session has ended.") == true)
        #expect(store.notice?.text.contains("not_found") == false)
    }

    @Test func answerFailureReopensCardWithItsOwnFailure() async {
        let store = store()
        services.failure = HelperError.closed
        await store.answer("r1", .deny)
        #expect(store.controlCenter.requests.first?.phase == .open)
        #expect(store.answerFailures == ["r1": "Couldn't reach Studio. Try again."])
        #expect(store.notice == nil)
    }

    @Test func snapshotKeepsPhaseOfCardBeingAnswered() {
        let store = store()
        store.controlCenter.requests[0].phase = .sending(.approve)
        store.apply(snapshot(sessions: [], requests: [Samples.request(id: "r1", bib: "04", title: "Run npm test?")]))
        #expect(store.controlCenter.requests.map(\.phase) == [.sending(.approve)])
    }
}
