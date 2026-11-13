import Foundation
import Synchronization
import Testing
import TogoKit
import TogoPush
@testable import Togo

@MainActor struct PairingCoordinatorTests {
    private let machine = Samples.studio
    private let directory = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
    private var store: PairingStore { PairingStore(directory: directory) }
    private var unpairs: PendingUnpairStore { PendingUnpairStore(directory: directory) }
    private let keys = InMemoryDeviceKeys()
    private let register = PairingRecordingRegister()
    private let helper = PairingCoordinatorHelperStub()
    private let sentChoices = UserDefaults(suiteName: "pairing-\(UUID().uuidString)")!

    private static let relay = URL(string: "https://relay.example.test")
    private static let tokenA = Data(repeating: 0xab, count: 32)
    private static let tokenB = Data(repeating: 0xcd, count: 32)

    private final class Inputs {
        var settings = AppSettings()
        var watches: [WatchDevice] = []
        var updateTokens: [String: String] = [:]
        var startToken: String?
    }

    private func coordinator(
        _ inputs: Inputs = Inputs(), permission: PushRegistrar.Authorization = .authorized,
        runtimeForeground: (@MainActor () -> Bool)? = nil
    ) async -> (PairingCoordinator, PushRegistrar) {
        let registrar = PushRegistrar(permissions: FakePermissions(permission), registerForRemote: {})
        await registrar.refresh()
        registrar.didRegister(token: Self.tokenA)
        let coordinator = PairingCoordinator(
            keys: keys, store: store, unpairs: unpairs, registrar: registrar,
            settings: { inputs.settings }, liveTokens: { id in (id.flatMap { inputs.updateTokens[$0] }, inputs.startToken) },
            watch: { inputs.watches },
            register: register, hostedRelay: Self.relay, sentChoices: sentChoices, deviceName: "Sam's iPhone",
            clock: { Samples.now }, foreground: runtimeForeground
        )
        return (coordinator, registrar)
    }
    private func online(_ coordinator: PairingCoordinator, capabilities: Set<String> = ["push.v1"]) async {
        let helper = self.helper
        await coordinator.machineOnline(hostID: machine, name: "Studio", capabilities: capabilities) { try await helper.pair($0) }
    }

    @Test func backgroundLaunchRoutesTokensWithoutAWindow() async {
        let inputs = Inputs()
        inputs.updateTokens["h_studio"] = "aaaa"
        let (coordinator, _) = await coordinator(inputs, runtimeForeground: { false })
        await online(coordinator)
        #expect(coordinator.isForeground)
        await coordinator.tokensChanged()
        #expect(register.sent.withLock { $0.count } == 1)
        #expect(register.live.withLock { $0["h_studio"] } == "aaaa")
    }

    @Test func activeApplicationUsesRepairDespiteAnOldBackgroundFlag() async {
        let (coordinator, _) = await coordinator(runtimeForeground: { true })
        await online(coordinator)
        coordinator.isForeground = false
        var repairs = 0
        coordinator.onRepairNeeded = { repairs += 1 }
        await coordinator.tokensChanged()
        #expect(repairs == 1)
        #expect(register.sent.withLock { $0.isEmpty })
    }

    @Test func pairsWhenPushCapable() async throws {
        let (coordinator, _) = await coordinator()
        await online(coordinator)

        let params = try #require(helper.pairs.first)
        #expect(helper.pairs.count == 1)
        #expect(params.deviceId == (try keys.deviceID()))
        #expect(params.name == "Sam's iPhone")
        #expect(params.apnsToken == String(repeating: "ab", count: 32))
        #expect(params.x25519 == Base64URL.encode(try keys.agreementKey().publicKey.rawRepresentation))
        #expect(params.approvalKey == Base64URL.encode(try keys.signingKey().publicKey.rawRepresentation))
        #expect(params.env == .sandbox)
        #expect(params.platform == .ios)
        #expect(params.relayUrl == "https://relay.example.test")
        #expect(!params.liveActivityCommand)
        let stored = try #require(store.pairing(hostId: "h_studio"))
        #expect(stored.appHostID == machine)
        #expect(stored.machineName == "Studio")
        #expect(stored.tokenHash.count == 6)
        #expect(coordinator.pairedMachines == [machine])
    }

    @Test func skipsWithoutPushCapability() async {
        let (coordinator, _) = await coordinator()
        await online(coordinator, capabilities: ["events.v1"])
        #expect(helper.pairs.isEmpty)
        #expect(store.all().isEmpty)
    }

