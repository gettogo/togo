import Foundation
import Testing
import TogoKit
import TogoTerminal
import UIKit
@testable import Togo

@MainActor struct WorkspaceTests {
    private let services = FakeAppServices()

    private func store(_ state: ControlCenterState = Samples.controlCenter) -> AppStore {
        AppStore(services: services, controlCenter: state)
    }

    @Test func closeLastTabReturnsToControlCenter() {
        let store = store()
        let terminals = TerminalPool()
        store.open(hostID: Samples.studio, sessionID: "s-01")
        store.open(hostID: Samples.studio, sessionID: "s-04")
        terminals.sync(with: store)
        #expect(Set(terminals.attached.map(\.sessionID)) == ["s-01", "s-04"])

        store.close(TabID(hostID: Samples.studio, sessionID: "s-04"))
        terminals.sync(with: store)
        #expect(store.workspaceOpen)
        #expect(store.workspace.selected?.sessionID == "s-01")
        #expect(services.stoppedTerminals.map(\.sessionID) == ["s-04"])

        store.perform(.detach)
        terminals.sync(with: store)
        #expect(!store.workspaceOpen)
        #expect(store.workspace.tabs.isEmpty)
        #expect(terminals.attached.isEmpty)
        #expect(services.stoppedTerminals.map(\.sessionID) == ["s-04", "s-01"])
    }

    @Test func commandNumberSelectsTab() {
        let store = store()
        for id in ["s-01", "s-04", "s-scratch"] {
            store.open(hostID: Samples.studio, sessionID: id)
        }

        store.perform(.session(2))
        #expect(store.workspace.selected?.sessionID == "s-04")
        store.selectTab(number: 1)
        #expect(store.workspace.selected?.sessionID == "s-01")
        store.selectTab(number: 3)
        #expect(store.workspace.selected?.sessionID == "s-scratch")

        store.selectTab(number: 9)
        store.selectTab(number: 0)
        #expect(store.workspace.selected?.sessionID == "s-scratch")
    }

    @Test func tabOffsetStopsAtTheEnds() {
        let store = store()
        for id in ["s-01", "s-04", "s-scratch"] {
            store.open(hostID: Samples.studio, sessionID: id)
        }

        store.selectTab(offset: 1)
        #expect(store.workspace.selected?.sessionID == "s-scratch")
        store.selectTab(offset: -1)
        #expect(store.workspace.selected?.sessionID == "s-04")
        store.selectTab(offset: -5)
        #expect(store.workspace.selected?.sessionID == "s-01")
    }

    @Test func incomingOnlyFromOtherSessions() {
        var state = Samples.controlCenter
        state.requests = [
            Samples.request(id: "r1", bib: "04", title: "Run npm test?"),
            Samples.request(id: "r2", bib: "01", title: "Edit routes.ts?"),
            Samples.request(id: "r3", bib: nil, title: "Allow network access?"),
        ]
        let store = store(state)

        store.open(hostID: Samples.studio, sessionID: "s-04")
        #expect(store.incomingRequests.map(\.id) == ["r2", "r3"])

        store.open(hostID: Samples.studio, sessionID: "s-01")
        #expect(store.incomingRequests.map(\.id) == ["r1", "r3"])
    }

    @Test func answeredIncomingRemoved() async {
        var state = Samples.controlCenter
        state.requests.append(Samples.request(id: "r2", bib: "01", title: "Edit routes.ts?"))
        let store = store(state)
        store.open(hostID: Samples.studio, sessionID: "s-01")
        #expect(store.incomingRequests.map(\.id) == ["r1"])

        await store.answer("r1", .approve)
        #expect(store.incomingRequests.isEmpty)
        #expect(services.calls == [.answer(Samples.studio, "r1", .approve)])

        services.failure = HelperError.closed
        store.open(hostID: Samples.studio, sessionID: "s-04")
        await store.answer("r2", .approve)
        #expect(store.incomingRequests.map(\.id) == ["r2"])
        #expect(store.answerFailures["r2"] != nil)
    }

