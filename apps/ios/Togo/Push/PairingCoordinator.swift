import CryptoKit
import Foundation
import TogoKit
import TogoPush
import UIKit

/// Sends new push tokens and the command text choice to a paired machine through its relay mailbox.
protocol PushTokenRegistering: Sendable {
    func send(
        _ tokens: (apns: String?, liveActivity: String?, liveActivityStart: String?), liveActivityCommand: Bool,
        pairing: Pairing
    ) async throws
}

extension RelayRegisterSender: PushTokenRegistering {}

/// Pairs this iPhone, and any Watch paired with it, with every machine whose helper sends
/// pushes. A machine is paired again only when something it holds is out of date: the push
/// tokens, the relay or the command text choice. A pairing is stored only once the helper has
/// accepted it.
@MainActor final class PairingCoordinator {
    /// The app is in the foreground. Token changes then wait for each machine's next report and
    /// go over SSH; in the background they go through the relay.
    var isForeground = true
    /// Asks the app to report every online machine again, so changed tokens or settings reach them.
    var onRepairNeeded: @MainActor () -> Void = {}
    /// The set of paired machines, or of machines waiting to unpair, changed.
    var onChange: @MainActor () -> Void = {}
    /// A removed machine's helper confirmed it forgot this device, or the user gave up on it.
    /// Whatever was kept to reach it, such as a saved password, can go now.
    var onUnpaired: @MainActor (PendingUnpair) -> Void = { _ in }
    /// The stored pairings changed. The Watch gets the list, since it answers with them.
    var onPairingsChanged: @MainActor ([Pairing]) -> Void = { _ in }
    /// A machine's pairing was retired because the machine was removed, by its helper's host id.
    var onRetired: @MainActor (String) -> Void = { _ in }

    private let keys: any DeviceKeyStore
    private let store: PairingStore
    private let unpairs: PendingUnpairStore
    private let registrar: PushRegistrar
    private let settings: @MainActor () -> AppSettings
    /// The Live Activity tokens for a machine, by its helper's host id (nil before the first
    /// pairing): the machine's own activity token and the shared start token.
    private let liveTokens: @MainActor (String?) -> (update: String?, start: String?)
    private let watch: @MainActor () -> [WatchDevice]
    private let register: any PushTokenRegistering
    /// The hosted relay this build was configured with; nil when it has none.
    private let hostedRelay: URL?
    /// The command text choice last sent to each machine through its mailbox and not yet
    /// confirmed over SSH, by helper host id. Kept across launches.
    private let sentChoices: UserDefaults
    private static let sentChoicesKey = "push.commandChoiceSent"
    /// Mailbox sends run one after another, so a later choice is never stamped before an earlier one.
    private var commandSend: Task<Void, Never>?
    private let deviceName: String
    private let clock: @Sendable () -> Date
    private var pairing: Set<HostID> = []
    /// Machines removed while their pairing was in flight: how to reach them to unpair, so the
    /// pairing that lands afterwards goes straight to the pending unpair list.
    private var removedWhilePairing: [HostID: Removal] = [:]
    private var retrying = false

    private struct Removal {
        var connection: Data?
        var unpair: ((String) async throws -> Void)?
    }

    init(
        keys: any DeviceKeyStore, store: PairingStore, unpairs: PendingUnpairStore, registrar: PushRegistrar,
        settings: @escaping @MainActor () -> AppSettings,
        liveTokens: @escaping @MainActor (String?) -> (update: String?, start: String?),
        watch: @escaping @MainActor () -> [WatchDevice],
        register: (any PushTokenRegistering)? = nil,
        hostedRelay: URL? = PushEnvironment.defaultRelay,
        sentChoices: UserDefaults = .standard,
        deviceName: String = UIDevice.current.name,
        clock: @escaping @Sendable () -> Date = { Date() }
    ) {
        self.keys = keys
        self.store = store
        self.unpairs = unpairs
        self.registrar = registrar
        self.settings = settings
        self.liveTokens = liveTokens
        self.watch = watch
        self.register = register ?? RelayRegisterSender(keys: keys, clock: clock)
        self.hostedRelay = hostedRelay
        self.sentChoices = sentChoices
        // The helper counts the name in Unicode scalars and takes at most 64.
        self.deviceName = String(String.UnicodeScalarView(deviceName.unicodeScalars.prefix(64)))
        self.clock = clock
    }

    /// Machines with a stored pairing, by the app's id.
    var pairedMachines: Set<HostID> {
        Set(store.all().compactMap(\.appHostID))
    }

    /// Machines paired or being paired now. A removed one among them needs its helper told to
    /// forget this device, so what reaches it, such as a saved password, has to stay.
    var pairedOrPairing: Set<HostID> {
        pairedMachines.union(pairing)
    }

    /// Machines whose helper confirmed, at its last pairing, that it sends command text.
    var confirmedCommands: Set<HostID> {
        Set(store.all().filter(\.liveActivityCommand).compactMap(\.appHostID))
    }

    /// Machines sent a command text choice through the mailbox that SSH has not confirmed yet.
    var pendingCommands: Set<HostID> {
        let sent = sentChoice
        return Set(store.all().filter { sent[$0.hostId] != nil }.compactMap(\.appHostID))
    }