    @Test func skipsWhenNotificationsDenied() async {
        let (coordinator, _) = await coordinator(permission: .denied)
        await online(coordinator)
        #expect(helper.pairs.isEmpty)
    }

    /// The system alert only follows a tap that asked for it, never a machine connecting.
    @Test func neverAsksPermissionByItself() async {
        let permissions = FakePermissions(.notDetermined)
        let registrar = PushRegistrar(permissions: permissions, registerForRemote: {})
        registrar.didRegister(token: Self.tokenA)
        let coordinator = PairingCoordinator(
            keys: keys, store: store, unpairs: unpairs, registrar: registrar, settings: { AppSettings() },
            liveTokens: { _ in (update: nil, start: nil) }, watch: { [] }, register: register, hostedRelay: Self.relay, deviceName: "iPhone"
        )
        coordinator.isForeground = true
        await online(coordinator)
        await online(coordinator)
        #expect(permissions.asked.withLock { $0 } == 0)
        #expect(helper.pairs.isEmpty)

        await registrar.requestAuthorization()
        await online(coordinator)
        #expect(permissions.asked.withLock { $0 } == 1)
        #expect(helper.pairs.count == 1, "once allowed, the machine pairs")
    }

    @Test func liveTokensPerMachine() async {
        let inputs = Inputs()
        inputs.updateTokens = ["h_other": "ee"]
        inputs.startToken = "5151"
        let (coordinator, _) = await coordinator(inputs)
        await online(coordinator)
        inputs.updateTokens["h_studio"] = "aaaa"
        await online(coordinator)

        #expect(helper.pairs.map(\.liveActivityToken) == [nil, "aaaa"])
        #expect(helper.pairs.map(\.liveActivityStartToken) == ["5151", "5151"])

        coordinator.isForeground = false
        await coordinator.tokensChanged()
        #expect(register.live.withLock { $0 } == ["h_studio": "aaaa"])
    }

    @Test func removedMachineReportsRetiredHost() async {
        let (coordinator, _) = await coordinator()
        await online(coordinator)
        var retired: [String] = []
        coordinator.onRetired = { retired.append($0) }

        await coordinator.machineRemoved(hostID: machine, connection: nil, unpair: nil)

        #expect(retired == ["h_studio"])
    }

    @Test func rePairsOnTokenChange() async {
        let (coordinator, registrar) = await coordinator()
        await online(coordinator)
        registrar.didRegister(token: Self.tokenB)
        await online(coordinator)
        #expect(helper.pairs.map(\.apnsToken) == [String(repeating: "ab", count: 32), String(repeating: "cd", count: 32)])
    }

    @Test func noRePairWhenNothingChanged() async {
        let (coordinator, _) = await coordinator()
        await online(coordinator)
        await online(coordinator)
        await online(coordinator)
        #expect(helper.pairs.count == 1)
    }

    /// A helper that restarted may have lost the activity's token: reconnecting sends it again,
    /// and only while an activity runs.
    @Test func reconnectingReportsARunningActivityAgain() async {
        let inputs = Inputs()
        let (coordinator, _) = await coordinator(inputs)
        let helper = self.helper
        let online: (Bool) async -> Void = { reconnected in
            await coordinator.machineOnline(
                hostID: machine, name: "Studio", capabilities: ["push.v1"], reconnected: reconnected
            ) { try await helper.pair($0) }
        }
        await online(true)
        await online(true)
        #expect(helper.pairs.count == 1, "no activity: nothing to report again")

        inputs.updateTokens["h_studio"] = "aaaa"
        await online(false)
        await online(false)
        #expect(helper.pairs.count == 2)
        await online(true)
        #expect(helper.pairs.count == 3)
        #expect(helper.pairs.last?.liveActivityToken == "aaaa")
    }

    @Test func commandTextToggleRepairs() async throws {
        let inputs = Inputs()
        let (coordinator, _) = await coordinator(inputs)
        await online(coordinator)
        helper.pairs = []

        inputs.settings.liveActivityCommandMachines = [machine]
        await online(coordinator)
        inputs.settings.liveActivityCommandMachines = []
        await online(coordinator)

        #expect(helper.pairs.map(\.liveActivityCommand) == [true, false])
        #expect(store.pairing(hostId: "h_studio")?.liveActivityCommand == false)
    }

