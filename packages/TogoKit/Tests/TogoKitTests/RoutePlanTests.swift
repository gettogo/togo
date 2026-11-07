import Foundation
import Testing
import TogoKit

struct RoutePlanTests {
    private let reported: [HostRoute] = [
        HostRoute(host: "studio.tail1234.ts.net", kind: .tailscale),
        HostRoute(host: "100.101.102.103", kind: .tailscale),
        HostRoute(host: "fd7a:115c:a1e0::1", kind: .tailscale),
        HostRoute(host: "2001:db8::10", kind: .lan),
        HostRoute(host: "192.168.1.20", kind: .lan),
        HostRoute(host: "studio.local", kind: .local),
    ]

    @Test func ordersLocalNetworkBeforeTailscale() {
        let order = RoutePlan.ordered(reported, saved: "192.168.1.20", last: nil)
        #expect(order == [
            "studio.local", "192.168.1.20", "2001:db8::10",
            "studio.tail1234.ts.net", "100.101.102.103", "fd7a:115c:a1e0::1",
        ])
    }

    @Test func lastSuccessfulRouteGoesFirst() {
        let order = RoutePlan.ordered(reported, saved: "192.168.1.20", last: "100.101.102.103")
        #expect(order.first == "100.101.102.103")
        #expect(order.filter { $0 == "100.101.102.103" }.count == 1)
        #expect(order.count == reported.count)
    }

    @Test func forgottenLastRouteIsIgnored() {
        let order = RoutePlan.ordered(reported, saved: "192.168.1.20", last: "10.9.9.9")
        #expect(order.first == "studio.local")
        #expect(!order.contains("10.9.9.9"))
    }

    @Test func savedAddressAlwaysTakesPartAndDuplicatesGoWithoutCase() {
        let routes = [HostRoute(host: "Studio.local", kind: .local), HostRoute(host: "studio.LOCAL", kind: .local)]
        let order = RoutePlan.ordered(routes, saved: "build.example.com", last: nil)
        #expect(order == ["Studio.local", "build.example.com"])
    }

    @Test func manyAddressesAreCapped() {
        let routes = (1...20).map { HostRoute(host: "10.0.0.\($0)", kind: .lan) }
        #expect(RoutePlan.ordered(routes, saved: "10.0.0.1", last: nil).count == RoutePlan.limit)
    }

    @Test func jumpHostMeansOneRoute() {
        var profile = HostProfile(
            id: UUID(), name: "lab", endpoint: HostEndpoint(host: "10.0.0.5", user: "ops"),
            jumps: [HostEndpoint(host: "bastion", user: "j")], auth: .password, routes: reported
        )
        #expect(RoutePlan.candidates(profile) == ["10.0.0.5"])
        profile.jumps = []
        #expect(RoutePlan.candidates(profile).count == reported.count + 1)
    }

    @Test func manualRoutesAreClassifiedByShape() {
        #expect(HostRoute.manual("studio.tail1234.ts.net").kind == .tailscale)
        #expect(HostRoute.manual("100.64.0.1").kind == .tailscale)
        #expect(HostRoute.manual("Studio.local.").kind == .local)
        #expect(HostRoute.manual("172.20.1.1").kind == .lan)
        #expect(HostRoute.manual("fd00::1").kind == .lan)
        #expect(HostRoute.manual("172.32.1.1").kind == .other)
        #expect(HostRoute.manual("build.example.com").kind == .other)
        #expect(HostRoute.manual("x").manual)
    }

    @Test func reportReplacesLearnedRoutesAndKeepsManualOnes() {
        let routes = [
            HostRoute.manual("192.168.1.20"), HostRoute.manual("build.example.com"),
            HostRoute(host: "192.168.1.99", kind: .lan),
        ]
        let fresh = [HostRoute(host: "192.168.1.20", kind: .lan), HostRoute(host: "studio.local", kind: .local)]
        let merged = RoutePlan.refreshed(routes, reported: fresh)
        #expect(merged.map(\.host) == ["192.168.1.20", "build.example.com", "studio.local"])
        #expect(merged[0].manual)
        #expect(!merged[2].manual)
    }

    @Test func learnedIsNilWithoutChange() {
        let profile = HostProfile(
            id: UUID(), name: "s", endpoint: HostEndpoint(host: "192.168.1.20", user: "sam"), auth: .password,
            routes: [.manual("192.168.1.20")], lastRoute: "192.168.1.20"
        )
        #expect(RoutePlan.learned(profile, reported: [], last: "192.168.1.20") == nil)
        #expect(RoutePlan.learned(profile, reported: nil, last: nil) == nil)
        let updated = RoutePlan.learned(profile, reported: [HostRoute(host: "studio.local", kind: .local)], last: "studio.local")
        #expect(updated?.routes.map(\.host) == ["192.168.1.20", "studio.local"])
        #expect(updated?.lastRoute == "studio.local")
    }

    @Test func placeReadsAsAPersonWouldSayIt() {
        #expect(RoutePlan.place(of: "studio.tail1234.ts.net", in: []) == .tailscale)
        #expect(RoutePlan.place(of: "studio.local", in: []) == .localNetwork)
        #expect(RoutePlan.place(of: "2001:db8::10", in: reported) == .localNetwork)
        #expect(RoutePlan.place(of: "build.example.com", in: []) == .address("build.example.com"))
    }

    // MARK: Migration

    @Test func singleAddressProfileMigratesToOneManualRoute() throws {
        let json = """
        {
          "id": "6F1C2D4E-8A9B-4C3D-9E0F-112233445566",
          "name": "studio",
          "endpoint": {"host": "192.168.1.20", "port": 2222, "user": "sam"},
          "auth": {"password": {}}
        }
        """
        let profile = try JSONDecoder().decode(HostProfile.self, from: Data(json.utf8))
        #expect(profile.routes == [HostRoute(host: "192.168.1.20", kind: .lan, manual: true)])
        #expect(profile.lastRoute == nil)
        #expect(RoutePlan.candidates(profile) == ["192.168.1.20"])
    }

    @Test func routesRoundTrip() throws {
        let profile = HostProfile(
            id: UUID(), name: "s", endpoint: HostEndpoint(host: "studio.local", user: "sam"), auth: .password,
            routes: [.manual("studio.local")] + reported, lastRoute: "100.101.102.103"
        )
        let decoded = try JSONDecoder().decode(HostProfile.self, from: JSONEncoder().encode(profile))
        #expect(decoded == profile)
    }

    @Test func hostInfoRoutesDecodeAndSkipUnknownKinds() throws {
        let json = """
        {"interfaces": [], "wakeOnLan": null, "routes": [
          {"kind": "mdns", "host": "studio.local"},
          {"kind": "lan", "host": "192.168.1.20", "interface": "en0"},
          {"kind": "carrier-pigeon", "host": "coop"},
          {"kind": "tailscale", "host": "100.101.102.103"}
        ]}
        """
        let info = try JSONDecoder().decode(HostInfo.self, from: Data(json.utf8))
        #expect(info.hostRoutes?.map(\.kind) == [.local, .lan, .tailscale])
        let old = try JSONDecoder().decode(HostInfo.self, from: Data(#"{"interfaces": [], "wakeOnLan": true}"#.utf8))
        #expect(old.hostRoutes == nil)
    }

    // MARK: This device's tailnet

    @Test func phoneIsOnATailnetOnlyWithACGNATAddressOnAUtun() {
        let tailnet = PhoneTailnet.Address(interface: "utun4", ipv4: 0x6465_6667)
        let wifi = PhoneTailnet.Address(interface: "en0", ipv4: 0xC0A8_0114)
        #expect(PhoneTailnet.isTailnet([wifi, tailnet]))
        #expect(!PhoneTailnet.isTailnet([wifi]))
        // A carrier's CGNAT address on cellular is not a tailnet.
        #expect(!PhoneTailnet.isTailnet([PhoneTailnet.Address(interface: "pdp_ip0", ipv4: 0x6440_0001)]))
        // A VPN on a utun outside 100.64.0.0/10 is not Tailscale.
        #expect(!PhoneTailnet.isTailnet([PhoneTailnet.Address(interface: "utun2", ipv4: 0x0A08_0001)]))
        #expect(!PhoneTailnet.isTailnet([PhoneTailnet.Address(interface: "utun2", ipv4: 0x6480_0001)]))
    }
}
