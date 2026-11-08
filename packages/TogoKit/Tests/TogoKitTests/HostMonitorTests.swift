import Foundation
import Testing
@testable import TogoKit

struct HostMonitorTests {
    typealias F = MonitorFixtures

    @Test func snapshotThenEventsFold() async throws {
        let source = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [F.session("s1")], approvals: [])])
        let monitor = F.monitor(ScriptedFactory([.source(source)]), clock: ManualClock())
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.reach == .online && log.sessionIDs == ["s1"] })

        source.emit(F.event(1, .session(F.session("s2"))))
        source.emit(F.event(2, .session(F.session("s1", name: "renamed", status: .needsYou))))
        source.emit(F.event(3, .sessionRemoved, sessionId: "s2"))
        source.emit(F.event(4, .approvalOpen(F.approval("r1"))))
        source.emit(F.event(5, .notify(title: "Done", body: "", level: .done)))
        #expect(await eventually { log.last?.approvals.map(\.requestId) == ["r1"] })
        let last = try #require(log.last)
        #expect(last.sessions == [F.session("s1", name: "renamed", status: .needsYou)])
        #expect(last.helper == .present(version: "0.3.0"))
        #expect(last.capabilities.contains(Capability.events))
        #expect(source.snapshotCalls == 1)
        await monitor.stop()
    }

    @Test func monitorFetchesAwakeAfterSubscribe() async throws {
        let source = FakeSource(
            snapshots: [EventSnapshot(seq: 0, sessions: [], approvals: [])],
            capabilities: [Capability.events, Capability.awake]
        )
        source.setAwake(F.awake())
        let monitor = F.monitor(ScriptedFactory([.source(source)]), clock: ManualClock())
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.last?.awake == F.awake() })
        #expect(source.awakeCalls == 1)
        await monitor.stop()
    }

    @Test func awakeEventUpdatesSnapshot() async throws {
        let source = FakeSource(
            snapshots: [EventSnapshot(seq: 0, sessions: [], approvals: [])],
            capabilities: [Capability.events, Capability.awake]
        )
        source.setAwake(F.awake(held: false, reason: .idle))
        let monitor = F.monitor(ScriptedFactory([.source(source)]), clock: ManualClock())
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.last?.awake?.held == false })
        source.emit(F.event(1, .awake(F.awake(held: true, reason: .grace))))
        #expect(await eventually { log.last?.awake == F.awake(held: true, reason: .grace) })
        await monitor.stop()
    }

    @Test func resyncReadsAwakeAgain() async throws {
        let source = FakeSource(
            snapshots: [
                EventSnapshot(seq: 0, sessions: [], approvals: []),
                EventSnapshot(seq: 4, sessions: [], approvals: []),
            ],
            capabilities: [Capability.events, Capability.awake]
        )
        source.setAwake(F.awake(held: false, reason: .idle))
        let monitor = F.monitor(ScriptedFactory([.source(source)]), clock: ManualClock())
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.last?.awake?.held == false })
        source.setAwake(F.awake(held: true, reason: .grace))
        source.emit(F.event(3, .session(F.session("gap"))))
        #expect(await eventually { source.awakeCalls == 2 && log.last?.awake == F.awake(held: true, reason: .grace) })
        await monitor.stop()
    }

    @Test func resyncKeepsAwakeWhenTheReadFails() async throws {
        let source = FakeSource(
            snapshots: [
                EventSnapshot(seq: 0, sessions: [], approvals: []),
                EventSnapshot(seq: 4, sessions: [], approvals: []),
            ],
            capabilities: [Capability.events, Capability.awake]
        )
        source.setAwake(F.awake(held: true, reason: .active))
        let monitor = F.monitor(ScriptedFactory([.source(source)]), clock: ManualClock())
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.last?.awake?.held == true })
        source.setAwake(nil)
        source.emit(F.event(3, .session(F.session("gap"))))
        #expect(await eventually { source.awakeCalls == 2 && source.snapshotCalls == 2 })
        #expect(log.last?.awake == F.awake(held: true, reason: .active))
        await monitor.stop()
    }

    @Test func firstSnapshotDoesNotWaitForAwake() async throws {
        let gate = Gate()
        let source = FakeSource(
            snapshots: [EventSnapshot(seq: 0, sessions: [F.session("s1")], approvals: [])],
            capabilities: [Capability.events, Capability.awake]
        )
        source.setAwake(F.awake(), gate: gate)
        let monitor = F.monitor(ScriptedFactory([.source(source)]), clock: ManualClock())
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.sessionIDs == ["s1"] })
        #expect(log.last?.awake == nil)
        gate.open()
        #expect(await eventually { log.last?.awake == F.awake() })
        await monitor.stop()
    }

    @Test func noAwakeWithoutCapability() async throws {
        let source = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [], approvals: [])])
        source.setAwake(F.awake())
        let monitor = F.monitor(ScriptedFactory([.source(source)]), clock: ManualClock())
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.reach == .online })
        #expect(source.awakeCalls == 0)
        #expect(log.last?.awake == nil)
        await monitor.stop()
    }

    @Test func seqGapResubscribes() async throws {
        let replacement = EventSnapshot(seq: 4, sessions: [F.session("s9")], approvals: [F.approval("r9")])
        let source = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [], approvals: []), replacement])
        let monitor = F.monitor(ScriptedFactory([.source(source)]), clock: ManualClock())
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.reach == .online })

        source.emit(F.event(1, .session(F.session("s1"))))
        source.emit(F.event(2, .session(F.session("s2"))))
        source.emit(F.event(4, .session(F.session("s4"))))
        #expect(await eventually { source.snapshotCalls == 2 && log.sessionIDs == ["s9"] })
        #expect(log.last?.approvals == replacement.approvals)

        // The stream continues from the replacement's seq.
        source.emit(F.event(5, .session(F.session("s5"))))
        #expect(await eventually { log.sessionIDs == ["s9", "s5"] })
        #expect(source.snapshotCalls == 2)
        await monitor.stop()
    }

    @Test func reconnectReplacesSnapshot() async throws {
        let first = FakeSource(snapshots: [EventSnapshot(seq: 7, sessions: [F.session("s1"), F.session("s2")], approvals: [])])
        // s2 was killed at the desk while the connection was down.
        let second = FakeSource(snapshots: [EventSnapshot(seq: 9, sessions: [F.session("s1")], approvals: [])])
        let factory = ScriptedFactory([.source(first), .source(second)])
        let monitor = F.monitor(factory, clock: ManualClock())
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.sessionIDs == ["s1", "s2"] })

        first.drop()
        #expect(await eventually { factory.calls == 2 && log.reach == .online && log.sessionIDs == ["s1"] })
        #expect(await eventually { first.isClosed })
        // Events from the new connection fold onto the new snapshot.
        second.emit(F.event(10, .sessionRemoved, sessionId: "s1"))
        #expect(await eventually { log.sessionIDs.isEmpty })
        await monitor.stop()
    }

    @Test func offlineKeepsLastSessions() async throws {
        let source = FakeSource(snapshots: [EventSnapshot(seq: 1, sessions: [F.session("s1")], approvals: [])])
        let factory = ScriptedFactory([
            .source(source), .fail(SSHError.timeout), .fail(SSHError.timeout), .fail(SSHError.timeout),
        ])
        let clock = ManualClock()
        let wall = WallClock()
        let monitor = F.monitor(factory, clock: clock, wall: wall)
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.reach == .online })

        wall.set(F.epoch + 600)
        source.drop()
        #expect(await eventually { factory.calls == 2 })
        #expect(await clock.runSleep(.milliseconds(500)))
        #expect(await eventually { factory.calls == 3 })
        #expect(log.reach == .connecting)
        #expect(await clock.runSleep(.seconds(1)))
        #expect(await eventually { log.reach == .offline(SSHError.timeout.localizedDescription) })
        #expect(factory.calls == 4)
        #expect(log.sessionIDs == ["s1"])
        #expect(log.last?.lastSeen == F.epoch)

        // Still in the foreground, so it keeps trying every 8 s.
        #expect(await clock.runSleep(.seconds(8)))
        #expect(await eventually { factory.calls == 5 })
        #expect(await eventually { clock.pending == [.seconds(8)] })
        await monitor.stop()
    }

    @Test func approvalClosedRemovesCard() async throws {
        let source = FakeSource(snapshots: [
            EventSnapshot(seq: 3, sessions: [F.session("s1")], approvals: [F.approval("r1"), F.approval("r2")]),
        ])
        let monitor = F.monitor(ScriptedFactory([.source(source)]), clock: ManualClock())
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.last?.approvals.count == 2 })

        source.emit(F.event(4, .approvalClosed(requestId: "r1", resolution: .desk), sessionId: "s1"))
        #expect(await eventually { log.last?.approvals.map(\.requestId) == ["r2"] })
        // Closing it again, as a second answer would, changes nothing.
        source.emit(F.event(5, .approvalClosed(requestId: "r1", resolution: .phone), sessionId: "s1"))
        source.emit(F.event(6, .session(F.session("s2"))))
        #expect(await eventually { log.sessionIDs == ["s1", "s2"] })
        #expect(log.last?.approvals.map(\.requestId) == ["r2"])
        #expect(log.last?.closed == [ClosedApproval(requestId: "r1", resolution: .desk)])
        await monitor.stop()
    }

    @Test func closedApprovalsKeepOnlyTheNewest() {
        var snapshot = HostSnapshot(hostID: UUID())
        for n in 0..<(HostSnapshot.closedLimit + 5) {
            snapshot.approvals.append(F.approval("r\(n)"))
            snapshot.apply(F.event(n + 1, .approvalClosed(requestId: "r\(n)", resolution: .expired)))
        }
        #expect(snapshot.closed.count == HostSnapshot.closedLimit)
        #expect(snapshot.closed.last?.requestId == "r\(HostSnapshot.closedLimit + 4)")
    }

    @Test func lateOpenForAClosedRequestIsIgnored() {
        var snapshot = HostSnapshot(hostID: UUID())
        snapshot.apply(F.event(1, .approvalOpen(F.approval("r1"))))
        snapshot.apply(F.event(2, .approvalClosed(requestId: "r1", resolution: .phone)))
        snapshot.apply(F.event(3, .approvalOpen(F.approval("r1"))))
        #expect(snapshot.approvals.isEmpty)
        // A close for a request this device never saw open also keeps its late open out.
        snapshot.apply(F.event(4, .approvalClosed(requestId: "r2", resolution: .desk)))
        snapshot.apply(F.event(5, .approvalOpen(F.approval("r2"))))
        #expect(snapshot.approvals.isEmpty)
        #expect(snapshot.closed == [ClosedApproval(requestId: "r1", resolution: .phone)])
        // A full snapshot from the helper is the truth: a request it lists is open.
        snapshot.replaceApprovals([F.approval("r1")])
        #expect(snapshot.approvals.map(\.requestId) == ["r1"])
        snapshot.apply(F.event(6, .approvalOpen(F.approval("r1", title: "Edit?"))))
        #expect(snapshot.approvals.first?.title == "Edit?")
    }

    @Test func approvalOpenRepeatUpdates() async throws {
        let source = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [], approvals: [F.approval("r0")])])
        let monitor = F.monitor(ScriptedFactory([.source(source)]), clock: ManualClock())
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.reach == .online })

        source.emit(F.event(1, .approvalOpen(F.approval("r1", title: "Edit main.go?"))))
        source.emit(F.event(2, .approvalOpen(F.approval("r1", title: "Edit main.go and go.mod?"))))
        #expect(await eventually { log.last?.approvals.last?.title == "Edit main.go and go.mod?" })
        #expect(log.last?.approvals.map(\.requestId) == ["r0", "r1"])
        await monitor.stop()
    }

    @Test func pollsWithoutEvents() async throws {
        let source = FakeSource(
            snapshots: [
                EventSnapshot(seq: 0, sessions: [F.session("s1")], approvals: []),
                EventSnapshot(seq: 0, sessions: [F.session("s1"), F.session("s2")], approvals: []),
                EventSnapshot(seq: 0, sessions: [F.session("s2")], approvals: []),
            ],
            capabilities: []
        )
        let clock = ManualClock()
        let monitor = F.monitor(ScriptedFactory([.source(source)]), clock: clock)
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.sessionIDs == ["s1"] })
        #expect(source.snapshotCalls == 1)

        #expect(await clock.runSleep(.seconds(5)))
        #expect(await eventually { log.sessionIDs == ["s1", "s2"] })
        #expect(source.snapshotCalls == 2)
        #expect(await clock.runSleep(.seconds(5)))
        #expect(await eventually { log.sessionIDs == ["s2"] })
        #expect(source.snapshotCalls == 3)
        // An ended event stream is not a dropped connection when the host has no events.
        #expect(log.reach == .online)
        await monitor.stop()
    }

    /// A machine that answers but whose helper does not, as while the helper restarts, is not
    /// offline: it reads as failed with the reason and the attempts go on until it is back.
    @Test func reachableButNotAnsweringIsNotOffline() async throws {
        let source = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [F.session("s1")], approvals: [])])
        let factory = ScriptedFactory([
            .fail(HelperError.closed), .fail(HelperError.closed), .fail(HelperError.closed), .source(source),
        ])
        let clock = ManualClock()
        let monitor = F.monitor(factory, clock: clock)
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { factory.calls == 1 })
        #expect(await clock.runSleep(.milliseconds(500)))
        #expect(await eventually { factory.calls == 2 })
        #expect(await clock.runSleep(.seconds(1)))
        #expect(await eventually { log.reach == .failed(HelperError.closed.localizedDescription) })
        #expect(await clock.runSleep(.seconds(8)))
        #expect(await eventually { log.reach == .online && log.sessionIDs == ["s1"] })
        await monitor.stop()
    }

    @Test func lockedKeychainAsksForSignIn() async throws {
        let factory = ScriptedFactory([.fail(SecretStoreError(status: -25308))])
        let monitor = F.monitor(factory, clock: ManualClock())
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.reach == .authNeeded })
        #expect(factory.calls == 1)
        await monitor.stop()
    }

    @Test func authNeededStopsRetrying() async throws {
        let source = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [F.session("s1")], approvals: [])])
        let factory = ScriptedFactory([.fail(SSHError.authFailed(tried: ["publickey"])), .source(source)])
        let clock = ManualClock()
        let monitor = F.monitor(factory, clock: clock)
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.reach == .authNeeded })

        clock.advance(.seconds(60))
        await monitor.setForeground(true)
        #expect(clock.pending.isEmpty)
        #expect(factory.calls == 1)

        await monitor.refresh()
        #expect(await eventually { log.reach == .online && log.sessionIDs == ["s1"] })
        #expect(factory.calls == 2)
        await monitor.stop()
    }

    @Test func missingKeyHoldsWithoutRetrying() async throws {
        let factory = ScriptedFactory([.fail(KeyError.missing)])
        let clock = ManualClock()
        let monitor = F.monitor(factory, clock: clock)
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.reach == .keyMissing })

        clock.advance(.seconds(60))
        await monitor.setForeground(true)
        #expect(clock.pending.isEmpty)
        #expect(factory.calls == 1)
        await monitor.stop()
    }

    @Test func unlockRefusedDuringBackgroundTripIsRetried() async throws {
        let gate = Gate()
        let source = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [F.session("s1")], approvals: [])])
        let factory = ScriptedFactory([.failLater(gate, SSHError.cancelled), .source(source)])
        let clock = ManualClock()
        let monitor = F.monitor(factory, clock: clock)
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { factory.calls == 1 })

        await monitor.setForeground(false)
        await monitor.setForeground(true)
        gate.open()
        #expect(await clock.runSleep(.milliseconds(500)))
        #expect(await eventually { log.reach == .online && log.sessionIDs == ["s1"] })
        await monitor.stop()
    }

    @Test func backgroundClosesAfterThirtySeconds() async throws {
        let first = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [F.session("s1")], approvals: [])])
        let second = FakeSource(snapshots: [EventSnapshot(seq: 2, sessions: [F.session("s1")], approvals: [])])
        let factory = ScriptedFactory([.source(first), .source(second)])
        let clock = ManualClock()
        let monitor = F.monitor(factory, clock: clock)
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.reach == .online })
        #expect(first.foregroundFlags == [true])

        await monitor.setForeground(false)
        // Subscribed again without the foreground flag, so pushes resume.
        #expect(first.foregroundFlags == [true, false])
        #expect(first.snapshotCalls == 2)
        #expect(await eventually { clock.pending == [.seconds(30)] })
        clock.advance(.seconds(29))
        #expect(!first.isClosed)
        clock.advance(.seconds(1))
        #expect(await eventually { first.isClosed })
        #expect(factory.calls == 1)

        await monitor.setForeground(true)
        #expect(await eventually { factory.calls == 2 && log.reach == .online })
        #expect(second.foregroundFlags == [true])
        await monitor.stop()
    }

    @Test func backgroundReturnInsideGraceKeepsConnection() async throws {
        let source = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [], approvals: [])])
        let factory = ScriptedFactory([.source(source)])
        let clock = ManualClock()
        let monitor = F.monitor(factory, clock: clock)
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.reach == .online })

        await monitor.setForeground(false)
        #expect(await eventually { clock.pending == [.seconds(30)] })
        clock.advance(.seconds(10))
        await monitor.setForeground(true)
        #expect(await eventually { clock.pending.isEmpty })
        clock.advance(.seconds(60))
        #expect(!source.isClosed)
        #expect(source.foregroundFlags == [true, false, true])
        #expect(factory.calls == 1)
        await monitor.stop()
    }

    @Test func foregroundChangeDuringConnectReachesConnection() async throws {
        let gate = Gate()
        let source = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [], approvals: [])], snapshotGate: gate)
        let monitor = F.monitor(ScriptedFactory([.source(source)]), clock: ManualClock())
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { source.foregroundFlags == [true] })

        await monitor.setForeground(false)
        gate.open()
        #expect(await eventually { log.reach == .online && source.foregroundFlags == [true, false] })
        #expect(await eventually { source.snapshotCalls == 2 })
        await monitor.stop()
    }

    @Test func noReconnectWhileBackgrounded() async throws {
        let first = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [F.session("s1")], approvals: [])])
        let second = FakeSource(snapshots: [EventSnapshot(seq: 3, sessions: [F.session("s2")], approvals: [])])
        let factory = ScriptedFactory([.source(first), .source(second)])
        let clock = ManualClock()
        let monitor = F.monitor(factory, clock: clock)
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.reach == .online })

        await monitor.setForeground(false)
        first.drop()
        #expect(await eventually { first.isClosed })
        clock.advance(.seconds(60))
        #expect(await stays { factory.calls > 1 })

        await monitor.setForeground(true)
        #expect(await eventually { factory.calls == 2 && log.sessionIDs == ["s2"] })
        await monitor.stop()
    }

    @Test func setForegroundBeforeStartIsKept() async throws {
        let source = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [], approvals: [])])
        let factory = ScriptedFactory([.source(source)])
        let monitor = F.monitor(factory, clock: ManualClock())
        let log = SnapshotLog(monitor)
        await monitor.setForeground(false)
        await monitor.start()
        #expect(await stays { factory.calls > 0 })

        await monitor.setForeground(true)
        #expect(await eventually { log.reach == .online })
        #expect(source.foregroundFlags == [true])
        await monitor.stop()
    }

    /// The interleaving that matters (an attempt cancelled before it first runs) depends
    /// on scheduling, so the scenario runs several times.
    @Test(arguments: 0..<10)
    func backToBackReconnectsLeaveOneSource(round: Int) async throws {
        let first = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [F.session("s1")], approvals: [])])
        let gate = Gate()
        let later = (0..<6).map { _ in
            FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [F.session("s2")], approvals: [])], snapshotGate: gate)
        }
        let factory = ScriptedFactory([.source(first)] + later.map { .source($0) })
        let path = FakePath()
        let monitor = F.monitor(factory, clock: ManualClock(), path: path)
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.reach == .online })

        first.drop()
        path.change()
        path.change()
        #expect(await eventually { factory.calls >= 2 })
        // Nothing is live yet; a foreground report must not start a second run of attempts.
        await monitor.setForeground(true)
        _ = try? await monitor.perform(.kill("s1"))
        gate.open()
        let all = [first] + later
        let live = { all.prefix(factory.calls).filter { !$0.isClosed }.count }
        #expect(await eventually { log.sessionIDs == ["s2"] })
        #expect(await eventually { live() == 1 })
        #expect(await stays { live() != 1 })

        await monitor.stop()
        #expect(await eventually { live() == 0 })
    }

    @Test func pathChangeReconnects() async throws {
        let first = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [F.session("s1")], approvals: [])])
        let second = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [F.session("s2")], approvals: [])])
        let factory = ScriptedFactory([.source(first), .source(second)])
        let path = FakePath()
        let monitor = F.monitor(factory, clock: ManualClock(), path: path)
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.sessionIDs == ["s1"] })

        path.change()
        #expect(await eventually { log.sessionIDs == ["s2"] })
        #expect(await eventually { first.isClosed })
        await monitor.stop()
    }

    @Test func performWithoutEventsAppliesResult() async throws {
        let source = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [F.session("s1")], approvals: [])], capabilities: [])
        let monitor = F.monitor(ScriptedFactory([.source(source)]), clock: ManualClock())
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.reach == .online })

        let created = try await monitor.perform(.create(CreateSessionParams(name: "s2")))
        #expect(created?.id == "s2")
        #expect(await eventually { log.sessionIDs == ["s1", "s2"] })
        _ = try await monitor.perform(.kill("s1"))
        #expect(await eventually { log.sessionIDs == ["s2"] })
        await monitor.stop()
    }
}
