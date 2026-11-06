import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct LifecycleTests {
    private static let launchCaps: Set<String> = [Capability.events, Capability.launch, Capability.lifecycle]

    private func session(_ status: SessionStatus, _ exit: SessionExit?) -> Session {
        Session(
            id: "s_abcdefghij", bib: "01", name: "Claude Code", agent: "claude", cwd: "/Users/sam/src/app",
            createdAt: Samples.now, status: status, activity: nil, attachedClients: 0, managed: true,
            command: ["claude"], exit: exit
        )
    }

    private func row(_ status: SessionStatus, _ exit: SessionExit? = nil, managed: Bool = true) -> SessionRow {
        var row = Samples.row(bib: "01", name: "app", status: status, cwd: "/Users/sam/src/app")
        row.exit = exit
        row.managed = managed
        return row
    }

    private func machine(
        _ rows: [SessionRow], caps: Set<String> = launchCaps, reachability: Reachability = .online,
        helper: HelperState = .present(version: "0.4.0", live: true)
    ) -> MachineSection {
        MachineSection(
            id: Samples.studio, name: "Studio", symbol: .desktop, reachability: reachability, helper: helper,
            sessions: rows, lastSeen: Samples.now, capabilities: caps
        )
    }

    @Test func statusWordsForEachExit() {
        let cases: [(SessionStatus, SessionExit?, String, String?)] = [
            (.ended, SessionExit(code: 0, reason: .exit), "Ended", nil),
            (.ended, SessionExit(code: 130, reason: .interrupted), "Ended", nil),
            (.failed, SessionExit(code: 1, reason: .exit), "Stopped with an error", "Exited with code 1"),
            (.failed, SessionExit(code: nil, reason: .error, detail: "panic"), "Stopped with an error", nil),
            (.failed, SessionExit(code: nil, reason: .usageLimit, detail: "Usage limit reached"), "Hit a usage limit", nil),
            (.ended, SessionExit(code: nil, reason: .restart), "Ended by restart", nil),
            (.failed, nil, "Stopped with an error", nil),
            (.ended, nil, "Ended", nil),
        ]
        for (status, exit, word, detail) in cases {
            #expect(LifecycleCopy.status(session(status, exit)) == word, "\(String(describing: exit))")
            #expect(exit.flatMap(LifecycleCopy.detail) == detail, "\(String(describing: exit))")
        }
        #expect(LifecycleCopy.status(session(.working, nil)) == "Working")
        #expect(StatusWord.label(.failed) == "Stopped with an error")
        #expect(StatusWord.label(.exited) == "Ended")
        #expect(LifecycleCopy.line(status: .failed, exit: SessionExit(code: 1, reason: .exit))
            == "Stopped with an error · Exited with code 1")
        #expect(LifecycleCopy.line(status: .working, exit: nil) == nil)
        #expect(StatusWord.label(.failed, exit: SessionExit(code: nil, reason: .usageLimit)) == "Hit a usage limit")
        var failed = row(.failed, SessionExit(code: 1, reason: .exit))
        failed.bib = "04"
        #expect(failed.spokenSummary == "04, app, Stopped with an error, in ~/src/app")
    }

    @Test func endedAndFailedCountAsDone() {
        for status in [SessionStatus.done, .exited, .ended, .failed] {
            #expect(SessionFilter.done.admits(status), "\(status)")
            #expect(status.isFinished)
        }
        #expect(!SessionFilter.done.admits(.working))
        #expect(!SessionStatus.idle.isFinished)

        let store = AppStore(services: FakeAppServices())
        let old = Samples.now.addingTimeInterval(-(AppStore.archiveAfter + 60))
        var failed = row(.failed, SessionExit(code: 1, reason: .exit))
        failed.statusSince = old
        store.apply(HostViewSnapshot(
            hostID: Samples.studio, reachability: .online, helper: .present(version: "0.4.0", live: true),
            sessions: [failed], requests: [], at: Samples.now
        ))
        #expect(store.controlCenter.archived.map(\.id) == [failed.id])
    }

    @Test func mapperKeepsExitAndBackdatesFinishedRows() {
        var mapper = SnapshotMapper()
        let exit = SessionExit(code: 1, reason: .exit)
        var failed = session(.failed, exit)
        failed.createdAt = Samples.now.addingTimeInterval(-600)
        let snapshot = HostSnapshot(
            hostID: Samples.studio, reach: .online, helper: .present(version: "0.4.0"), sessions: [failed],
            approvals: [], lastSeen: Samples.now, capabilities: Self.launchCaps
        )
        let row = mapper.map(snapshot, noHelper: false, now: Samples.now).sessions[0]
        #expect(row.exit == exit)
        #expect(row.statusSince == failed.createdAt, "first seen finished: as old as it could be")
    }

    @Test func runAgainOnlyWhenEnded() {
        for status in [SessionStatus.ended, .failed] {
            let r = row(status, SessionExit(code: 1, reason: .exit))
            #expect(SessionActions.actions(for: r, on: machine([r])).allows(.runAgain), "\(status)")
        }
        for status in [SessionStatus.working, .needsYou, .done, .idle, .exited] {
            let r = row(status)
            #expect(!SessionActions.actions(for: r, on: machine([r])).available.contains(.runAgain), "\(status)")
        }
        let failed = row(.failed, SessionExit(code: 1, reason: .exit))
        // The machine's hello decides, not a later call.
        #expect(!SessionActions.actions(for: failed, on: machine([failed], caps: [Capability.events])).available
            .contains(.runAgain))
        #expect(!SessionActions.actions(for: failed).available.contains(.runAgain))
        let outside = row(.failed, SessionExit(code: 1, reason: .exit), managed: false)
        #expect(!SessionActions.actions(for: outside, on: machine([outside])).available.contains(.runAgain))
        let away = SessionActions.actions(
            for: failed, on: machine([failed], reachability: .offline(since: Samples.now, reason: "Asleep"))
        )
        #expect(away.available.contains(.runAgain))
        #expect(!away.allows(.runAgain))
        #expect(SessionAction.runAgain.title(for: failed) == "Run again")
    }

    @Test func runAgainHiddenForUsageLimit() {
        let limited = row(.failed, SessionExit(code: nil, reason: .usageLimit, detail: "Usage limit reached"))
        #expect(!SessionActions.actions(for: limited, on: machine([limited])).available.contains(.runAgain))
        #expect(LifecycleCopy.status(session(.failed, limited.exit)) == "Hit a usage limit")
    }

    /// An error the agent reported, with no exit code yet, leaves it alive at its prompt: the
    /// helper refuses to start it again, so neither the row nor the header offers Run again.
    @Test func errorStopOffersReplyNotRunAgain() {
        let waiting = row(.failed, SessionExit(code: nil, reason: .error, detail: "API error"))
        #expect(!SessionActions.actions(for: waiting, on: machine([waiting])).available.contains(.runAgain))
        #expect(!LifecycleCopy.canRunAgain(.failed, exit: waiting.exit))
        #expect(LifecycleCopy.status(session(.failed, waiting.exit)) == "Stopped with an error")
    }

    @Test func exitedErrorWithCodeRunsAgain() {
        let gone = row(.failed, SessionExit(code: 1, reason: .error))
        #expect(SessionActions.actions(for: gone, on: machine([gone])).allows(.runAgain))
        #expect(LifecycleCopy.canRunAgain(.failed, exit: SessionExit(code: 2, reason: .exit)))
    }

    @Test func runAgainCallsRelaunch() async throws {
        let failed = row(.failed, SessionExit(code: 1, reason: .exit))
        let store = AppStore(
            services: FakeAppServices(), controlCenter: ControlCenterState(machines: [machine([failed])], loaded: true)
        )
        let tools = FakeMachineTools()
        tools.defaultCapabilities = Self.launchCaps
        tools.answer("session.relaunch", Self.relaunched(id: failed.id, status: "working"))

        await store.runAgain(failed, tools: tools)

        #expect(tools.requests == [FakeMachineTools.Request(method: "session.relaunch", params: ["id": .string(failed.id)])])
        #expect(tools.calledHosts == [Samples.studio])
        let updated = try #require(store.controlCenter.machines.first?.sessions.first)
        #expect(updated.status == .working)
        #expect(updated.exit == nil)
        #expect(store.notice == nil)
        #expect(store.relaunching.isEmpty)
    }

    @Test func runAgainBringsArchivedRowBack() async throws {
        var failed = row(.failed, SessionExit(code: 1, reason: .exit))
        failed.statusSince = Samples.now.addingTimeInterval(-(AppStore.archiveAfter + 60))
        let live = Samples.row(bib: "02", name: "api", status: .working, cwd: "/Users/sam/src/api")
        let store = AppStore(services: FakeAppServices(), controlCenter: ControlCenterState(
            machines: [machine([live])], archived: [failed], loaded: true
        ))
        let tools = FakeMachineTools()
        tools.defaultCapabilities = Self.launchCaps
        tools.answer("session.relaunch", Self.relaunched(id: failed.id, status: "working"))

        await store.runAgain(failed, tools: tools)

        #expect(store.controlCenter.archived.isEmpty)
        #expect(store.controlCenter.machines[0].sessions.map(\.bib) == ["01", "02"])
        #expect(store.controlCenter.machines[0].sessions[0].status == .working)
    }

    @Test func runAgainRefusedSaysWhy() async throws {
        let failed = row(.failed, SessionExit(code: 1, reason: .exit))
        let store = AppStore(
            services: FakeAppServices(), controlCenter: ControlCenterState(machines: [machine([failed])], loaded: true)
        )
        let tools = FakeMachineTools()
        tools.defaultCapabilities = Self.launchCaps
        tools.fail("session.relaunch", code: -32006, message: "conflict", data: #"{"reason":"running"}"#)

        await store.runAgain(failed, tools: tools)

        #expect(store.controlCenter.machines.first?.sessions.first?.status == .failed)
        let notice = try #require(store.notice)
        #expect(notice.isError)
        #expect(notice.text == "Couldn't run 01 Claude Code again: The agent is still running.")
    }

    @Test func liveActivityEndedWords() {
        #expect(LiveStatus("ended").word == "Ended")
        #expect(LiveStatus("failed").word == "Stopped with an error")
        #expect(!LiveStatus("ended").showsElapsed)
        #expect(!LiveStatus("failed").showsElapsed)
        #expect(LiveStatus("failed", stale: true) == .notUpdated)
    }

    @Test func liveActivityOfflineHidesTimer() {
        let offline = LiveStatus("offline")
        #expect(offline == .offline)
        #expect(offline.word == "Offline")
        #expect(!offline.showsElapsed)
        #expect(offline.showsCount, "the waiting count stays")
        #expect(!offline.showsCompactCount, "the compact island says Offline, not a count")
        #expect(LiveStatus("working").showsCompactCount)
        #expect(offline.word(reason: "usage_limit") == "Offline")
        #expect(LiveStatus("offline", stale: true) == .notUpdated)
    }

    @Test func liveActivityWordsFollowTheExitReason() {
        #expect(LiveStatus("failed").word(reason: "usage_limit") == "Hit a usage limit")
        #expect(LiveStatus("ended").word(reason: "restart") == "Ended by restart")
        #expect(LiveStatus("failed").word(reason: "error") == "Stopped with an error")
        #expect(LiveStatus("ended").word(reason: "interrupted") == "Ended")
        #expect(LiveStatus("failed").word(reason: "exit") == "Stopped with an error")
        #expect(LiveStatus("ended").word(reason: "exit") == "Ended")
        #expect(LiveStatus("failed").word(reason: "brand_new") == "Stopped with an error")
        #expect(LiveStatus("ended").word(reason: nil) == "Ended")
        #expect(LiveStatus("working").word(reason: "usage_limit") == "Working")
    }

    private static func relaunched(id: String, status: String) -> String {
        """
        {"id":"\(id)","bib":"01","name":"app","agent":"claude","cwd":"/Users/sam/src/app",\
        "createdAt":"2026-11-03T12:00:00Z","status":"\(status)","activity":null,"attachedClients":0,\
        "managed":true,"muted":false,"meta":{"model":null,"contextPercent":null,"costUsd":null},\
        "command":["claude"],"exit":null}
        """
    }
}
