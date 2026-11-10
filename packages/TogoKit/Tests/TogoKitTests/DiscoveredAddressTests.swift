import Foundation
import Testing
import TogoKit

@Suite struct DiscoveredAddressTests {
    @Test func hostNameComesFirst() {
        let chosen = DiscoveredAddress.preferred(
            hostName: "Sams-MacBook-Pro.local.", addresses: ["192.168.1.20", "fe80::1c2b:3a4d:5e6f:7081%en0"]
        )
        #expect(chosen == "Sams-MacBook-Pro.local")
    }

    @Test func ipv4BeatsIPv6() {
        let chosen = DiscoveredAddress.preferred(
            hostName: nil, addresses: ["2001:db8::20", "fe80::1%en0", "192.168.1.20"]
        )
        #expect(chosen == "192.168.1.20")
    }

    @Test func routableIPv6WhenThereIsNoIPv4() {
        let chosen = DiscoveredAddress.preferred(hostName: nil, addresses: ["fe80::1%en0", "2001:db8::20"])
        #expect(chosen == "2001:db8::20")
    }

    @Test func neverLinkLocalOrZoned() {
        #expect(DiscoveredAddress.preferred(hostName: nil, addresses: ["fe80::1c2b:3a4d:5e6f:7081%en0"]) == nil)
        #expect(DiscoveredAddress.preferred(hostName: nil, addresses: ["fe80::1c2b:3a4d:5e6f:7081"]) == nil)
        #expect(DiscoveredAddress.preferred(hostName: nil, addresses: ["FE80::1"]) == nil)
        #expect(DiscoveredAddress.preferred(hostName: nil, addresses: ["2001:db8::20%en0"]) == "2001:db8::20")
        #expect(DiscoveredAddress.preferred(hostName: nil, addresses: ["169.254.10.2", "2001:db8::20"]) == "2001:db8::20")
    }

    @Test func blankOrNumericHostNamesFallThrough() {
        #expect(DiscoveredAddress.preferred(hostName: "", addresses: ["192.168.1.20"]) == "192.168.1.20")
        #expect(DiscoveredAddress.preferred(hostName: ".", addresses: ["192.168.1.20"]) == "192.168.1.20")
        #expect(DiscoveredAddress.preferred(hostName: "fe80::1%en0", addresses: ["192.168.1.20"]) == "192.168.1.20")
        #expect(DiscoveredAddress.preferred(hostName: nil, addresses: []) == nil)
    }
}

@Suite struct DiscoveredListingTests {
    @Test func waitsForTheHostNameBeforeGivingUp() {
        #expect(DiscoveredAddress.listed(hostName: nil, addresses: ["fe80::1%en0"], lookupFinished: false) == nil)
    }

    @Test func aMachineWithOnlyALinkLocalAddressIsStillListed() {
        #expect(DiscoveredAddress.listed(hostName: nil, addresses: ["fe80::1%en0"], lookupFinished: true) == "")
        #expect(DiscoveredAddress.listed(hostName: nil, addresses: [], lookupFinished: true) == "")
    }

    @Test func aUsableAddressIsListedAtOnce() {
        #expect(DiscoveredAddress.listed(hostName: nil, addresses: ["192.168.1.20"], lookupFinished: false) == "192.168.1.20")
        #expect(DiscoveredAddress.listed(hostName: "studio.local.", addresses: [], lookupFinished: false) == "studio.local")
    }
}

@Suite struct SavedAddressTidyTests {
    private let name: @Sendable (String) async -> String? = { _ in nil }

    @Test func untouchedWithoutAZone() async {
        #expect(await SavedAddress.tidied("studio.local", hostName: name) == .unchanged)
        #expect(await SavedAddress.tidied("192.168.1.20", hostName: name) == .unchanged)
    }

    @Test func zoneDroppedFromARoutableAddress() async {
        #expect(await SavedAddress.tidied("2001:db8::20%en0", hostName: name) == .replaced("2001:db8::20"))
    }

    @Test func linkLocalBecomesTheMachineName() async {
        let lookUp: @Sendable (String) async -> String? = { address in
            address == "fe80::1c2b%en0" ? "Sams-MacBook-Pro.local." : nil
        }
        #expect(await SavedAddress.tidied("fe80::1c2b%en0", hostName: lookUp) == .replaced("Sams-MacBook-Pro.local"))
    }

    /// Without a name the zoned address is the only form that can still connect on that network.
    @Test func linkLocalWithoutANameIsKeptAndFlagged() async {
        #expect(await SavedAddress.tidied("fe80::1c2b%en0", hostName: name) == .needsAttention)
    }

    @Test func storeRewritesZonedHostsOnce() async {
        let store = HostStore(records: SyncedRecordStore(
            prefix: "host.", local: MemoryBackend(), remote: nil, deviceID: "a", clock: { Date(timeIntervalSince1970: 0) }
        ))
        func record(_ name: String, _ host: String) -> HostRecord {
            HostRecord(
                id: UUID(), profile: HostProfile(id: UUID(), name: name, endpoint: HostEndpoint(host: host, user: "sam"), auth: .password),
                symbol: "laptop"
            )
        }
        let mac = record("Mac", "fe80::1c2b%en0")
        let box = record("Box", "build.local")
        let pad = record("Pad", "fe80::99%en0")
        for item in [mac, box, pad] { await store.save(item) }

        let flagged = await store.tidyAddresses { $0 == "fe80::1c2b%en0" ? "mac.local." : nil }
        let hosts = Dictionary(uniqueKeysWithValues: await store.hosts().map { ($0.profile.name, $0.profile.endpoint.host) })
        #expect(hosts == ["Mac": "mac.local", "Box": "build.local", "Pad": "fe80::99%en0"])
        #expect(flagged == [pad.id])
    }

    /// The name lookup is slow. A machine removed while it runs stays removed, and one that
    /// changed meanwhile keeps the change.
    @Test func tidyingKeepsChangesMadeDuringTheLookup() async {
        let store = HostStore(records: SyncedRecordStore(
            prefix: "host.", local: MemoryBackend(), remote: nil, deviceID: "a", clock: { Date(timeIntervalSince1970: 0) }
        ))
        func record(_ name: String, _ host: String) -> HostRecord {
            HostRecord(
                id: UUID(), profile: HostProfile(id: UUID(), name: name, endpoint: HostEndpoint(host: host, user: "sam"), auth: .password),
                symbol: "laptop"
            )
        }
        let mac = record("Mac", "fe80::1c2b%en0")
        let pad = record("Pad", "fe80::99%en0")
        for item in [mac, pad] { await store.save(item) }

        _ = await store.tidyAddresses { address in
            if address == "fe80::1c2b%en0" {
                var renamed = mac
                renamed.symbol = "desktopcomputer"
                await store.save(renamed)
                return "mac.local."
            }
            await store.remove(pad.id)
            return "pad.local."
        }
        let hosts = await store.hosts()
        #expect(hosts.map(\.id) == [mac.id], "the removed machine is not saved back")
        #expect(hosts.first?.profile.endpoint.host == "mac.local")
        #expect(hosts.first?.symbol == "desktopcomputer", "the change made during the lookup stays")
    }
}