    @Test func linkPhasesReachTheirTab() async {
        services.terminalPhases = [.connecting, .live(transport: "Mosh", note: nil)]
        let store = store()
        let terminals = TerminalPool()
        store.open(hostID: Samples.studio, sessionID: "s-01")
        terminals.sync(with: store)

        for _ in 0..<50 where store.workspace.tabs.first?.link != .live(transport: "Mosh", note: nil) {
            await Task.yield()
        }
        #expect(store.workspace.tabs.first?.link == .live(transport: "Mosh", note: nil))
    }

    @Test func changingTransportRestartsTheTerminal() {
        let store = store()
        let terminals = TerminalPool()
        store.open(hostID: Samples.studio, sessionID: "s-01")
        terminals.sync(with: store)
        let first = terminals.handle(for: TabID(hostID: Samples.studio, sessionID: "s-01"))

        store.open(hostID: Samples.studio, sessionID: "s-01", transport: .ssh)
        terminals.sync(with: store)
        #expect(services.stoppedTerminals.map(\.sessionID) == ["s-01"])
        #expect(terminals.handle(for: TabID(hostID: Samples.studio, sessionID: "s-01")) !== first)
        #expect(store.workspace.tabs.first?.link == .connecting)
    }

    @Test func copyLastLinesCapturesFiveHundredJoined() async throws {
        let store = store()
        store.open(hostID: Samples.studio, sessionID: "s-01")
        let tab = try #require(store.selectedTab)

        let result = await store.copyLastLines(of: tab)
        #expect(services.calls == [.capture(Samples.studio, "s-01", 500)])
        #expect(result == WorkspaceCopy.copied)

        services.failure = HelperError.closed
        #expect(await store.copyLastLines(of: tab) == WorkspaceCopy.copyFailed("Studio"))
    }

    @Test func endingASessionClosesItsTabOnlyWhenTheKillArrives() async throws {
        let store = store()
        store.open(hostID: Samples.studio, sessionID: "s-01")
        store.open(hostID: Samples.studio, sessionID: "s-04")
        let tab = try #require(store.selectedTab)

        services.failure = HelperError.closed
        #expect(await store.end(tab) != nil)
        #expect(store.workspace.tabs.map(\.sessionID) == ["s-01", "s-04"])

        services.failure = nil
        #expect(await store.end(tab) == nil)
        #expect(store.workspace.tabs.map(\.sessionID) == ["s-01"])
    }

