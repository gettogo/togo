import Foundation
import Testing
import TogoKit
import TogoPush
@testable import Togo

@MainActor struct PairingAnswerReceiptTests {
    private final class Devices { var watches: [WatchDevice] = [] }

    @Test func receiptsAreGatedAndAnOldWatchIsNotOptedIn() async throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let suite = "pairing-answer-receipts-\(UUID())"
        defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
        let store = PairingStore(directory: directory)
        let helper = PairingCoordinatorHelperStub()
        let registrar = PushRegistrar(permissions: FakePermissions(.authorized), registerForRemote: {})
        await registrar.refresh()
        registrar.didRegister(token: Data(repeating: 0xab, count: 32))
        let devices = Devices()
        let keys = InMemoryDeviceKeys()
        devices.watches = [WatchDevice(deviceId: "d_abcdefghijklmnopqrstuvwxyz", name: "Watch",
            x25519: Base64URL.encode(try keys.agreementKey().publicKey.rawRepresentation),
            approvalKey: Base64URL.encode(try keys.signingKey().publicKey.rawRepresentation), apnsToken: "ab")]
        let coordinator = PairingCoordinator(
            keys: InMemoryDeviceKeys(), store: store, unpairs: PendingUnpairStore(directory: directory), registrar: registrar,
            settings: { AppSettings() }, liveTokens: { _ in (nil, nil) }, watch: { devices.watches },
            register: PairingRecordingRegister(), hostedRelay: URL(string: "https://relay.example.test"),
            sentChoices: UserDefaults(suiteName: suite)!, clock: { Samples.now }
        )
        func online(_ supports: Bool) async {
            await coordinator.machineOnline(hostID: Samples.studio, name: "Machine",
                capabilities: supports ? [Capability.push, Capability.answerReceipts] : [Capability.push]) {
                try await helper.pair($0)
            }
        }
        await online(false)
        #expect(helper.pairs.count == 2)
        for pair in helper.pairs {
            let json = try #require(JSONSerialization.jsonObject(with: JSONEncoder().encode(pair)) as? [String: Any])
            #expect(json["answerReceipts"] == nil)
        }
        await online(true)
        #expect(helper.pairs.count == 4)
        #expect(helper.pairs[2].answerReceipts)
        #expect(!helper.pairs[3].answerReceipts, "An old Watch must not receive an unknown push kind")
        #expect(store.pairing(hostId: "h_studio")?.answerReceipts == true)
        await online(true)
        #expect(helper.pairs.count == 4)
        devices.watches[0].answerReceipts = true
        await online(true)
        #expect(helper.pairs.count == 6)
        #expect(helper.pairs[5].answerReceipts)
        await online(false)
        #expect(helper.pairs.suffix(2).allSatisfy { !$0.answerReceipts })
    }
}
