import Foundation
import Testing
@testable import TogoKit

extension HostMonitorTests {
    @Test func rejectedKeychainAuthenticationAsksForSignIn() async throws {
        let factory = ScriptedFactory([.fail(SecretStoreError(status: -25293))])
        let monitor = MonitorFixtures.monitor(factory, clock: ManualClock())
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.reach == .authNeeded })
        #expect(factory.calls == 1)
        await monitor.stop()
    }

    @Test func authNeededStopsRetrying() async throws {
        let source = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [MonitorFixtures.session("s1")], approvals: [])])
        let factory = ScriptedFactory([.fail(SSHError.authFailed(tried: ["publickey"])), .source(source)])
        let clock = ManualClock()
        let monitor = MonitorFixtures.monitor(factory, clock: clock)
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
        let monitor = MonitorFixtures.monitor(factory, clock: clock)
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
        let source = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [MonitorFixtures.session("s1")], approvals: [])])
        let factory = ScriptedFactory([.failLater(gate, SSHError.cancelled), .source(source)])
        let clock = ManualClock()
        let monitor = MonitorFixtures.monitor(factory, clock: clock)
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

}
