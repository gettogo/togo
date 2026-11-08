import Foundation
import Network
import Testing
@testable import TogoKit

/// How the monitor words a machine it cannot reach, and which addresses it tries.
struct HostMonitorReachTests {
    typealias F = MonitorFixtures

    /// A Mac saved by its LAN address is reached over Tailscale away from home: the wake probe
    /// tries every known route, as terminals do.
    @Test func wakeProbeTriesEveryKnownRoute() async {
        let book = RouteBook()
        var profile = F.record.profile
        profile.endpoint.host = "192.168.1.20"
        await book.setRoutes(
            [HostRoute(host: "192.168.1.20", kind: .lan), HostRoute(host: "studio.tail1234.ts.net", kind: .tailscale),
             HostRoute(host: "100.101.102.103", kind: .tailscale)],
            for: profile.id
        )
        let hosts = await HostMonitor.probeHosts(profile, book: book)
        #expect(hosts.contains("192.168.1.20"))
        #expect(hosts.contains("studio.tail1234.ts.net"))
        #expect(hosts.contains("100.101.102.103"))
    }

    @Test func wakeProbeThroughAJumpTriesTheJump() async {
        var profile = F.record.profile
        profile.jumps = [HostEndpoint(host: "bastion.example.com", user: "dev")]
        #expect(await HostMonitor.probeHosts(profile, book: RouteBook()) == ["bastion.example.com"])
    }

    /// With no network on the phone, nothing is tried and the machine is not called offline.
    @Test func deviceOfflineSkipsTheMachineAndRecoversOnAPathChange() async throws {
        let source = FakeSource(snapshots: [EventSnapshot(seq: 0, sessions: [F.session("s1")], approvals: [])])
        let factory = ScriptedFactory([.source(source)])
        let path = FakePath(offline: true)
        let clock = ManualClock()
        let monitor = F.monitor(factory, clock: clock, path: path)
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.reach == .deviceOffline })
        #expect(factory.calls == 0)

        path.setOffline(false)
        path.change()
        #expect(await eventually { log.reach == .online })
        #expect(factory.calls == 1)
        await monitor.stop()
    }

    /// A changed host key holds the machine on its own state, which the app turns into a way
    /// to review the key, instead of a "Try again" that can never work.
    @Test func changedHostKeyHoldsOnItsOwnState() async throws {
        let changed = SSHError.hostKeyChanged(host: "[work-mac.local]:22", expected: ["SHA256:a"], presented: "SHA256:b")
        let factory = ScriptedFactory([.fail(changed)])
        let clock = ManualClock()
        let monitor = F.monitor(factory, clock: clock)
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.reach == .hostKeyChanged })
        #expect(await stays { factory.calls > 1 })
        await monitor.stop()
    }

    @Test func rawTransportErrorsAreWordedPlainly() {
        struct ChannelError: Error {}
        #expect(HostMonitor.awayReach(for: ChannelError()) == .failed(SSHError.channelClosed(exitStatus: nil).localizedDescription))
        #expect(HostMonitor.awayReach(for: SSHError.refused) == .failed(SSHError.refused.localizedDescription))
        #expect(HostMonitor.awayReach(for: SSHError.timeout) == .offline(SSHError.timeout.localizedDescription))
    }

    @Test func pathSignatureFollowsThePreferredPhysicalInterface() {
        let wifi = PathSettle.signature(status: .satisfied, interfaces: [.wifi, .cellular])
        let wifiNoCell = PathSettle.signature(status: .satisfied, interfaces: [.wifi])
        let vpnOverWifi = PathSettle.signature(status: .satisfied, interfaces: [.other, .wifi, .cellular])
        let cell = PathSettle.signature(status: .satisfied, interfaces: [.other, .cellular])
        #expect(wifi == wifiNoCell)
        #expect(wifi == vpnOverWifi)
        #expect(wifi != cell)
        #expect(PathSettle.signature(status: .unsatisfied, interfaces: []) != wifi)
    }

    @Test func pathSettleReportsOnlyASettledChange() {
        let settle = PathSettle()
        #expect(!settle.update("wifi"))
        // Off and straight back within the settle time: nothing to report.
        #expect(settle.update("none"))
        #expect(!settle.update("wifi"))
        #expect(!settle.settle())
        // Wi-Fi to cellular, with a blip in between: one report.
        #expect(settle.update("none"))
        #expect(!settle.update("cellular"))
        #expect(settle.settle())
        #expect(!settle.update("cellular"))
    }
}
