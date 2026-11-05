import Foundation
import Synchronization
import Testing
import TogoKit
import struct TogoPush.Pairing
import class TogoPush.PairingStore
import class TogoPush.PendingUnpairStore
import class TogoPush.InMemoryDeviceKeys
@testable import Togo

/// A helper that signs in only while the machine is reachable and its saved password is
/// still on the device, and records each unpair.
private final class PasswordHelper: HelperCalling {
    struct Unreachable: Error {}
    let reachable = Mutex(false)
    let unpaired = Mutex<[UUID]>([])
    let forgotten: ForgottenPasswords

    init(forgotten: ForgottenPasswords) {
        self.forgotten = forgotten
    }

    func call<T: Sendable>(
        _ record: HostRecord, prompter: (any ConnectionPrompter)?, _ body: @Sendable (HelperClient) async throws -> T
    ) async throws -> T {
        guard reachable.withLock({ $0 }), !forgotten.ids.withLock({ $0.contains(record.id) }),
              let done = () as? T
        else { throw Unreachable() }
        unpaired.withLock { $0.append(record.id) }
        return done
    }

    func drop(_ id: UUID) async {}
    func dropAll() async {}
}

@MainActor struct PendingRemovalTests {
    private let directory = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
    private let forgotten = ForgottenPasswords()

    private func record() -> HostRecord {
        HostRecord(
            id: Samples.studio,
            profile: HostProfile(
                id: Samples.studio, name: "Studio", endpoint: HostEndpoint(host: "studio.local", port: 22, user: "sam"),
                auth: .password
            ),
            symbol: "desktop"
        )
    }

    private func services(helper: PasswordHelper) -> (LiveServices, PairingCoordinator) {
        let now = Samples.now
        let registrar = PushRegistrar(permissions: FakePermissions(.authorized), registerForRemote: {})
        let coordinator = PairingCoordinator(
            keys: InMemoryDeviceKeys(), store: PairingStore(directory: directory),
            unpairs: PendingUnpairStore(directory: directory), registrar: registrar, settings: { AppSettings() },
            liveTokens: { _ in (update: nil, start: nil) }, watch: { [] },
            hostedRelay: URL(string: "https://relay.example.test"), deviceName: "iPhone"
        )
        let forgotten = self.forgotten
        let services = LiveServices(
            hostRecords: SyncedRecordStore(prefix: "host.", local: MemoryBackend(), remote: nil, deviceID: "a", clock: { now }),
            settingsRecords: SyncedRecordStore(prefix: "settings.", local: MemoryBackend(), remote: nil, deviceID: "a", clock: { now }),
            syncSwitch: SyncSwitch(backend: MemoryBackend()),
            dependencies: LiveServices.Dependencies(
                watch: { FakeWatch(record: $0) },
                helpers: helper,
                connect: { _, _, _, _ in { _, _, _ in throw PasswordHelper.Unreachable() } },
                forgetPassword: { id in forgotten.ids.withLock { $0.append(id) } },
                now: { now },
                pairing: coordinator
            )
        )
        return (services, coordinator)
    }

    private func eventually(_ condition: () -> Bool) async {
        for _ in 0..<200 where !condition() {
            try? await Task.sleep(for: .milliseconds(5))
        }
    }

    private func pairStudio() {
        PairingStore(directory: directory).put(Pairing(
            hostId: "h_studio", hostKey: "", relayURL: URL(string: "https://relay.example.test")!, machineName: "Studio",
            appHostID: Samples.studio, pairedAt: Samples.now, tokenHash: "000000", liveActivityCommand: false
        ))
    }

    @Test func offlinePasswordMachineUnpairsLaterWithTheKeptPassword() async throws {
        let helper = PasswordHelper(forgotten: forgotten)
        let (services, _) = services(helper: helper)
        pairStudio()
        await services.start()
        try await services.saveHost(record())

        try await services.removeHost(Samples.studio)
        await eventually { services.pendingRemovals.count == 1 }
        #expect(services.pendingRemovals == [PendingRemoval(id: "h_studio", name: "Studio")])
        #expect(forgotten.ids.withLock { $0 }.isEmpty, "the password is kept for the unpair")

        helper.reachable.withLock { $0 = true }
        services.retryUnpairs()
        await eventually { services.pendingRemovals.isEmpty }

        #expect(helper.unpaired.withLock { $0 } == [Samples.studio])
        #expect(forgotten.ids.withLock { $0 } == [Samples.studio])
        #expect(PendingUnpairStore(directory: directory).all().isEmpty)
    }

    @Test func forgetNowDropsTheEntryAndThePassword() async throws {
        let helper = PasswordHelper(forgotten: forgotten)
        let (services, _) = services(helper: helper)
        pairStudio()
        await services.start()
        try await services.saveHost(record())
        try await services.removeHost(Samples.studio)
        await eventually { services.pendingRemovals.count == 1 }

        services.forgetRemoval("h_studio")
        #expect(services.pendingRemovals.isEmpty)
        #expect(forgotten.ids.withLock { $0 } == [Samples.studio])
        #expect(PendingUnpairStore(directory: directory).all().isEmpty)
    }

    @Test func unpairedMachineForgetsItsPasswordAtOnce() async throws {
        let (services, _) = services(helper: PasswordHelper(forgotten: forgotten))
        await services.start()
        try await services.saveHost(record())
        try await services.removeHost(Samples.studio)
        #expect(forgotten.ids.withLock { $0 } == [Samples.studio])
    }
}