    @Test func relayChangeRepairs() async {
        let inputs = Inputs()
        let (coordinator, _) = await coordinator(inputs)
        await online(coordinator)
        inputs.settings.relayURL = URL(string: "https://relay.example.com")
        await online(coordinator)
        #expect(helper.pairs.map(\.relayUrl).last == "https://relay.example.com")
        #expect(helper.pairs.count == 2)
    }

    @Test func watchDevicesPairedAlongside() async {
        let inputs = Inputs()
        inputs.watches = [WatchDevice(
            deviceId: "w_abc", name: "Sam's Watch", x25519: Base64URL.encode(Data(repeating: 1, count: 32)),
            approvalKey: Base64URL.encode(Data(repeating: 2, count: 32)), apnsToken: String(repeating: "ef", count: 32)
        )]
        let (coordinator, _) = await coordinator(inputs)
        await online(coordinator)

        #expect(helper.pairs.map(\.platform) == [.ios, .watchos])
        #expect(helper.pairs.last?.deviceId == "w_abc")
        #expect(helper.pairs.last?.apnsToken == String(repeating: "ef", count: 32))
        #expect(helper.pairs.last?.relayUrl == helper.pairs.first?.relayUrl)

        await online(coordinator)
        #expect(helper.pairs.count == 2)
    }

    @Test func removeMachineUnpairsAndForgets() async throws {
        let (coordinator, _) = await coordinator()
        await online(coordinator)
        let helper = self.helper

        await coordinator.machineRemoved(hostID: machine, connection: Data("studio".utf8)) { id in helper.unpaired.append(id) }
        #expect(helper.unpaired == [try keys.deviceID()])
        #expect(store.all().isEmpty)
        #expect(unpairs.all().isEmpty)
        #expect(coordinator.pairedMachines.isEmpty)
    }

    @Test func offlineRemovalQueuesUntilRetrySucceeds() async throws {
        let (coordinator, _) = await coordinator()
        await online(coordinator)

        await coordinator.machineRemoved(hostID: machine, connection: Data("studio".utf8), unpair: nil)
        #expect(store.all().isEmpty, "pushes from the removed machine stop opening at once")
        let queued = try #require(unpairs.all().first)
        #expect(queued.id == "h_studio")
        #expect(queued.deviceIds == [try keys.deviceID()])
        #expect(queued.connection == Data("studio".utf8))

        var attempts: [String] = []
        await coordinator.retryUnpairs { _, id in
            attempts.append(id)
            throw HelperError.closed
        }
        #expect(unpairs.all().count == 1)
        await coordinator.retryUnpairs { entry, id in attempts.append("\(entry.id) \(id)") }
        #expect(attempts == [try keys.deviceID(), "h_studio \(try keys.deviceID())"])
        #expect(unpairs.all().isEmpty)
    }

    @Test func failedOnlineUnpairQueues() async {
        let (coordinator, _) = await coordinator()
        await online(coordinator)
        await coordinator.machineRemoved(hostID: machine, connection: nil) { _ in throw HelperError.closed }
        #expect(store.all().isEmpty)
        #expect(unpairs.all().map(\.id) == ["h_studio"])
    }

    @Test func unknownDeviceCountsAsUnpaired() async {
        let (coordinator, _) = await coordinator()
        await online(coordinator)
        await coordinator.machineRemoved(hostID: machine, connection: nil) { _ in
            throw HelperError.rpc(RPCErrorObject(code: RPCErrorCode.notFound, message: "not found"))
        }
        #expect(unpairs.all().isEmpty)
    }

    @Test func pairingAgainCancelsAPendingUnpair() async {
        let (coordinator, _) = await coordinator()
        await online(coordinator)
        await coordinator.machineRemoved(hostID: machine, connection: nil, unpair: nil)
        await online(coordinator)
        #expect(unpairs.all().isEmpty)
        var attempts = 0
        await coordinator.retryUnpairs { _, _ in attempts += 1 }
        #expect(attempts == 0)
    }

    @Test func backgroundTokenChangeUsesRegisterSender() async {
        let (coordinator, registrar) = await coordinator()
        await online(coordinator)
        coordinator.isForeground = false

        registrar.didRegister(token: Self.tokenB)
        await coordinator.tokensChanged()

        let sent = register.sent.withLock { $0 }
        #expect(sent.map(\.hostId) == ["h_studio"])
        #expect(sent.first?.apns == String(repeating: "cd", count: 32))
        #expect(helper.pairs.count == 1)
    }

