import Foundation
import Testing
@testable import TogoKit

struct FailedRouteRecoveryTests {
    private func profile() -> HostProfile {
        HostProfile(id: UUID(), name: "test", endpoint: HostEndpoint(host: "studio.local", user: "test"),
                    auth: .password, routes: [
                        .manual("studio.local"), HostRoute(host: "192.168.1.20", kind: .lan),
                        HostRoute(host: "100.101.102.103", kind: .tailscale),
                    ])
    }

    @Test func failedRaceQuarantinesReusedLearnedLanWithoutStoppingRecovery() async {
        let book = RouteBook(), host = profile()
        #expect(await book.recordFailedRace(["192.168.1.20"], for: host, sawPinnedKey: false))
        #expect(await book.candidates(for: host) == ["studio.local", "100.101.102.103"])
        await book.clearMismatches(["192.168.1.20"], for: host.id)
        #expect(await book.candidates(for: host).contains("192.168.1.20"))
    }

    @Test(arguments: ["studio.local", "100.101.102.103", "unrecognised.local"])
    func explicitAndNonLanKeyChangesRetainTheirWarning(_ address: String) async {
        let book = RouteBook(), host = profile()
        #expect(await book.recordFailedRace([address], for: host, sawPinnedKey: false) == false)
    }

    @Test func typedLanAndMixedMismatchKeepTheSecurityWarning() async {
        let book = RouteBook()
        var host = profile()
        host.routes.append(.manual("192.168.1.30"))
        #expect(await book.recordFailedRace(["192.168.1.30"], for: host, sawPinnedKey: false) == false)
        #expect(await book.recordFailedRace(["192.168.1.20", "studio.local"], for: host, sawPinnedKey: false) == false)
        #expect(await book.recordFailedRace([], for: host, sawPinnedKey: false) == false)
        #expect(await book.recordFailedRace(["studio.local"], for: host, sawPinnedKey: true))
    }
}