    private var sentChoice: [String: Bool] {
        get { sentChoices.dictionary(forKey: Self.sentChoicesKey) as? [String: Bool] ?? [:] }
        set { sentChoices.set(newValue, forKey: Self.sentChoicesKey) }
    }

    /// A machine's helper connected. The first helper that can send pushes while the user is
    /// looking at the app is the moment it asks for notification permission; a machine that
    /// connects in the background never brings up the question.
    func machineOnline(
        hostID: HostID, name: String, capabilities: Set<String>,
        pair: (PushPairParams) async throws -> PushPairResult
    ) async {
        guard capabilities.contains(Capability.push), !pairing.contains(hostID) else { return }
        pairing.insert(hostID)
        defer {
            pairing.remove(hostID)
            removedWhilePairing[hostID] = nil
        }
        if registrar.authorization == .notDetermined, isForeground {
            await registrar.requestAuthorization()
        }
        guard registrar.authorization.allowsPush, let apnsToken = registrar.apnsToken else { return }

        let settings = settings()
        guard let relay = PushEnvironment.relay(settings, hosted: hostedRelay) else { return }
        let command = settings.liveActivityCommand(for: hostID)
        let stored = store.all().first { $0.appHostID == hostID }
        let live = settings.liveActivities ? liveTokens(stored?.hostId) : (update: nil, start: nil)
        let watches = watch()
        let hash = Self.tokenHash(apns: apnsToken, live: live, watches: watches)
        // A choice sent through the mailbox is confirmed over SSH, even when it matches the old one.
        if let stored, stored.tokenHash == hash, stored.relayURL == relay, stored.liveActivityCommand == command,
           sentChoice[stored.hostId] == nil {
            return
        }

        guard let phone = try? params(apnsToken: apnsToken, live: live, relay: relay, command: command),
              let result = try? await pair(phone)
        else { return }
        var pairedWatches: [WatchDevice] = []
        for device in watches {
            if (try? await pair(Self.params(device, relay: relay, command: command))) != nil {
                pairedWatches.append(device)
            }
        }
        if let stored, stored.hostId != result.hostId {
            store.remove(hostId: stored.hostId)
        }
        let fresh = Pairing(
            hostId: result.hostId, hostKey: result.hostKey, relayURL: relay, machineName: name, appHostID: hostID,
            pairedAt: clock(), tokenHash: Self.tokenHash(apns: apnsToken, live: live, watches: pairedWatches),
            liveActivityCommand: command
        )
        if let removal = removedWhilePairing.removeValue(forKey: hostID) {
            // Removed while the helper was pairing: it holds this device now, so tell it to forget.
            let deviceIDs = [try? keys.deviceID()].compactMap(\.self) + pairedWatches.map(\.deviceId)
            let entry = PendingUnpair(pairing: fresh, deviceIds: deviceIDs, connection: removal.connection)
            unpairs.put(entry)
            onRetired(entry.id)
            onChange()
            if let unpair = removal.unpair { await attempt(entry, unpair) }
            return
        }
        store.put(fresh)
        if let stored { sentChoice[stored.hostId] = nil }
        sentChoice[result.hostId] = nil
        // The machine was removed and added again; its old unpair must not undo this pairing.
        unpairs.remove(hostId: result.hostId)
        onChange()
        onPairingsChanged(store.all())
    }

    /// The machine was removed from Togo. Its pairing moves to the pending unpair list before
    /// anything else, so its pushes stop opening at once. `unpair` is nil when the machine is
    /// not reachable now; the helper keeps pushing until it is told, so `retryUnpairs` asks
    /// again until it confirms. `connection` is the encoded host record the retry connects with.
    func machineRemoved(hostID: HostID, connection: Data?, unpair: ((String) async throws -> Void)?) async {
        if pairing.contains(hostID) {
            removedWhilePairing[hostID] = Removal(connection: connection, unpair: unpair)
        }
        let stored = store.all().filter { $0.appHostID == hostID }
        guard !stored.isEmpty else { return }
        var deviceIDs = watch().map(\.deviceId)
        if let phone = try? keys.deviceID() { deviceIDs.insert(phone, at: 0) }
        let entries = stored.map { PendingUnpair(pairing: $0, deviceIds: deviceIDs, connection: connection) }
        for entry in entries {
            unpairs.retire(entry, from: store)
            onRetired(entry.id)
        }
        onChange()
        onPairingsChanged(store.all())
        guard let unpair else { return }
        for entry in entries { await attempt(entry, unpair) }
    }

    /// Asks each removed machine's helper again to forget this device. Runs when the app comes
    /// forward and when the network returns; never asks the user anything.
    func retryUnpairs(_ unpair: (PendingUnpair, String) async throws -> Void) async {
        guard !retrying else { return }
        retrying = true
        defer { retrying = false }
        for entry in unpairs.all() {
            if store.pairing(hostId: entry.pairing.hostId) != nil {
                unpairs.remove(hostId: entry.id)
                continue
            }
            await attempt(entry) { try await unpair(entry, $0) }
        }
    }

