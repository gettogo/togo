import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct SessionListRound3Tests {
    @Test func railUsesTheSessionNameAndKeepsUserCapitalization() throws {
        let row = Samples.row(bib: "01", name: "my API check", agentName: "OMP", cwd: "/srv")
        var state = Samples.controlCenter
        state.machines[0].sessions = [row]
        let store = AppStore(services: FakeAppServices(), controlCenter: state)
        store.open(hostID: row.hostID, sessionID: row.id)
        let tab = try #require(store.workspace.tabs.first)
        #expect(store.chipLabel(for: tab).bib == "01")
        #expect(store.chipLabel(for: tab).name == row.name)
        store.controlCenter.machines = []
        #expect(store.chipLabel(for: tab).name == row.name)
    }

    @Test func restorationDropsRemovedMachinesButKeepsKnownOfflineMachines() {
        let services = FakeAppServices()
        let known = TabID(hostID: Samples.studio, sessionID: "s-01")
        let removed = TabID(hostID: HostID(), sessionID: "s-old")
        let store = AppStore(services: services, controlCenter: ControlCenterState())
        store.restore(WindowRestoration(
            tabs: [removed, known], selected: removed, secondary: known, workspaceOpen: true,
            labels: [TabLabel(id: known, bib: "01", title: "offline API", agent: "OMP")]
        ))
        #expect(store.workspace.tabs.map(\.id) == [known])
        #expect(store.workspace.tabs.first?.title == "offline API")
        #expect(store.workspace.selected == known)
        #expect(store.workspace.secondary == nil)
        #expect(store.workspaceOpen)

        store.restore(WindowRestoration(tabs: [removed], selected: removed, secondary: nil, workspaceOpen: true))
        #expect(store.workspace.tabs.isEmpty)
        #expect(store.workspace.selected == nil)
        #expect(!store.workspaceOpen)
    }

    @Test func archiveRequiresAnEndedSessionAndPreservesTheAgeBoundary() {
        let statuses: [SessionStatus] = [.done, .working, .needsYou, .idle, .unknown("future"), .exited, .ended, .failed]
        let old = Samples.now.addingTimeInterval(-AppStore.archiveAfter - 1)
        let rows = statuses.enumerated().map { index, status in
            Samples.row(bib: String(index), name: "session", status: status, cwd: "/srv", since: old)
        }
        let boundary = Samples.row(
            bib: "10", name: "just ended", status: .ended, cwd: "/srv",
            since: Samples.now.addingTimeInterval(-AppStore.archiveAfter)
        )
        let store = AppStore(services: FakeAppServices(), controlCenter: ControlCenterState())
        store.apply(HostViewSnapshot(
            hostID: Samples.studio, reachability: .online, helper: .present(version: "1.1.0", live: true),
            sessions: rows + [boundary], requests: [], at: Samples.now
        ))
        #expect(Set(store.controlCenter.archived.map(\.status)) == Set([.exited, .ended, .failed]))
        #expect(store.controlCenter.archived.count == 3)
        #expect(store.controlCenter.machines[0].sessions.contains { $0.id == boundary.id })
        #expect(store.controlCenter.machines[0].sessions.contains { $0.status == .done })
    }

    @Test func endWaitsForConfirmationAndRemovesOnlyTheConfirmedArchiveRow() async {
        let services = FakeAppServices()
        let row = Samples.row(bib: "02", name: "old", status: .ended, cwd: "/srv")
        var other = row
        other.hostID = Samples.build
        var state = Samples.controlCenter
        state.archived = [row, other]
        let store = AppStore(services: services, controlCenter: state)
        var confirmation: SessionRow?
        store.run(.kill(row), tools: FakeMachineTools(), open: { _, _ in }, kill: { confirmation = $0 }, rename: { _ in })
        #expect(confirmation == row)
        #expect(services.calls.isEmpty)
        services.failure = HelperError.closed
        await store.kill(row)
        #expect(store.controlCenter.archived == [row, other])
        services.failure = nil
        await store.kill(row)
        #expect(store.controlCenter.archived == [other])
    }

    @Test func terminalOnlyHeaderAppearsOnlyForMixedGroupsWithStableOrder() {
        let first = Samples.row(bib: "01", name: "native", cwd: "/srv")
        let second = Samples.row(bib: "02", name: "native two", cwd: "/srv")
        let shell = Samples.row(bib: nil, name: "shell", agentName: "Shell", cwd: "/srv")
        let outsideAgent = Samples.row(bib: nil, name: "desk OMP", agentName: "OMP", cwd: "/srv")
        var machine = Samples.controlCenter.machines[0]
        machine.sessions = [shell, first, outsideAgent, second]
        let mixed = RosterModel.make(machines: [machine], requests: []).groups[0]
        #expect(mixed.showsTerminalOnlyHeader)
        #expect(mixed.lines.map(\.row.id) == [first.id, second.id, shell.id, outsideAgent.id])
        #expect(mixed.terminalOnlyLines.map(\.row.id) == [shell.id, outsideAgent.id])
        for rows in [[first, second], [shell, outsideAgent], []] {
            machine.sessions = rows
            #expect(!RosterModel.make(machines: [machine], requests: []).groups[0].showsTerminalOnlyHeader)
        }
        machine.helper = .missing
        machine.sessions = [shell, Samples.row(bib: nil, name: "togo-2", agentName: "Shell", cwd: "/srv")]
        let plain = RosterModel.make(machines: [machine], requests: []).groups[0]
        #expect(plain.showsTerminalOnlyHeader)
        #expect(plain.togoLines.map(\.row.name) == ["togo-2"])
    }
}
