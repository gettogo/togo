import Foundation
import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor struct ReviewEntryTests {
    private static let everything: Set<String> = [Capability.git, Capability.gitWrite, Capability.ports]

    private func subject(_ capabilities: Set<String> = everything) -> ReviewSubject {
        ReviewSubject(
            hostID: Samples.studio, sessionID: "s-01", bib: "01", title: "api", cwd: "/Users/sam/src/api",
            managed: true, capabilities: capabilities
        )
    }

    private func status(_ count: Int) -> GitStatus {
        var status = ReviewFixtures.status
        status.files = (0..<count).map { ReviewFixtures.file("src/file\($0).ts", ".", "M") }
        return status
    }

    // MARK: Header

    @Test func headerCountHiddenAtZero() async {
        let model = ReviewHeaderModel(services: FakeReviewServices(status: status(0)))
        await model.select(subject())
        #expect(model.changes == 0)
        #expect(model.changesLabel == nil)

        let one = ReviewHeaderModel(services: FakeReviewServices(status: status(1)))
        await one.select(subject())
        #expect(one.changesLabel == "1 change")

        let twelve = ReviewHeaderModel(services: FakeReviewServices(status: status(12)))
        await twelve.select(subject())
        #expect(twelve.changesLabel == "12 changes")

        var cut = status(3)
        cut.truncated = true
        let many = ReviewHeaderModel(services: FakeReviewServices(status: cut))
        await many.select(subject())
        #expect(many.changesLabel == "3+ changes")
    }

    @Test func headerHiddenWithoutGit() async {
        let services = FakeReviewServices(status: status(3))
        let model = ReviewHeaderModel(services: services)
        await model.select(subject([]))
        #expect(model.changesLabel == nil)
        #expect(!model.hasPorts)
        #expect(services.calls.isEmpty)
    }

    @Test func headerRefreshesOnStatusChange() async {
        let services = FakeReviewServices(status: status(3))
        let model = ReviewHeaderModel(services: services)
        await model.select(subject())
        #expect(model.changesLabel == "3 changes")
        let reads = services.calls.filter { $0 == .status }.count

        await model.statusChanged(to: .working)
        #expect(services.calls.filter { $0 == .status }.count == reads)

        services.editBehindBack("src/file0.ts")
        for settled: SessionStatus in [.idle, .done, .needsYou] {
            await model.statusChanged(to: settled)
        }
        #expect(services.calls.filter { $0 == .status }.count == reads + 3)
    }

    @Test func headerRefreshesWhenTheReviewCloses() async {
        let services = FakeReviewServices(status: status(3))
        let model = ReviewHeaderModel(services: services)
        await model.select(subject())
        #expect(model.changesLabel == "3 changes")
        let all = services.current.files
        _ = try? await services.discard(
            subject(), paths: all.map(\.path), seen: Dictionary(uniqueKeysWithValues: all.map { ($0.path, GitSeen($0)) })
        )
        await model.reviewChanged(open: true)
        #expect(model.changesLabel == "3 changes")
        await model.reviewChanged(open: false)
        #expect(model.changesLabel == nil)
    }

    @Test func previewButtonOnlyWithPorts() async {
        let services = FakeReviewServices()
        let model = ReviewHeaderModel(services: services)
        await model.select(subject())
        #expect(model.hasPorts)

        services.portList = []
        await model.refresh()
        #expect(!model.hasPorts)

        services.portList = ReviewFixtures.ports
        let withoutCapability = ReviewHeaderModel(services: services)
        await withoutCapability.select(subject([Capability.git]))
        #expect(!withoutCapability.hasPorts)
    }

    // MARK: Control center

    @Test func rowMenuNeedsGitCapability() {
        var machine = Samples.controlCenter.machines[0]
        let row = machine.sessions[0]

        machine.capabilities = [Capability.events]
        #expect(SessionActions.actions(for: row, on: machine).review.isEmpty)

        machine.capabilities = [Capability.git]
        var actions = SessionActions.actions(for: row, on: machine)
        #expect(actions.review == [.changes, .files])
        #expect(actions.allows(.changes))

        machine.capabilities = [Capability.git, Capability.ports]
        #expect(SessionActions.actions(for: row, on: machine).review == [.changes, .files, .ports])

        machine.helper = .missing
        machine.capabilities = []
        actions = SessionActions.actions(for: row, on: machine)
        #expect(actions.review == [.changes, .files, .ports])
        #expect(!actions.allows(.changes) && !actions.allows(.files) && !actions.allows(.ports))
        #expect(actions.reviewNote == "Changes, files and ports need the Togo helper on Studio.")
        // The other actions keep their own lists.
        #expect(!actions.available.contains(.changes))
    }

    @Test func rowMenuOpensReviewOnTheChosenTab() {
        var state = Samples.controlCenter
        state.machines[0].capabilities = [Capability.git, Capability.ports]
        let store = AppStore(services: FakeAppServices(), controlCenter: state)
        store.review(state.machines[0].sessions[0], tab: .files)
        #expect(store.reviewing?.tab == .files)
        #expect(store.reviewing?.subject.canRead == true)
    }

    // MARK: Root

    @Test func portsTabHiddenWithoutCapability() {
        #expect(ReviewRoot.tabs(for: subject()) == [.changes, .history, .files, .ports])
        #expect(ReviewRoot.tabs(for: subject([Capability.git])) == [.changes, .history, .files])
    }

    @Test func regularWidthUsesSplitView() {
        #expect(ReviewRoot.layout(for: .regular) == .split)
        #expect(ReviewRoot.layout(for: .compact) == .stack)
        #expect(ReviewRoot.layout(for: nil) == .stack)
    }

    @Test func workingTreeRouteGoesToFilesRoot() {
        var navigation = ReviewNavigation(tab: .history)
        navigation.open(.commit(ReviewFixtures.log[0]), layout: .stack)
        navigation.open(.folder(path: "", ref: ReviewFixtures.log[0].hash), layout: .stack)
        #expect(navigation.path.count == 2)
        navigation.open(.folder(path: "", ref: nil), layout: .stack)
        #expect(navigation.tab == .files)
        #expect(navigation.path.isEmpty)
    }

    @Test func splitReplacesTheDetail() {
        var navigation = ReviewNavigation(tab: .changes)
        navigation.open(.diff(.worktree(path: "src/router.ts")), layout: .split)
        navigation.open(.diff(.worktree(path: "src/store.ts")), layout: .split)
        #expect(navigation.detail == .diff(.worktree(path: "src/store.ts")))
        #expect(navigation.path.isEmpty)
        navigation.open(.folder(path: "src", ref: nil), layout: .split)
        #expect(navigation.path == [.folder(path: "src", ref: nil)])
    }

    // MARK: Sending

    @Test func sendToSessionPastesWithoutEnter() throws {
        let handle = TerminalHandle(
            engineView: UIViewController(), phases: AsyncStream { $0.finish() }, onStop: {}
        )
        var pasted: [String] = []
        var enters = 0
        handle.paste = { pasted.append($0) }
        handle.pressEnter = { enters += 1 }

        let pool = HandleBox(handle)
        let send = try #require(ReviewRoot.sendToSession { pool.handle })
        send("src/router.ts:12: check\u{1b}[31m this\r\nfirst")
        #expect(pasted == ["src/router.ts:12: check[31m this first"])
        #expect(enters == 0)

        // A reconnect replaces the handle; the next comment goes to the new one.
        let replacement = TerminalHandle(engineView: UIViewController(), phases: AsyncStream { $0.finish() }, onStop: {})
        var replaced: [String] = []
        replacement.paste = { replaced.append($0) }
        pool.handle = replacement
        send("again")
        #expect(replaced == ["again"])
        #expect(ReviewRoot.sendToSession { nil } == nil)
    }

    // MARK: Shortcuts

    private func storeWithReview() -> AppStore {
        var state = Samples.controlCenter
        state.machines[0].capabilities = [Capability.git]
        let store = AppStore(services: FakeAppServices(hosts: Samples.hosts), controlCenter: state)
        store.open(hostID: Samples.studio, sessionID: "s-01")
        store.open(hostID: Samples.studio, sessionID: "s-04")
        store.select(TabID(hostID: Samples.studio, sessionID: "s-01"))
        store.openReview(.changes)
        return store
    }

    @Test func commandWClosesTheReviewNotTheTab() {
        let store = storeWithReview()
        #expect(store.reviewing != nil)
        store.perform(.detach)
        #expect(store.reviewing == nil)
        #expect(store.workspace.tabs.count == 2)
        #expect(store.workspaceOpen)
    }

    @Test func commandOneWaitsForTheReview() {
        let store = storeWithReview()
        let selected = store.workspace.selected
        store.showTab(number: 2)
        store.perform(.session(2))
        store.requestFind()
        store.startNewSession()
        #expect(store.workspace.selected == selected)
        #expect(store.findRequests == 0)
        #expect(store.reviewing?.tab == .changes)
    }

    // MARK: When the session or machine goes

    private func reviewFromList() -> AppStore {
        var state = Samples.controlCenter
        state.machines[0].capabilities = Self.everything
        let store = AppStore(services: FakeAppServices(), controlCenter: state)
        store.review(state.machines[0].sessions[0], tab: .changes)
        return store
    }

    private func snapshot(_ store: AppStore, reachability: Reachability = .online, dropping id: String? = nil) -> HostViewSnapshot {
        let machine = store.controlCenter.machines[0]
        return HostViewSnapshot(
            hostID: machine.id, reachability: reachability, helper: .present(version: "0.3.0", live: true),
            sessions: machine.sessions.filter { $0.id != id }, requests: [], at: Date(), capabilities: Self.everything
        )
    }

    @Test func forgettingTheMachineClosesTheReview() {
        let store = storeWithReview()
        store.forgetMachine(Samples.studio)
        #expect(store.reviewing == nil)
        #expect(store.sheet == nil)
    }

    @Test func theSessionEndingClosesTheReview() {
        let store = reviewFromList()
        #expect(store.reviewing?.subject.sessionID == "s-01")
        store.apply(snapshot(store, dropping: "s-04"))
        #expect(store.reviewing != nil, "Another session ending leaves the review open")
        store.apply(snapshot(store, dropping: "s-01"))
        #expect(store.reviewing == nil)
    }

    @Test func theSessionEndingClosesTheReviewOverItsTab() {
        let store = storeWithReview()
        store.closeEndedTabs(on: Samples.studio, listed: ["s-04"])
        #expect(store.reviewing == nil)
    }

    @Test func losingTheMachineForGoodClosesTheReview() {
        let store = reviewFromList()
        store.apply(snapshot(store, reachability: .offline(since: Date(), reason: "asleep")))
        #expect(store.reviewing != nil, "A machine that is only asleep keeps the review")
        store.apply(snapshot(store, reachability: .failed("The host key changed.")))
        #expect(store.reviewing == nil)
    }

    @Test func aCommitMessageKeepsTheReviewReadOnly() {
        let store = reviewFromList()
        store.reviewHasDraft = true
        store.forgetMachine(Samples.studio)
        #expect(store.reviewing != nil)
        #expect(store.reviewEnded == "Studio was removed from Togo.")
        // Opening a review again starts clean.
        store.closeReview()
        var state = Samples.controlCenter
        state.machines[0].capabilities = Self.everything
        store.controlCenter = state
        store.review(state.machines[0].sessions[0], tab: .changes)
        #expect(store.reviewEnded == nil)
        #expect(!store.reviewHasDraft)
    }

    @Test func endedReviewTurnsWritesOff() async {
        let services = FakeReviewServices()
        let changes = ChangesModel(subject: subject(), services: services, machine: "Studio")
        await changes.refresh()
        changes.message = "Fix the router"
        #expect(changes.canWrite)
        changes.ended = "This session has ended."
        #expect(!changes.canWrite)
        #expect(!changes.canCommit)
        #expect(changes.message == "Fix the router")
        #expect(changes.readOnlyNote == "This session has ended. Nothing can be changed from here now; your commit message is kept so you can copy it.")

        let diff = DiffModel(subject: subject(), services: services, source: .worktree(path: "src/handlers/items.ts"), sendToSession: nil)
        await diff.load()
        #expect(!diff.actions.isEmpty)
        diff.ended = "This session has ended."
        #expect(diff.actions.isEmpty)
    }

    @Test func switchingTabsClosesTheDetail() {
        var navigation = ReviewNavigation(tab: .changes)
        navigation.open(.diff(.worktree(path: "src/router.ts")), layout: .split)
        navigation.select(.files)
        #expect(navigation.detail == nil)
    }

    @Test func shortcutsOpenTheSelectedSession() {
        var state = Samples.controlCenter
        state.machines[0].capabilities = [Capability.git]
        let store = AppStore(services: FakeAppServices(), controlCenter: state)
        #expect(!store.canOpenReview(.changes))
        store.open(hostID: Samples.studio, sessionID: "s-01")
        #expect(store.canOpenReview(.changes))
        #expect(!store.canOpenReview(.ports))
        store.openReview(.changes)
        #expect(store.reviewing?.tab == .changes)
        #expect(store.reviewing?.subject.sessionID == "s-01")
    }
}

/// Stands in for the terminal pool: which handle a tab has right now.
@MainActor private final class HandleBox {
    var handle: TerminalHandle?

    init(_ handle: TerminalHandle?) {
        self.handle = handle
    }
}
