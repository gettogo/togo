import Foundation
import Synchronization
import Testing
import TogoKit
import struct TogoPush.Pairing
import class TogoPush.PairingStore
import class TogoPush.PendingUnpairStore
import class TogoPush.InMemoryDeviceKeys
import enum TogoPush.Base64URL
@testable import Togo

private final class EventLog: Sendable {
    let events = Mutex<[String]>([])
}

/// Forgetting a machine unpairs it, then takes this device's key off it, then forgets it.
@MainActor struct ForgetMachineTests {
    private let directory = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
    private let keys = InMemoryDeviceKeys()
    /// What happened, in order: helper requests that remove something, then "forget" when the
    /// saved password and the rest of the machine go.
    private let log = EventLog()

    nonisolated static let blob = "AAAAC3NzaC1lZDI1NTE5AAAAIAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4f"
    nonisolated static let publicKey = "ssh-ed25519 \(blob)"
    nonisolated static let full = "\(publicKey) togo@Sams-iPhone"
    private static let pushAndKeys = ["session.v1", "push.v1", "authkeys.v1"]

    private func record(
        auth: AuthMethodRef = .key(id: "k1"), id: HostID = Samples.studio, host: String = "studio.local"
    ) -> HostRecord {
        HostRecord(
            id: id,
            profile: HostProfile(
                id: id, name: "Studio", endpoint: HostEndpoint(host: host, port: 22, user: "sam"), auth: auth
            ),
            symbol: "desktop"
        )
    }

    private func coordinator(registrar: PushRegistrar? = nil, line: String = full) -> PairingCoordinator {
        let coordinator = PairingCoordinator(
            keys: keys, store: PairingStore(directory: directory),
            unpairs: PendingUnpairStore(directory: directory),
            registrar: registrar ?? PushRegistrar(permissions: FakePermissions(.authorized), registerForRemote: {}),
            settings: { AppSettings() }, liveTokens: { _ in (update: nil, start: nil) }, watch: { [] },
            keyRemovals: PendingKeyRemovalStore(file: directory.appending(path: "pending-keys.json")),
            hostedRelay: URL(string: "https://relay.example.test"), deviceName: "iPhone"
        )
        coordinator.deviceKey = { record in
            guard case .key = record.profile.auth else { return nil }
            return DeviceKeyLine(line: line)
        }
        return coordinator
    }

    private func services(
        helper: ScriptedHelper, watches: Watches = Watches(), line: String = full
    ) -> (LiveServices, PairingCoordinator) {
        let now = Samples.now
        let log = self.log
        let coordinator = coordinator(line: line)
        let services = LiveServices(
            hostRecords: SyncedRecordStore(prefix: "host.", local: MemoryBackend(), remote: nil, deviceID: "a", clock: { now }),
            settingsRecords: SyncedRecordStore(prefix: "settings.", local: MemoryBackend(), remote: nil, deviceID: "a", clock: { now }),
            syncSwitch: SyncSwitch(backend: MemoryBackend()),
            dependencies: LiveServices.Dependencies(
                watch: { record in
                    let watch = FakeWatch(record: record)
                    watches.made.withLock { $0[record.id] = watch }
                    return watch
                },
                helpers: helper,
                connect: { _, _, _, _ in { _, _, _ in throw ScriptedHelper.Unreachable() } },
                forgetPassword: { _ in
                    log.events.withLock { $0 = Self.removals(helper) + ["forget"] }
                },
                now: { now },
                pairing: coordinator
            )
        )
        return (services, coordinator)
    }

    /// The requests that remove something, leaving out what the services ask a machine online.
    private static func removals(_ helper: ScriptedHelper) -> [String] {
        helper.requests.map(\.method).filter { ["authkeys.remove", "push.unpair"].contains($0) }
    }

    private func pairStudio() {
        PairingStore(directory: directory).put(Pairing(
            hostId: "h_studio", hostKey: "", relayURL: URL(string: "https://relay.example.test")!, machineName: "Studio",
            appHostID: Samples.studio, pairedAt: Samples.now, tokenHash: "000000", liveActivityCommand: false
        ))
    }

    private func eventually(_ condition: () -> Bool) async {
        for _ in 0..<400 where !condition() {
            try? await Task.sleep(for: .milliseconds(5))
        }
    }

