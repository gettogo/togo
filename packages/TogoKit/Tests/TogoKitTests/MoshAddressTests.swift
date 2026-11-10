import Foundation
import Testing
@testable import TogoKit

struct MoshAddressTests {
    static let linkLocal = "fe80::a8bb:ccff:fedd:ee52"
    static let routes: [HostRoute] = [
        HostRoute(host: "studio-mac-16.local", kind: .local),
        HostRoute(host: "192.168.1.123", kind: .lan),
        HostRoute(host: "2001:db8::5", kind: .lan),
        HostRoute(host: "mbp.tail1234.ts.net", kind: .tailscale),
        HostRoute(host: "100.88.123.45", kind: .tailscale),
    ]

    @Test func ipv4ReachKeepsTheSSHAddress() {
        let target = MoshAddress.choose(
            reached: ReachedAddress(ip: "192.168.1.123"), fallbackHost: "mbp.local", routes: Self.routes, resolved: []
        )
        #expect(target == MoshTarget(host: "192.168.1.123", bind: .sshAddress))
    }

    @Test func globalIPv6ReachKeepsTheSSHAddress() {
        let target = MoshAddress.choose(
            reached: ReachedAddress(ip: "2001:db8::5"), fallbackHost: "mbp.local", routes: [], resolved: []
        )
        #expect(target == MoshTarget(host: "2001:db8::5", bind: .sshAddress))
    }

    @Test func linkLocalReachPrefersAnAddressTheNameResolvedTo() {
        let target = MoshAddress.choose(
            reached: ReachedAddress(ip: Self.linkLocal, interface: "en0"), fallbackHost: "studio-mac-16.local",
            routes: Self.routes, resolved: ["192.168.1.50"]
        )
        #expect(target == MoshTarget(host: "192.168.1.50", bind: .address("192.168.1.50")))
    }

    @Test func linkLocalReachUsesTheHelpersLANAddressAndBindsTheServerThere() {
        let target = MoshAddress.choose(
            reached: ReachedAddress(ip: Self.linkLocal, interface: "en0"), fallbackHost: "studio-mac-16.local",
            routes: Self.routes, resolved: []
        )
        #expect(target == MoshTarget(host: "192.168.1.123", bind: .address("192.168.1.123")))
    }

    @Test func linkLocalReachFallsToTheTailnetAddress() {
        let routes = Self.routes.filter { $0.kind != .lan }
        let target = MoshAddress.choose(
            reached: ReachedAddress(ip: Self.linkLocal), fallbackHost: "studio-mac-16.local", routes: routes, resolved: []
        )
        #expect(target == MoshTarget(host: "100.88.123.45", bind: .address("100.88.123.45")))
    }

    @Test func linkLocalReachTakesAGlobalIPv6WhenNoIPv4IsKnown() {
        let routes = [HostRoute(host: "2001:db8::5", kind: .lan), HostRoute(host: "fe80::9", kind: .lan)]
        let target = MoshAddress.choose(
            reached: ReachedAddress(ip: Self.linkLocal), fallbackHost: "m.local", routes: routes, resolved: []
        )
        #expect(target == MoshTarget(host: "2001:db8::5", bind: .address("2001:db8::5")))
    }

    @Test func linkLocalReachWithOnlyItsInterfaceIsScoped() {
        let target = MoshAddress.choose(
            reached: ReachedAddress(ip: Self.linkLocal, interface: "en0"), fallbackHost: "m.local",
            routes: [HostRoute(host: "m.local", kind: .local)], resolved: []
        )
        #expect(target == MoshTarget(host: "\(Self.linkLocal)%en0", bind: .sshAddress))
    }

    @Test func unscopedLinkLocalWithNothingElseCannotUseMosh() {
        let target = MoshAddress.choose(
            reached: ReachedAddress(ip: Self.linkLocal), fallbackHost: "m.local",
            routes: [HostRoute(host: "m.local", kind: .local)], resolved: []
        )
        #expect(target == nil)
    }

    @Test func savedLinkLocalAddressWithAZoneIsKept() {
        let target = MoshAddress.choose(reached: nil, fallbackHost: "fe80::1%en1", routes: [], resolved: [])
        #expect(target == MoshTarget(host: "fe80::1%en1", bind: .sshAddress))
    }

    @Test func addressShapes() {
        #expect(MoshAddress.isLinkLocalIPv6("fe80::1"))
        #expect(MoshAddress.isLinkLocalIPv6("FE80::1%en0"))
        #expect(MoshAddress.isLinkLocalIPv6("febf::1"))
        #expect(!MoshAddress.isLinkLocalIPv6("fec0::1"))
        #expect(!MoshAddress.isLinkLocalIPv6("192.168.1.2"))
        #expect(!MoshAddress.isLinkLocalIPv6("fe80.local"))
        #expect(MoshAddress.isIPv4("100.88.123.45"))
        #expect(!MoshAddress.isIPv4("mbp.local"))
        #expect(MoshAddress.isRoutableIPv6("fd7a:115c:a1e0::1"))
        #expect(!MoshAddress.isRoutableIPv6("::1"))
        #expect(!MoshAddress.isRoutableIPv6("::"))
        #expect(!MoshAddress.isRoutableIPv6("ff02::1"))
    }

    @Test func resolvingALiteralOrUnknownNameIsQuick() async {
        #expect(await MoshAddress.resolveIPv4("192.168.1.2", timeout: .seconds(1)) == [])
        #expect(await MoshAddress.resolveIPv4("localhost", timeout: .seconds(2)) == ["127.0.0.1"])
    }
}
