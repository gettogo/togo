import Foundation
import Testing
@testable import TogoKit

struct WakeOnLANTests {
    @Test func packetIs102Bytes() throws {
        let packet = try WakeOnLAN.packet(mac: "02:00:00:aa:bb:cc")
        #expect(packet.count == 102)
        #expect(packet.prefix(6) == Data(repeating: 0xFF, count: 6))
        let mac = Data([0x02, 0x00, 0x00, 0xAA, 0xBB, 0xCC])
        for copy in 0..<16 {
            #expect(packet.subdata(in: (6 + copy * 6)..<(12 + copy * 6)) == mac)
        }
    }

    @Test func dashesAndCaseAreAccepted() throws {
        #expect(try WakeOnLAN.packet(mac: "02-00-00-AA-BB-CC") == WakeOnLAN.packet(mac: "02:00:00:aa:bb:cc"))
    }

    @Test(arguments: [
        "", "02:00:00:aa:bb", "02:00:00:aa:bb:cc:dd", "02:00:00:aa:bb:zz", "02:00-00:aa:bb:cc",
        "0200.00aa.bbcc", "02:00:00:aa:bb:c", " 02:00:00:aa:bb:cc", "02:00:00:aa:bb:cc\n",
        "ff:ff:ff:ff:ff:ff", "00:00:00:00:00:00", "01:00:5e:00:00:01", "33:33:00:00:00:01",
    ])
    func badMacThrows(_ mac: String) {
        #expect(throws: WakeError.self) { try WakeOnLAN.packet(mac: mac) }
    }