    /// A paired Studio, added and seen online with `capabilities`.
    private func onlineStudio(
        helper: ScriptedHelper, capabilities: [String] = pushAndKeys, line: String = full
    ) async throws -> (LiveServices, PairingCoordinator) {
        let watches = Watches()
        let (services, coordinator) = services(helper: helper, watches: watches, line: line)
        pairStudio()
        await services.start()
        try await services.saveHost(record())
        let watch = try #require(watches[Samples.studio])
        watch.sink.yield(HostSnapshot(
            hostID: Samples.studio, reach: .online, helper: .present(version: "0.4.0"), capabilities: Set(capabilities)
        ))
        await eventually { services.isOnline(Samples.studio) }
        return (services, coordinator)
    }

    // MARK: Pairing

    @Test func pairSendsSSHKey() async throws {
        let registrar = PushRegistrar(permissions: FakePermissions(.authorized), registerForRemote: {})
        await registrar.refresh()
        registrar.didRegister(token: Data(repeating: 0xab, count: 32))
        let coordinator = coordinator(registrar: registrar)
        let sent = Mutex<[PushPairParams]>([])
        let pair: (PushPairParams) async throws -> PushPairResult = { params in
            sent.withLock { $0.append(params) }
            return PushPairResult(hostId: "h_studio", hostKey: Base64URL.encode(Data(repeating: 7, count: 32)))
        }

        let key = try #require(coordinator.deviceKey(record()))
        #expect(key.publicKey == Self.publicKey, "the line goes without its comment")
        await coordinator.machineOnline(
            hostID: Samples.studio, name: "Studio", capabilities: ["push.v1", "authkeys.v1"], sshKey: key.publicKey, pair: pair
        )
        #expect(sent.withLock { $0.map(\.sshKey) } == [Self.publicKey])

        // A helper without authkeys.v1 turns down a field it does not know, so it never gets one.
        await coordinator.machineOnline(hostID: HostID(), name: "Old", capabilities: ["push.v1"], sshKey: key.publicKey, pair: pair)
        #expect(sent.withLock { $0.last?.sshKey } == nil)
        #expect(coordinator.deviceKey(record(auth: .password)) == nil)
    }

    @Test func pairingAgainSendsAKeyItDidNotHave() async throws {
        let registrar = PushRegistrar(permissions: FakePermissions(.authorized), registerForRemote: {})
        await registrar.refresh()
        registrar.didRegister(token: Data(repeating: 0xab, count: 32))
        let coordinator = coordinator(registrar: registrar)
        let count = Mutex(0)
        let pair: (PushPairParams) async throws -> PushPairResult = { _ in
            count.withLock { $0 += 1 }
            return PushPairResult(hostId: "h_studio", hostKey: Base64URL.encode(Data(repeating: 7, count: 32)))
        }
        await coordinator.machineOnline(hostID: Samples.studio, name: "Studio", capabilities: ["push.v1"], pair: pair)
        await coordinator.machineOnline(hostID: Samples.studio, name: "Studio", capabilities: ["push.v1"], pair: pair)
        #expect(count.withLock { $0 } == 1)
        await coordinator.machineOnline(
            hostID: Samples.studio, name: "Studio", capabilities: ["push.v1", "authkeys.v1"], sshKey: Self.publicKey,
            pair: pair
        )
        #expect(count.withLock { $0 } == 2, "an upgraded helper hears the key once")
    }

    // MARK: Removing

