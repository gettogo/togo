import Foundation
import Testing
import TogoKit
import TogoPush
@testable import Togo

@MainActor struct PairingReceiptCapabilityTests {
    @Test func repairsAnOtherwiseCurrentPairingWhenReceiptSupportChanges() async throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let store = PairingStore(directory: directory)
        let helper = PairingCoordinatorHelperStub()
        let registrar = PushRegistrar(permissions: FakePermissions(.authorized), registerForRemote: {})
        await registrar.refresh()
        registrar.didRegister(token: Data(repeating: 0xab, count: 32))
        let coordinator = PairingCoordinator(
            keys: InMemoryDeviceKeys(), store: store, unpairs: PendingUnpairStore(directory: directory), registrar: registrar,
            settings: { AppSettings() }, liveTokens: { _ in (nil, nil) }, watch: { [] },
            register: PairingRecordingRegister(), hostedRelay: URL(string: "https://relay.example.test"),
            sentChoices: UserDefaults(suiteName: "pairing-receipt-\(UUID().uuidString)")!, clock: { Samples.now }
        )
        func online(_ capabilities: Set<String>) async {
            await coordinator.machineOnline(hostID: Samples.studio, name: "Studio", capabilities: capabilities) {
                try await helper.pair($0)
            }
        }
        await online([Capability.push])
        #expect(store.pairing(hostId: "h_studio")?.liveContextReceipts == false)
        await online([Capability.push, Capability.liveContext])
        #expect(helper.pairs.count == 2)
        #expect(store.pairing(hostId: "h_studio")?.liveContextReceipts == true)
        await online([Capability.push, Capability.liveContext])
        #expect(helper.pairs.count == 2)
        await online([Capability.push])
        #expect(store.pairing(hostId: "h_studio")?.liveContextReceipts == false)
    }
}