    /// Machines still waiting for their helper to forget this device.
    var pendingUnpairs: [PendingUnpair] { unpairs.all() }

    /// Stops trying to unpair. The helper may keep pushing until `togo devices revoke` runs on it.
    func forgetUnpair(hostId: String) {
        guard let entry = unpairs.all().first(where: { $0.id == hostId }) else { return }
        unpairs.remove(hostId: hostId)
        onUnpaired(entry)
        onChange()
    }

    private func attempt(_ entry: PendingUnpair, _ unpair: (String) async throws -> Void) async {
        var remaining: [String] = []
        for deviceID in entry.deviceIds {
            do {
                try await unpair(deviceID)
            } catch HelperError.rpc(let error) where error.code == RPCErrorCode.notFound {
                // The helper does not know the device: nothing left to forget.
            } catch {
                remaining.append(deviceID)
            }
        }
        if remaining.isEmpty {
            unpairs.remove(hostId: entry.id)
            onUnpaired(entry)
            onChange()
        } else {
            var left = entry
            left.deviceIds = remaining
            unpairs.put(left)
        }
    }

    /// The APNs or Live Activity tokens changed.
    func tokensChanged() async {
        guard !isForeground else {
            onRepairNeeded()
            return
        }
        let settings = settings()
        // The stored hash stays as it was, so the next connection over SSH pairs again and
        // confirms what the mailbox carried.
        for pairing in store.all() {
            let live = settings.liveActivities ? liveTokens(pairing.hostId) : (update: nil, start: nil)
            let tokens = (apns: registrar.apnsToken, liveActivity: live.update, liveActivityStart: live.start)
            guard tokens.apns != nil || tokens.liveActivity != nil || tokens.liveActivityStart != nil else { continue }
            let command = pairing.appHostID.map(settings.liveActivityCommand(for:)) ?? pairing.liveActivityCommand
            try? await register.send(tokens, liveActivityCommand: command, pairing: pairing)
        }
    }

    /// The command text choice changed. Each paired machine hears a choice that differs from
    /// the last one it was sent (or, with none sent since SSH, the one it confirmed) through
    /// its relay mailbox at once, so turning it off does not wait for an SSH connection. The
    /// machine counts as pending until the next pairing over SSH confirms what it holds.
    func commandChoiceChanged() async {
        let previous = commandSend
        let send = Task { @MainActor [weak self] in
            await previous?.value
            await self?.sendCommandChoices()
        }
        commandSend = send
        await send.value
    }

    private func sendCommandChoices() async {
        let settings = settings()
        var changed = false
        for pairing in store.all() {
            guard let id = pairing.appHostID else { continue }
            let command = settings.liveActivityCommand(for: id)
            guard command != (sentChoice[pairing.hostId] ?? pairing.liveActivityCommand) else { continue }
            let live = settings.liveActivities ? liveTokens(pairing.hostId) : (update: nil, start: nil)
            let tokens = (apns: registrar.apnsToken, liveActivity: live.update, liveActivityStart: live.start)
            // A failed send keeps the old record, so the next change tries again; the next SSH
            // pairing carries the choice either way.
            guard (try? await register.send(tokens, liveActivityCommand: command, pairing: pairing)) != nil else { continue }
            sentChoice[pairing.hostId] = command
            changed = true
        }
        if changed { onChange() }
    }

    // MARK: Building requests

    private func params(
        apnsToken: String, live: (update: String?, start: String?), relay: URL, command: Bool
    ) throws -> PushPairParams {
        PushPairParams(
            deviceId: try keys.deviceID(), name: deviceName,
            x25519: Base64URL.encode(try keys.agreementKey().publicKey.rawRepresentation),
            approvalKey: Base64URL.encode(try keys.signingKey().publicKey.rawRepresentation),
            apnsToken: apnsToken, env: PushEnvironment.apns, relayUrl: relay.absoluteString,
            liveActivityToken: live.update, platform: .ios, liveActivityStartToken: live.start,
            liveActivityCommand: command
        )
    }

    private static func params(_ device: WatchDevice, relay: URL, command: Bool) -> PushPairParams {
        PushPairParams(
            deviceId: device.deviceId, name: String(String.UnicodeScalarView(device.name.unicodeScalars.prefix(64))),
            x25519: device.x25519, approvalKey: device.approvalKey, apnsToken: device.apnsToken,
            env: PushEnvironment.apns, relayUrl: relay.absoluteString, platform: .watchos,
            liveActivityCommand: command
        )
    }

    /// Six hex characters of SHA-256 over every token the machine was given, enough to tell
    /// whether it is out of date without keeping the tokens.
    static func tokenHash(apns: String, live: (update: String?, start: String?), watches: [WatchDevice]) -> String {
        var parts = [apns, live.update ?? "", live.start ?? ""]
        parts += watches.map { "\($0.deviceId):\($0.apnsToken)" }
        let digest = SHA256.hash(data: Data(parts.joined(separator: "|").utf8))
        return digest.prefix(3).map { String(format: "%02x", $0) }.joined()
    }
}