    @Test func headerWordFollowsTheLink() {
        #expect(LinkPhase.live(transport: "Mosh", note: nil).word == "Mosh")
        #expect(LinkPhase.reconnecting(2).word == "Reconnecting")
        #expect(LinkPhase.waking("MacBook").word == "Waking")
        #expect(WorkspaceCopy.fallback(transport: "SSH", note: "mosh-server not found on mini")
            == "SSH, mosh-server not found on mini")
    }

    @Test func overlayShowsUntilLiveAndExplainsAnOfflineMachine() {
        var machine = Samples.controlCenter.machines[0]
        #expect(ConnectingOverlay.Kind(link: .connecting, machine: machine) == .connecting("Studio"))
        #expect(ConnectingOverlay.Kind(link: .waking("Studio"), machine: machine) == .waking("Studio"))
        #expect(ConnectingOverlay.Kind(link: .live(transport: "SSH", note: nil), machine: machine) == nil)
        #expect(ConnectingOverlay.Kind(link: .reconnecting(1), machine: machine) == nil)
        #expect(ConnectingOverlay.Kind(link: .failed("Connection refused"), machine: machine)
            == .failed(machine: "Studio", reason: "Connection refused"))

        machine.reachability = .offline(since: Samples.now, reason: "Asleep")
        #expect(ConnectingOverlay.Kind(link: .failed("Timed out"), machine: machine) == .offline("Studio"))
    }

    @Test func killFromTheListClosesTheTabAndReleasesItsTerminal() async {
        let store = store()
        let terminals = TerminalPool()
        store.open(hostID: Samples.studio, sessionID: "s-01")
        store.open(hostID: Samples.studio, sessionID: "s-04")
        store.workspaceOpen = false
        terminals.sync(with: store)

        await store.kill(Samples.controlCenter.machines[0].sessions[0])
        terminals.sync(with: store)
        #expect(store.workspace.tabs.map(\.sessionID) == ["s-04"])
        #expect(services.stoppedTerminals.map(\.sessionID) == ["s-01"])
        #expect(store.workspace.ended == nil)
    }

    @Test func sessionGoneFromItsMachineClosesTheTab() {
        let store = store()
        let terminals = TerminalPool()
        store.open(hostID: Samples.studio, sessionID: "s-01")
        store.open(hostID: Samples.studio, sessionID: "s-04")
        terminals.sync(with: store)
        let sessions = Samples.controlCenter.machines[0].sessions

        store.apply(HostViewSnapshot(
            hostID: Samples.studio, reachability: .offline(since: Samples.now, reason: "Asleep"),
            helper: .present(version: "0.3.0", live: true), sessions: [], requests: [], at: Samples.now
        ))
        #expect(store.workspace.tabs.count == 2)

        store.apply(HostViewSnapshot(
            hostID: Samples.studio, reachability: .online, helper: .present(version: "0.3.0", live: true),
            sessions: sessions.filter { $0.id != "s-04" }, requests: [], at: Samples.now
        ))
        terminals.sync(with: store)
        #expect(store.workspace.tabs.map(\.sessionID) == ["s-01"])
        #expect(store.workspace.ended == "04 ended")
        #expect(services.stoppedTerminals.map(\.sessionID) == ["s-04"])
    }

    @Test func poolKeepsTheFourMostRecentTerminals() {
        let store = store()
        let terminals = TerminalPool()
        let ids = ["s-a", "s-b", "s-c", "s-d", "s-e"]
        for id in ids {
            store.open(hostID: Samples.studio, sessionID: id)
            terminals.sync(with: store)
        }
        #expect(Set(terminals.attached.map(\.sessionID)) == ["s-b", "s-c", "s-d", "s-e"])
        #expect(services.stoppedTerminals.map(\.sessionID) == ["s-a"])
        #expect(store.workspace.tabs.map(\.sessionID) == ids)

        store.select(TabID(hostID: Samples.studio, sessionID: "s-a"))
        terminals.sync(with: store)
        #expect(Set(terminals.attached.map(\.sessionID)) == ["s-a", "s-c", "s-d", "s-e"])
        #expect(services.stoppedTerminals.map(\.sessionID) == ["s-a", "s-b"])
        #expect(store.workspace.tabs.first?.link == .connecting)
    }

    @Test func sameSessionIDOnTwoMachinesIsTwoTabs() async {
        var state = Samples.controlCenter
        state.machines[1].sessions.append(Samples.row(hostID: Samples.build, bib: "01", name: "api", cwd: "/home/sam/api"))
        let store = store(state)
        let terminals = TerminalPool()
        let onStudio = TabID(hostID: Samples.studio, sessionID: "s-01")
        let onBuild = TabID(hostID: Samples.build, sessionID: "s-01")
        store.open(hostID: Samples.studio, sessionID: "s-01")
        store.open(hostID: Samples.build, sessionID: "s-01")
        terminals.sync(with: store)
        #expect(store.workspace.tabs.map(\.id) == [onStudio, onBuild])
        #expect(terminals.attached == [onStudio, onBuild])

        store.selectTab(number: 1)
        #expect(store.workspace.selected == onStudio)
        store.selectTab(number: 2)
        #expect(store.workspace.selected == onBuild)

        await store.kill(Samples.controlCenter.machines[0].sessions[0])
        terminals.sync(with: store)
        #expect(store.workspace.tabs.map(\.id) == [onBuild])
        #expect(services.stoppedTerminals == [onStudio])

        store.open(hostID: Samples.studio, sessionID: "s-01")
        store.close(onBuild)
        terminals.sync(with: store)
        #expect(store.workspace.tabs.map(\.id) == [onStudio])
        #expect(store.workspace.selected == onStudio)
    }

    @Test func incomingCardFromTheSameSessionIDOnAnotherMachineStillShows() {
        var state = Samples.controlCenter
        state.requests = [Samples.request(id: "r9", hostID: Samples.build, bib: "04", title: "Push to main?")]
        let store = store(state)
        store.open(hostID: Samples.studio, sessionID: "s-04")
        #expect(store.incomingRequests.map(\.id) == ["r9"])
    }

    @Test func pastedImageAlwaysReachesTheComposeBar() {
        let tab = TabID(hostID: Samples.studio, sessionID: "s-04")
        let without = store()
        without.pasteImage(Data([1, 2, 3]), into: tab)
        #expect(without.composing == tab)
        #expect(without.pastedImage?.data == Data([1, 2, 3]))
        #expect(without.imageSupport(for: Samples.studio) == .unsupported)

        var state = Samples.controlCenter
        state.machines[0].capabilities = [Capability.upload]
        let with = store(state)
        with.pasteImage(Data([1, 2, 3]), into: tab)
        #expect(with.composing == tab)
        #expect(with.pastedImage?.data == Data([1, 2, 3]))
        #expect(with.imageSupport(for: Samples.build) == .unsupported)
    }

    @Test func composeKeyTogglesTheBar() {
        let store = store()
        let tab = TabID(hostID: Samples.studio, sessionID: "s-01")
        store.toggleCompose(tab)
        #expect(store.composing == tab)
        store.toggleCompose(tab)
        #expect(store.composing == nil)
    }

    /// Every window runs the same store; the snapshot stream still has one reader.
    @Test func secondWindowDoesNotStartASecondStreamReader() async {
        let store = store(ControlCenterState())
        let first = Task { await store.run() }
        await Task.yield()
        let second = Task { await store.run() }
        await second.value
        services.publish(HostViewSnapshot(
            hostID: Samples.studio, reachability: .online, helper: .present(version: "0.3.0", live: true),
            sessions: [], requests: [], at: Samples.now
        ))
        services.finish()
        await first.value
        #expect(store.controlCenter.machines.map(\.id) == [Samples.studio])
    }

    @Test func windowsSharingThePoolMakeOneConnectionPerTab() {
        let store = store()
        let terminals = TerminalPool()
        store.open(hostID: Samples.studio, sessionID: "s-04")
        terminals.sync(with: store)
        terminals.sync(with: store)
        #expect(services.terminalsMade == [TabID(hostID: Samples.studio, sessionID: "s-04")])
    }

    @Test func forgetMachineClearsItsCardsArchiveAndTabs() {
        var state = Samples.controlCenter
        state.archived = [Samples.row(bib: "03", name: "old", status: .done, cwd: "/")]
        let store = store(state)
        let terminals = TerminalPool()
        store.open(hostID: Samples.studio, sessionID: "s-04")
        store.open(hostID: Samples.build, sessionID: "s-02")
        terminals.sync(with: store)
        store.forgetMachine(Samples.studio)
        terminals.sync(with: store)
        #expect(store.controlCenter.machines.map(\.id) == [Samples.build])
        #expect(store.controlCenter.requests.isEmpty)
        #expect(store.controlCenter.archived.isEmpty)
        #expect(store.workspace.tabs.map(\.hostID) == [Samples.build])
        #expect(terminals.attached == [TabID(hostID: Samples.build, sessionID: "s-02")])
        #expect(services.stoppedTerminals == [TabID(hostID: Samples.studio, sessionID: "s-04")])
    }

    /// A machine that cannot take images says so in the compose bar instead of eating the paste.
    @Test func imagePastedWhereUploadsDoNotWorkShowsTheNotice() async throws {
        let png = try #require(UIGraphicsImageRenderer(size: CGSize(width: 2, height: 2)).image { _ in }.pngData())
        let old = ImagePasteFlow(support: .unsupported, upload: { _, _ in "" }, paste: { _ in })
        await old.begin(png)
        #expect(old.phase.message == "This machine\u{2019}s helper can\u{2019}t receive images yet.")
        let plain = ImagePasteFlow(support: .none, upload: { _, _ in "" }, paste: { _ in })
        await plain.begin(png)
        #expect(plain.phase.message == "This machine has no Togo helper, so images can\u{2019}t be sent.")
    }

    /// Only a tab the user opens, or Try again, may ask a sign-in question.
    @Test func onlyUserStartedAttachesMayAsk() {
        let store = store()
        let terminals = TerminalPool()
        store.open(hostID: Samples.studio, sessionID: "s-04")
        terminals.sync(with: store)
        let tab = store.workspace.tabs[0]
        terminals.restart(tab, store: store)
        terminals.restartTabs(on: Samples.studio)
        #expect(services.terminalsUserStarted == [true, true, false])
    }

    @Test func removingAMachineClosesItsSignIn() {
        let store = store()
        store.sheet = .signIn(Samples.studio)
        store.forgetMachine(Samples.studio)
        #expect(store.sheet == nil)
    }
}