    /// An ended activity is reported to its machine through the mailbox at once, in front or
    /// not; in front the machines are also paired again over SSH.
    @Test func endedLiveActivityReportedToItsMachine() async {
        let (coordinator, _) = await coordinator()
        await online(coordinator)
        var asked = 0
        coordinator.onRepairNeeded = { asked += 1 }
        let token = String(repeating: "ef", count: 32)
        await coordinator.liveActivityEnded(hostId: "h_studio", token: token)
        await coordinator.liveActivityEnded(hostId: "h_unknown", token: token)

        let ended = register.ended.withLock { $0 }
        #expect(ended.map(\.hostId) == ["h_studio"])
        #expect(ended.first?.token == token)
        #expect(asked == 1)
    }

    /// "Done silent" reaches only a helper that takes it, and turning it on pairs again.
    @Test func doneSilentSentOnlyToHelpersThatTakeIt() async {
        let inputs = Inputs()
        inputs.settings.doneSilent = true
        let (coordinator, _) = await coordinator(inputs)
        await online(coordinator)
        #expect(helper.pairs.last?.doneSilent == false)

        await online(coordinator, capabilities: ["push.v1", "donesilent.v1"])
        #expect(helper.pairs.count == 2)
        #expect(helper.pairs.last?.doneSilent == true)

        await online(coordinator, capabilities: ["push.v1", "donesilent.v1"])
        #expect(helper.pairs.count == 2, "nothing changed, so no pairing again")
    }

    @Test func foregroundTokenChangeAsksForMachinesAgain() async {
        let (coordinator, _) = await coordinator()
        var asked = 0
        coordinator.onRepairNeeded = { asked += 1 }
        await coordinator.tokensChanged()
        #expect(asked == 1)
        #expect(register.sent.withLock { $0 }.isEmpty)
    }

    @Test func customRelayMustBeHTTPS() {
        #expect(PushEnvironment.customRelay("https://relay.example.com") == URL(string: "https://relay.example.com"))
        #expect(PushEnvironment.customRelay("  https://relay.example.com/togo ") == URL(string: "https://relay.example.com/togo"))
        #expect(PushEnvironment.customRelay("http://relay.example.com") == nil)
        #expect(PushEnvironment.customRelay("relay.example.com") == nil)
        #expect(PushEnvironment.customRelay("https://") == nil)
        #expect(PushEnvironment.customRelay("https://user:pw@relay.example.com") == nil)
        #expect(PushEnvironment.customRelay("https://relay.example.com/?a=1") == nil)

        var settings = AppSettings()
        #expect(PushEnvironment.relay(settings, hosted: Self.relay) == Self.relay)
        settings.relayURL = URL(string: "http://relay.example.com")
        #expect(PushEnvironment.relay(settings, hosted: Self.relay) == Self.relay)
        settings.relayURL = URL(string: "https://relay.example.com")
        #expect(PushEnvironment.relay(settings, hosted: Self.relay) == URL(string: "https://relay.example.com"))
        #expect(PushEnvironment.relay(settings, hosted: nil) == URL(string: "https://relay.example.com"))
    }

    /// The hosted relay comes from the build's configuration, never from the source.
    @Test func hostedRelayFromBuildSetting() {
        #expect(PushEnvironment.hostedRelay("relay.example.com") == URL(string: "https://relay.example.com"))
        #expect(PushEnvironment.hostedRelay("") == nil)
        #expect(PushEnvironment.hostedRelay(nil) == nil)
        #expect(PushEnvironment.hostedRelay("$(TOGO_RELAY_HOST)") == nil)
        #expect(PushEnvironment.relay(AppSettings(), hosted: nil) == nil)
    }

    @Test func noRelayNoPairing() async {
        let registrar = PushRegistrar(permissions: FakePermissions(.authorized), registerForRemote: {})
        await registrar.refresh()
        registrar.didRegister(token: Self.tokenA)
        let coordinator = PairingCoordinator(
            keys: keys, store: store, unpairs: unpairs, registrar: registrar, settings: { AppSettings() },
            liveTokens: { _ in (update: nil, start: nil) }, watch: { [] }, register: register, hostedRelay: nil,
            deviceName: "iPhone"
        )
        await online(coordinator)
        #expect(helper.pairs.isEmpty)
    }

