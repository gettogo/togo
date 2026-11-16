import Foundation
import Testing
@testable import TogoKit

struct HostFieldUpdateTests {
    @Test func concurrentLearnedFieldsPreserveTheEditedName() async throws {
        let records = SyncedRecordStore<HostRecord>(
            prefix: "host.", local: MemoryBackend(), remote: nil, deviceID: "test", clock: { Date() }
        )
        let hosts = HostStore(records: records)
        let id = UUID()
        let record = HostRecord(id: id, profile: HostProfile(
            id: id, name: "before", endpoint: HostEndpoint(host: "example.test", user: "test"), auth: .password
        ), symbol: "desktopcomputer")
        await hosts.save(record)
        let wake = WakeTarget(mac: "02:00:00:00:00:01", broadcast: ["255.255.255.255"], enabled: true)
        async let name = hosts.update(id) { $0.profile.name = "edited" }
        async let address = hosts.update(id) { $0.profile.routes = [.manual("new.example.test")] }
        async let target = hosts.update(id) { $0.wake = wake }
        _ = await (name, address, target)
        let current = try #require(await hosts.hosts().first)
        #expect(current.profile.name == "edited")
        #expect(current.profile.routes == [.manual("new.example.test")])
        #expect(current.wake == wake)
        await hosts.remove(id)
        #expect(await hosts.update(id) { $0.wake = wake } == nil)
        #expect(await hosts.hosts().isEmpty)
    }
}