    @Test func broadcastsFor24() {
        #expect(WakeOnLAN.broadcasts(for: ["192.168.1.20"]) == ["192.168.1.255", "255.255.255.255"])
        #expect(WakeOnLAN.broadcasts(for: ["10.0.3.7", "172.16.9.1", "192.168.1.20", "192.168.1.21"])
            == ["10.0.3.255", "172.16.9.255", "192.168.1.255", "255.255.255.255"])
    }

    @Test func broadcastsKeepToPrivateRanges() {
        #expect(WakeOnLAN.broadcasts(for: ["8.8.8.8", "172.32.0.1", "172.15.0.1", "100.64.0.1", "169.254.1.1"])
            == ["255.255.255.255"])
        #expect(WakeOnLAN.broadcasts(for: ["172.31.255.1"]) == ["172.31.255.255", "255.255.255.255"])
        #expect(WakeOnLAN.broadcasts(for: ["fe80::1", "not an address", "192.168.1", "192.168.1.1.1", "192.168.1.300"])
            == ["255.255.255.255"])
        #expect(WakeOnLAN.broadcasts(for: []) == ["255.255.255.255"])
    }

    @Test func sendRefusesForeignBroadcastsAndBadMacs() async {
        await #expect(throws: WakeError.self) {
            try await WakeOnLAN.send(WakeTarget(mac: "02:00:00:aa:bb:cc", broadcast: ["8.8.8.8"]))
        }
        await #expect(throws: WakeError.self) {
            try await WakeOnLAN.send(WakeTarget(mac: "ff:ff:ff:ff:ff:ff", broadcast: ["255.255.255.255"]))
        }
        await #expect(throws: WakeError.self) {
            try await WakeOnLAN.send(WakeTarget(mac: "02:00:00:aa:bb:cc", broadcast: []))
        }
    }

    @Test func broadcastValidation() {
        #expect(WakeOnLAN.isAllowedBroadcast("192.168.1.255"))
        #expect(WakeOnLAN.isAllowedBroadcast("255.255.255.255"))
        #expect(!WakeOnLAN.isAllowedBroadcast("192.168.1.20"))
        #expect(!WakeOnLAN.isAllowedBroadcast("8.8.8.255"))
        #expect(!WakeOnLAN.isAllowedBroadcast("224.0.0.1"))
    }

    // MARK: Target from host.info

    private func info(_ interfaces: [HostInterface], wol: Bool? = true) -> HostInfo {
        HostInfo(interfaces: interfaces, wakeOnLan: wol)
    }

    @Test func targetUsesTheFirstRealInterface() {
        let target = WakeTarget(
            from: info([
                HostInterface(name: "utun3", mac: "02:00:00:00:00:09", ipv4: ["10.8.0.2"]),
                HostInterface(name: "bridge100", mac: "02:00:00:00:00:08", ipv4: ["192.168.64.1"]),
                HostInterface(name: "en0", mac: "A4:83:E7:00:11:22", ipv4: ["192.168.1.20"]),
                HostInterface(name: "en1", mac: "a4:83:e7:00:11:33", ipv4: ["192.168.1.21"]),
            ]))
        #expect(target == WakeTarget(mac: "a4:83:e7:00:11:22", broadcast: ["192.168.1.255", "255.255.255.255"], enabled: true))
    }

    @Test func noTargetWithoutAUsableInterface() {
        #expect(WakeTarget(from: info([])) == nil)
        #expect(WakeTarget(from: info([HostInterface(name: "en0", mac: "ff:ff:ff:ff:ff:ff", ipv4: ["192.168.1.2"])])) == nil)
        #expect(WakeTarget(from: info([HostInterface(name: "en0", mac: "a4:83:e7:00:11:22", ipv4: ["8.8.8.8"])])) == nil)
        #expect(WakeTarget(from: info([HostInterface(name: "docker0", mac: "a4:83:e7:00:11:22", ipv4: ["172.17.0.1"])])) == nil)
    }

    @Test func recordCodesWithoutWake() throws {
        var record = HostRecord(
            id: UUID(), profile: HostProfile(id: UUID(), name: "a", endpoint: HostEndpoint(host: "h", port: 22, user: "u"), auth: .password),
            symbol: "laptop")
        #expect(record.wake == nil)
        record.wake = WakeTarget(mac: "a4:83:e7:00:11:22", broadcast: ["255.255.255.255"])
        let again = try JSONDecoder().decode(HostRecord.self, from: JSONEncoder().encode(record))
        #expect(again == record)
        record.wake = nil
        let json = String(decoding: try JSONEncoder().encode(record), as: UTF8.self)
        #expect(!json.contains("wake"))
    }

    // MARK: Reading host.info on connect

    @Test func monitorReadsHostInfoOnConnect() async throws {
        let info = HostInfo(interfaces: [HostInterface(name: "en0", mac: "a4:83:e7:00:11:22", ipv4: ["192.168.1.20"])], wakeOnLan: true)
        let source = FakeSource(
            snapshots: [EventSnapshot(seq: 0, sessions: [], approvals: [])],
            capabilities: [Capability.events, Capability.hostInfo]
        )
        source.setHostInfo(info)
        let monitor = MonitorFixtures.monitor(ScriptedFactory([.source(source)]), clock: ManualClock())
        let log = SnapshotLog(monitor)
        await monitor.start()
        #expect(await eventually { log.last?.hostInfo == info })
        #expect(source.hostInfoCalls == 1)
        await monitor.stop()
    }

    // MARK: Bounds on what a machine reports

    @Test func storedBroadcastsAreCappedAtFour() {
        let many = (1...50).map { "10.0.\($0).5" }
        let list = WakeOnLAN.broadcasts(for: many)
        #expect(list.count == 4)
        #expect(list.last == "255.255.255.255")
    }

    @Test func onlyTheFirstFourAddressesOfAnInterfaceCount() throws {
        let addresses = (1...20).map { "10.0.\($0).5" }
        let target = try #require(WakeTarget(from: info([HostInterface(name: "en0", mac: "a4:83:e7:00:11:22", ipv4: addresses)])))
        #expect(target.broadcast == ["10.0.1.255", "10.0.2.255", "10.0.3.255", "255.255.255.255"])
        let late = ["8.8.8.8", "8.8.4.4", "1.1.1.1", "9.9.9.9", "192.168.1.5"]
        #expect(WakeTarget(from: info([HostInterface(name: "en0", mac: "a4:83:e7:00:11:22", ipv4: late)])) == nil)
    }

    @Test func onlyTheFirstEightInterfacesAreExamined() {
        let junk = (0..<8).map { HostInterface(name: "utun\($0)", mac: "02:00:00:00:00:0\($0)", ipv4: []) }
        let real = HostInterface(name: "en0", mac: "a4:83:e7:00:11:22", ipv4: ["192.168.1.20"])
        #expect(WakeTarget(from: info(junk + [real])) == nil)
        #expect(WakeTarget(from: info(Array(junk.prefix(7)) + [real])) != nil)
    }

    @Test func sendRefusesMoreThanFourBroadcasts() async {
        let many = (1...5).map { "10.0.\($0).255" }
        await #expect(throws: WakeError.noBroadcast) {
            try await WakeOnLAN.send(WakeTarget(mac: "02:00:00:aa:bb:cc", broadcast: many))
        }
    }
}