    @Test func settingsDefaultsKeepCommandsOffAndUnlockOn() throws {
        let decoded = try JSONDecoder().decode(AppSettings.self, from: Data(#"{"themeID":"plain"}"#.utf8))
        #expect(decoded.relayURL == nil)
        #expect(decoded.liveActivityCommandMachines.isEmpty)
        #expect(!decoded.approveWithoutUnlock)
        #expect(decoded.liveActivities)
    }

    // MARK: Removed while pairing

    @Test func removedWhilePairingUnpairsAtOnce() async throws {
        let (coordinator, _) = await coordinator()
        let helper = self.helper
        var retired: [String] = []
        coordinator.onRetired = { retired.append($0) }
        await coordinator.machineOnline(hostID: machine, name: "Studio", capabilities: ["push.v1"]) { params in
            #expect(coordinator.pairedOrPairing.contains(machine), "the saved password stays for the unpair")
            await coordinator.machineRemoved(hostID: machine, connection: Data("studio".utf8)) { id in
                helper.unpaired.append(id)
            }
            return try await helper.pair(params)
        }
        #expect(store.all().isEmpty)
        #expect(coordinator.pairedMachines.isEmpty)
        #expect(retired == ["h_studio"])
        #expect(helper.unpaired == [try keys.deviceID()])
        #expect(unpairs.all().isEmpty)
    }

    @Test func removedWhilePairingOfflineQueuesUnpair() async throws {
        let (coordinator, _) = await coordinator()
        let helper = self.helper
        await coordinator.machineOnline(hostID: machine, name: "Studio", capabilities: ["push.v1"]) { params in
            await coordinator.machineRemoved(hostID: machine, connection: Data("studio".utf8), unpair: nil)
            return try await helper.pair(params)
        }
        #expect(store.all().isEmpty, "pushes from the removed machine never open")
        let queued = try #require(unpairs.all().first)
        #expect(queued.id == "h_studio")
        #expect(queued.connection == Data("studio".utf8))
        #expect(queued.deviceIds == [try keys.deviceID()])
        #expect(!coordinator.pairedOrPairing.contains(machine))
    }

    // MARK: Command text through the mailbox

    @Test func commandChoiceGoesThroughMailbox() async throws {
        let inputs = Inputs()
        let (coordinator, _) = await coordinator(inputs)
        await online(coordinator)
        await coordinator.commandChoiceChanged()
        #expect(register.sent.withLock { $0 }.isEmpty, "nothing changed")

        inputs.settings.liveActivityCommandMachines = [machine]
        await coordinator.commandChoiceChanged()
        #expect(register.commands.withLock { $0 } == ["h_studio": true])
        // The machine has not confirmed yet, so the page shows the change as pending.
        #expect(coordinator.confirmedCommands.isEmpty)

        await online(coordinator)
        #expect(coordinator.confirmedCommands == [machine])

        inputs.settings.liveActivityCommandMachines = []
        coordinator.isForeground = true
        await coordinator.commandChoiceChanged()
        #expect(register.commands.withLock { $0 } == ["h_studio": false], "sent in the foreground too")
        #expect(coordinator.confirmedCommands == [machine])
    }

    /// With the machine out of SSH reach, on and then off must both go out: the helper may
    /// already hold the "on", whatever it last confirmed.
    @Test func onThenOffWithoutSSHSendsBoth() async throws {
        let inputs = Inputs()
        let (first, _) = await coordinator(inputs)
        await online(first)

        inputs.settings.liveActivityCommandMachines = [machine]
        await first.commandChoiceChanged()
        inputs.settings.liveActivityCommandMachines = []
        await first.commandChoiceChanged()

        #expect(register.commandLog.withLock { $0 } == [true, false])
        #expect(first.confirmedCommands.isEmpty)
        #expect(first.pendingCommands == [machine])
        let shown = NotificationValues.machines(
            [MachineSection(
                id: machine, name: "Studio", symbol: .mini, reachability: .offline(since: Samples.now, reason: "away"),
                helper: .present(version: "1", live: true), sessions: []
            )],
            paired: [machine], commandsOn: first.confirmedCommands, pending: first.pendingCommands
        )
        #expect(NotificationMachinesSection.commandNote(shown[0], showing: false) == "Turns off when Studio receives it")

        // The last choice sent is kept across launches, and the next SSH pairing confirms it.
        let (relaunched, _) = await coordinator(inputs)
        await relaunched.commandChoiceChanged()
        #expect(register.commandLog.withLock { $0 } == [true, false], "nothing new to send")
        #expect(relaunched.pendingCommands == [machine])
        await online(relaunched)
        #expect(helper.pairs.last?.liveActivityCommand == false)
        #expect(relaunched.pendingCommands.isEmpty)
    }
}
