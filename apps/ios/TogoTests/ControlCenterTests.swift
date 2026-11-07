import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct ControlCenterTests {
    private let services = FakeAppServices()

    private func managedRow(muted: Bool = false) -> SessionRow {
        var row = Samples.row(bib: "04", name: "app", status: .needsYou, cwd: "/Users/sam/src/app")
        row.muted = muted
        return row
    }

    private func unmanagedRow() -> SessionRow {
        Samples.row(bib: nil, name: "scratch", agentName: "Shell", status: .idle, cwd: "/Users/sam")
    }

    @Test func answerConflictClearsCard() async {
        let store = AppStore(services: services, controlCenter: Samples.controlCenter)
        services.failure = HelperError.conflict("answered in the terminal")

        await store.answer("r1", .approve)
        #expect(store.controlCenter.requests.map(\.phase) == [.done(.answeredElsewhere)])
        #expect(RequestCard.Outcome.answeredElsewhere.prompt.label == "Answered at the desk")
        #expect(store.notice == nil)

        await store.answer("r1", .deny)
        #expect(services.calls == [.answer(Samples.studio, "r1", .approve)])

        store.apply(HostViewSnapshot(
            hostID: Samples.studio, reachability: .online, helper: .present(version: "0.3.0", live: true),
            sessions: Samples.controlCenter.machines[0].sessions, requests: [], at: Samples.now
        ))
        // The card holds its outcome until the list collapses it.
        #expect(store.controlCenter.requests.map(\.phase) == [.done(.answeredElsewhere)])
        store.dismissAnswered(["r1"])
        #expect(store.controlCenter.requests.isEmpty)
        #expect(store.notice == nil)
    }

    @Test func answerFailureStaysInItsCardAndLeavesOtherNoticesAlone() async {
        var state = Samples.controlCenter
        state.requests.append(Samples.request(id: "r2", bib: "01", title: "Delete dist/?"))
        let store = AppStore(services: services, controlCenter: state)
        services.failure = HelperError.closed

        await store.rename(state.machines[0].sessions[0], to: "routes")
        let renameNotice = store.notice
        #expect(renameNotice?.isError == true)

        await store.answer("r1", .approve)
        #expect(store.notice == renameNotice)
        #expect(store.answerFailures == ["r1": "Couldn't reach Studio. Try again."])
        #expect(store.controlCenter.requests.map(\.phase) == [.open, .open])

        services.failure = nil
        await store.answer("r1", .approve)
        #expect(store.answerFailures.isEmpty)
        #expect(store.notice == renameNotice)
    }

    @Test func answerFailureGoesWithItsCard() async {
        let store = AppStore(services: services, controlCenter: Samples.controlCenter)
        services.failure = HelperError.closed
        await store.answer("r1", .approve)
        store.apply(HostViewSnapshot(
            hostID: Samples.studio, reachability: .online, helper: .present(version: "0.3.0", live: true),
            sessions: [], requests: [], at: Samples.now
        ))
        #expect(store.answerFailures.isEmpty)
    }

    @Test func unmanagedRowHasNoKill() {
        let actions = SessionActions.actions(for: unmanagedRow())
        #expect(!actions.available.contains(.kill))
        #expect(!actions.available.contains(.rename))
        #expect(!actions.available.contains(.mute))
        #expect(actions.available == [.open, .connectWith, .copyPath])
        #expect(actions.note == "Started outside Togo")
    }

    @Test func killAsksFirst() {
        let row = managedRow()
        let actions = SessionActions.actions(for: row)
        #expect(actions.available == [.open, .connectWith, .rename, .mute, .copyPath, .kill])
        #expect(actions.disabled.isEmpty)
        #expect(actions.note == nil)
        #expect(SessionAction.allCases.filter(\.needsConfirmation) == [.kill])

        let confirmation = SessionActions.killConfirmation(for: row)
        #expect(confirmation.title == "End 04 Claude Code?")
        #expect(confirmation.message == "The agent stops and its screen is gone.")
    }

    @Test func offlineMachineDisablesHelperActionsWithReason() {
        var machine = Samples.controlCenter.machines[1]
        machine.lastSeen = Samples.now.addingTimeInterval(-720)
        let row = machine.sessions[0]
        let actions = SessionActions.actions(for: row, on: machine)
        #expect(actions.available == [.open, .connectWith, .rename, .mute, .copyPath, .kill])
        #expect(actions.disabled == [.rename, .mute, .kill])
        #expect(actions.note == "Build box is offline")
    }

    @Test func plainSshSessionsTogoStartedCanBeKilledAndRenamed() {
        var machine = Samples.controlCenter.machines[0]
        machine.helper = .missing
        var row = unmanagedRow()
        row.name = "togo-2"
        #expect(SessionActions.actions(for: row, on: machine).available == [.open, .connectWith, .rename, .copyPath, .kill])
        row.name = "work"
        #expect(SessionActions.actions(for: row, on: machine).available == [.open, .connectWith, .copyPath])
    }

    @Test func muteActionReadsUnmuteWhenMuted() {
        #expect(SessionAction.mute.title(for: managedRow(muted: false)) == "Mute")
        #expect(SessionAction.mute.title(for: managedRow(muted: true)) == "Unmute")
    }

    @Test func connectWithListsOnlyAvailable() {
        #expect(ConnectWithMenu.transports(etAvailable: false) == [.auto, .ssh, .mosh])
        #expect(ConnectWithMenu.transports(etAvailable: true) == [.auto, .ssh, .mosh, .et])
    }

    @Test func openWithTransportRecordsItOnTheTab() {
        let store = AppStore(services: services, controlCenter: Samples.controlCenter)
        store.open(hostID: Samples.studio, sessionID: "s-01", transport: .mosh)
        #expect(store.workspace.tabs.map(\.transport) == [.mosh])
        store.open(hostID: Samples.studio, sessionID: "s-04")
        #expect(store.workspace.tabs.map(\.transport) == [.mosh, nil])
    }

    @Test func offlineCaptionCountsMinutesSinceLastSeen() {
        let now = Samples.now
        let offline = Reachability.offline(since: now, reason: "Asleep")
        #expect(RosterSection.caption(offline, helper: .present(version: "0.3.0", live: true),
                                      lastSeen: now.addingTimeInterval(-720), now: now) == "Offline, seen 12 min ago")
        #expect(RosterSection.caption(.waking, helper: .checking, lastSeen: nil, now: now) == "Waking")
        #expect(RosterSection.caption(.online, helper: .missing, lastSeen: now, now: now) == "No helper")
        #expect(RosterSection.caption(.online, helper: .present(version: "0.3.0", live: true),
                                      lastSeen: now, now: now) == nil)
    }

    @Test func approveIsTheApprovingOptionAndTheRestGoUnderMore() {
        var card = Samples.request(id: "r2", bib: "04", title: "Edit package.json?")
        card.options = [
            ApprovalOption(n: 1, label: "Yes", role: .approve),
            ApprovalOption(n: 2, label: "Yes, and don't ask again", role: .approveAlways),
            ApprovalOption(n: 3, label: "No, and tell Claude what to do", role: .deny),
        ]
        let choices = RequestChoices(card.options)
        #expect(choices.approve?.n == 1)
        #expect(choices.deny?.n == 3)
        #expect(choices.more.map(\.n) == [2])
    }
}
