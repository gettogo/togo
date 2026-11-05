import Testing
import TogoPush

struct DeviceIdentityTests {
    @Test func watchDeviceIDsHaveTheirOwnPrefix() throws {
        let id = try InMemoryDeviceKeys().deviceID()
        #expect(id.hasPrefix("w_"))
        #expect(id.count == 28)
    }

    @Test func watchSharesItsOwnGroup() {
        #expect(SharedGroup.watch == "group.app.togo.watch")
    }
}
