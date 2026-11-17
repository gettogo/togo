import Foundation
import Synchronization
import Testing
@testable import TogoKit

struct ConnectionTimingTests {
    private func profile() -> HostProfile {
        .init(id: UUID(), name: "Test", endpoint: .init(host: "host.test.ts.net", user: "test"), auth: .key(id: "test"))
    }

    @Test func tailnetBootstrapDoesNotSlowLanJumpChannelsOrExplicitTimeouts() {
        for kind in [HostRoute.Kind.local, .lan, .other] {
            #expect(ConnectionTiming.timeout(kind: kind, succeeded: false, direct: true, override: nil) == .seconds(8))
        }
        #expect(ConnectionTiming.timeout(kind: .tailscale, succeeded: false, direct: true, override: nil) == .seconds(20))
        #expect(ConnectionTiming.timeout(kind: .tailscale, succeeded: true, direct: true, override: nil) == .seconds(8))
        #expect(ConnectionTiming.timeout(kind: .tailscale, succeeded: false, direct: false, override: nil) == .seconds(8))
        #expect(ConnectionTiming.timeout(kind: .tailscale, succeeded: false, direct: true, override: .milliseconds(30)) == .milliseconds(30))
    }

    @Test func successfulRoutesBelongToOneProfileAndForgetMakesThemColdAgain() async {
        let book = RouteBook(), first = profile(), second = profile()
        let host = first.endpoint.host
        #expect(await book.connectionTimeout(for: first, host: host, direct: true, override: nil) == .seconds(20))
        await book.recordConnected(host.uppercased(), for: first.id)
        #expect(await book.connectionTimeout(for: first, host: host, direct: true, override: nil) == .seconds(8))
        #expect(await book.connectionTimeout(for: second, host: host, direct: true, override: nil) == .seconds(20))
        #expect(await book.connectionTimeout(for: first, host: "100.64.0.1", direct: true, override: nil) == .seconds(20))
        #expect(await book.connectionTimeout(for: first, host: "100.128.0.1", direct: true, override: nil) == .seconds(8))
        await book.forget(first.id)
        #expect(await book.connectionTimeout(for: first, host: host, direct: true, override: nil) == .seconds(20))
    }

    @Test func initialMonitorRetriesAreFlatAndEstablishedLinksKeepBackoff() {
        #expect(HostMonitor.retryDelay(failures: 0, hasConnected: false) == .zero)
        for failure in 1...12 { #expect(HostMonitor.retryDelay(failures: failure, hasConnected: false) == .seconds(2)) }
        #expect(HostMonitor.retryDelay(failures: 1, hasConnected: true) == .milliseconds(500))
        #expect(HostMonitor.retryDelay(failures: 2, hasConnected: true) == .seconds(1))
        #expect(HostMonitor.retryDelay(failures: 3, hasConnected: true) == .seconds(8))
        #expect(HostMonitor.retryDelay(failures: 3, hasConnected: false, deviceOffline: true) == .seconds(8))
    }

    @Test func spansPairTheOutcomeWithAMonotonicDurationAndNoHostText() {
        let events = Mutex<[ConnectionAttemptEvent]>([])
        let host = UUID()
        let span = ConnectionAttemptSpan(hostID: host, stage: .route(.tailscale), report: { event in events.withLock { $0.append(event) } })
        span.end(.succeeded)
        let recorded = events.withLock { $0 }
        #expect(recorded.count == 2 && recorded.first?.outcome == .started && recorded.last?.outcome == .succeeded)
        #expect(recorded.first?.id == recorded.last?.id && recorded.last?.hostID == host)
        #expect((recorded.last?.elapsedMilliseconds ?? -1) >= 0)
        #expect(ConnectionAttemptSpan.failure(SSHError.cancelled) == .cancelled)
        #expect(ConnectionAttemptSpan.failure(CancellationError()) == .cancelled)
        #expect(ConnectionAttemptSpan.failure(SSHError.timeout) == .failed)
    }
}
