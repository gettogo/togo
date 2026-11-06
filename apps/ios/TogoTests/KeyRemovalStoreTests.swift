import Foundation
import Testing
import TogoKit
@testable import Togo

struct KeyRemovalStoreTests {
    private func record(_ auth: AuthMethodRef) -> HostRecord {
        HostRecord(
            id: Samples.studio,
            profile: HostProfile(
                id: Samples.studio, name: "Studio", endpoint: HostEndpoint(host: "studio.local", port: 22, user: "sam"),
                auth: auth
            ),
            symbol: "desktop"
        )
    }

    @Test func onlyTheKeyTogoMadeIsThisDevicesKey() {
        let lines = ["device": ForgetMachineTests.full, "imported": ForgetMachineTests.full]
        let device = DeviceKeyLine.of(record(.key(id: "device")), deviceKeyID: "device") { lines[$0] }
        #expect(device?.publicKey == ForgetMachineTests.publicKey)
        #expect(DeviceKeyLine.of(record(.key(id: "imported")), deviceKeyID: "device") { lines[$0] } == nil)
        #expect(DeviceKeyLine.of(record(.password), deviceKeyID: "device") { lines[$0] } == nil)
        let foreign = DeviceKeyLine.of(record(.key(id: "device")), deviceKeyID: "device") { _ in
            "\(ForgetMachineTests.publicKey) sam@laptop"
        }
        #expect(foreign == nil, "the helper leaves a line without togo@")
        #expect(DeviceKeyLine(line: "ssh-ed25519 not*base64 togo@x") == nil)
        #expect(DeviceKeyLine(line: "ssh-dss AAAA togo@x") == nil)
    }

    @Test func unreadableStoreIsKeptAside() throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let file = directory.appending(path: "pending-keys.json")
        try Data("{not json".utf8).write(to: file)

        let store = PendingKeyRemovalStore(file: file)
        #expect(store.all().isEmpty)
        #expect(try Data(contentsOf: file.appendingPathExtension("bad")) == Data("{not json".utf8))
        let key = try #require(DeviceKeyLine(line: ForgetMachineTests.full))
        store.put(PendingKeyRemoval(id: Samples.studio, machineName: "Studio", key: key, connection: nil, manual: false))
        #expect(PendingKeyRemovalStore(file: file).all().map(\.id) == [Samples.studio])
    }
}