    @Test func removeCallsUnpairThenKeyThenForget() async throws {
        let helper = ScriptedHelper(capabilities: Self.pushAndKeys)
        helper.answer("authkeys.remove", #"{"removed":1}"#)
        let (services, coordinator) = try await onlineStudio(helper: helper)
        #expect(services.removesKey(Samples.studio))

        try await services.removeHost(Samples.studio)
        await coordinator.settled()

        #expect(log.events.withLock { $0 } == ["push.unpair", "authkeys.remove", "forget"])
        let removal = try #require(helper.requests.first { $0.method == "authkeys.remove" })
        #expect(removal.params == ["publicKey": .string(Self.publicKey)], "only the public key, without its comment")
        #expect(services.hosts.isEmpty)
        #expect(services.pendingRemovals.isEmpty)
    }

    @Test func failedUnpairKeepsTheKeyUntilUnpaired() async throws {
        let helper = ScriptedHelper(capabilities: Self.pushAndKeys)
        helper.fail("push.unpair", code: RPCErrorCode.internal, data: "null")
        helper.answer("authkeys.remove", #"{"removed":1}"#)
        let (services, coordinator) = try await onlineStudio(helper: helper)

        try await services.removeHost(Samples.studio)
        await coordinator.settled()
        #expect(Self.removals(helper) == ["push.unpair"], "the key stays while the machine still has to be reached")
        #expect(services.pendingRemovals.map(\.kind) == [.unpair, .key(comment: "togo@Sams-iPhone", manual: false)])

        helper.answer("push.unpair", "{}")
        await services.retryUnpairs()?.value
        #expect(Self.removals(helper) == ["push.unpair", "push.unpair", "authkeys.remove"])
        let methods = helper.requests.map(\.method)
        let unpaired = try #require(methods.lastIndex(of: "push.unpair"))
        let removed = try #require(methods.firstIndex(of: "authkeys.remove"))
        #expect(!helper.dropsAt.withLock { $0 }.contains { $0 > unpaired && $0 <= removed }, "one channel for both")
        #expect(services.pendingRemovals.isEmpty)
    }

    @Test func keyThatIsNotThereCountsAsRemoved() async throws {
        // No line, or no authorized_keys at all: either way nothing is left to remove.
        for missingFile in [false, true] {
            let helper = ScriptedHelper(capabilities: Self.pushAndKeys)
            if missingFile {
                helper.fail("authkeys.remove", code: RPCErrorCode.conflict, data: #"{"reason":"missing"}"#)
            } else {
                helper.answer("authkeys.remove", #"{"removed":0}"#)
            }
            let (services, coordinator) = try await onlineStudio(helper: helper)
            try await services.removeHost(Samples.studio)
            await coordinator.settled()
            #expect(services.pendingRemovals.isEmpty)
        }
    }

    @Test func unreachableQueuesKeyRemoval() async throws {
        let helper = ScriptedHelper(capabilities: Self.pushAndKeys)
        helper.answer("authkeys.remove", #"{"removed":1}"#)
        helper.reachable.withLock { $0 = false }
        let (services, coordinator) = services(helper: helper)
        pairStudio()
        await services.start()
        try await services.saveHost(record())

        try await services.removeHost(Samples.studio)
        await coordinator.settled()
        #expect(services.hosts.isEmpty, "the machine is forgotten at once")
        let key = try #require(services.pendingRemovals.first { $0.kind != .unpair })
        #expect(key.name == "Studio")
        #expect(key.kind == .key(comment: "togo@Sams-iPhone", manual: false))
        #expect(key.footer == "If Studio can't be reached, run togo devices revoke on it later.")

        helper.reachable.withLock { $0 = true }
        await services.retryUnpairs()?.value
        #expect(services.pendingRemovals.isEmpty)
        #expect(Self.removals(helper) == ["push.unpair", "authkeys.remove"])
    }

    @Test func machineAddedAgainKeepsItsKey() async throws {
        let helper = ScriptedHelper(capabilities: Self.pushAndKeys)
        helper.reachable.withLock { $0 = false }
        let (services, coordinator) = services(helper: helper)
        await services.start()
        try await services.saveHost(record())
        try await services.removeHost(Samples.studio)
        await coordinator.settled()
        #expect(services.pendingRemovals.count == 1)

        try await services.saveHost(record(id: HostID(), host: "Studio.local"))
        helper.reachable.withLock { $0 = true }
        await services.retryUnpairs()?.value
        #expect(services.pendingRemovals.isEmpty)
        #expect(!helper.requests.contains { $0.method == "authkeys.remove" }, "the same machine signs in with it again")
    }

    @Test func conflictShowsManualStep() async throws {
        let helper = ScriptedHelper(capabilities: Self.pushAndKeys)
        helper.fail("authkeys.remove", code: RPCErrorCode.conflict, data: #"{"reason":"symlink"}"#)
        let (services, coordinator) = try await onlineStudio(helper: helper)

        try await services.removeHost(Samples.studio)
        await coordinator.settled()

        #expect(log.events.withLock { $0 } == ["push.unpair", "authkeys.remove", "forget"])
        let removal = try #require(services.pendingRemovals.first)
        #expect(services.pendingRemovals.count == 1)
        #expect(removal.kind == .key(comment: "togo@Sams-iPhone", manual: true))
        #expect(removal.footer == "Remove the line ending in togo@Sams-iPhone from ~/.ssh/authorized_keys on Studio.")

        // A manual step is not retried; the user dismisses it.
        await services.retryUnpairs()?.value
        #expect(helper.requests.filter { $0.method == "authkeys.remove" }.count == 1)
        services.forgetRemoval(removal.id)
        #expect(services.pendingRemovals.isEmpty)
    }

    @Test func refusedKeyIsAManualStep() async throws {
        let helper = ScriptedHelper(capabilities: Self.pushAndKeys)
        helper.fail("authkeys.remove", code: RPCErrorCode.invalidParams, data: #"{"reason":"key"}"#)
        let (services, coordinator) = try await onlineStudio(helper: helper)
        try await services.removeHost(Samples.studio)
        await coordinator.settled()
        #expect(services.pendingRemovals.map(\.kind) == [.key(comment: "togo@Sams-iPhone", manual: true)])
    }

    @Test func helperWithoutAuthkeysShowsManualStep() async throws {
        let helper = ScriptedHelper(capabilities: ["session.v1", "push.v1"])
        let (services, coordinator) = try await onlineStudio(helper: helper, capabilities: ["session.v1", "push.v1"])
        #expect(!services.removesKey(Samples.studio), "the question does not promise it")

        try await services.removeHost(Samples.studio)
        await coordinator.settled()

        #expect(log.events.withLock { $0 } == ["push.unpair", "forget"])
        #expect(services.pendingRemovals.map(\.kind) == [.key(comment: "togo@Sams-iPhone", manual: true)])
    }

    @Test func importedKeyIsLeftAlone() async throws {
        let helper = ScriptedHelper(capabilities: Self.pushAndKeys)
        let (services, coordinator) = try await onlineStudio(helper: helper, line: "\(Self.publicKey) sam@laptop")
        #expect(!services.removesKey(Samples.studio), "the helper would leave a line without togo@")

        try await services.removeHost(Samples.studio)
        await coordinator.settled()
        #expect(log.events.withLock { $0 } == ["push.unpair", "forget"])
        #expect(services.pendingRemovals.isEmpty)
    }

    @Test func machineSharingTheKeyKeepsIt() async throws {
        let helper = ScriptedHelper(capabilities: Self.pushAndKeys)
        helper.reachable.withLock { $0 = false }
        let (services, coordinator) = services(helper: helper)
        await services.start()
        try await services.saveHost(record())
        try await services.saveHost(record(id: HostID(), host: "STUDIO.local"))
        #expect(!services.removesKey(Samples.studio))

        try await services.removeHost(Samples.studio)
        await coordinator.settled()
        #expect(services.pendingRemovals.isEmpty)
        #expect(services.hosts.count == 1)
    }

    @Test func machineAtTheAddressWithAPasswordDoesNotKeepTheKey() async throws {
        let helper = ScriptedHelper(capabilities: Self.pushAndKeys)
        helper.reachable.withLock { $0 = false }
        let (services, coordinator) = services(helper: helper)
        await services.start()
        try await services.saveHost(record())
        try await services.saveHost(record(auth: .password, id: HostID()))
        #expect(services.removesKey(Samples.studio))

        try await services.removeHost(Samples.studio)
        await coordinator.settled()
        #expect(services.pendingRemovals.map(\.kind) == [.key(comment: "togo@Sams-iPhone", manual: false)])
    }

    @Test func passwordMachineHasNoKeyToRemove() async throws {
        let helper = ScriptedHelper(capabilities: Self.pushAndKeys)
        helper.reachable.withLock { $0 = false }
        let (services, coordinator) = services(helper: helper)
        await services.start()
        try await services.saveHost(record(auth: .password))
        #expect(!services.removesKey(Samples.studio))
        try await services.removeHost(Samples.studio)
        await coordinator.settled()
        #expect(services.pendingRemovals.isEmpty)
    }
}
