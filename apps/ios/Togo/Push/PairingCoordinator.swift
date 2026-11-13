import CryptoKit
import Foundation
import TogoKit
import TogoPush
import UIKit

/// Sends new push tokens and the command text choice to a paired machine through its relay mailbox.
protocol PushTokenRegistering: Sendable {
    /// `liveActivityEnded` is the update token of an activity that ended or was dismissed.
    func send(
        _ tokens: (apns: String?, liveActivity: String?, liveActivityStart: String?), liveActivityCommand: Bool,
        liveActivityEnded: String?, pairing: Pairing
    ) async throws
}

extension PushTokenRegistering {
    func send(
        _ tokens: (apns: String?, liveActivity: String?, liveActivityStart: String?), liveActivityCommand: Bool,
        pairing: Pairing
    ) async throws {
        try await send(tokens, liveActivityCommand: liveActivityCommand, liveActivityEnded: nil, pairing: pairing)
    }
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
    /// This device's `authorized_keys` line on a machine, or nil when it signs in with a password.
    var deviceKey: @MainActor (HostRecord) -> DeviceKeyLine? = { _ in nil }

    private let keys: any DeviceKeyStore
    private let store: PairingStore
    private let unpairs: PendingUnpairStore
    /// Removed machines that may still hold this device's key.
    let keyRemovals: PendingKeyRemovalStore
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
    private let foreground: (@MainActor () -> Bool)?
    private let deviceName: String
    private let clock: @Sendable () -> Date
    private var pairing: Set<HostID> = []
    /// Machines removed while their pairing was in flight: how to reach them to unpair, so the
    /// pairing that lands afterwards goes straight to the pending unpair list.
    private var removedWhilePairing: [HostID: Removal] = [:]
    private var retrying = false
    /// Removals and their retries, one after another, so a retry never races the removal it follows.
    private var work: Task<Void, Never>?

    private struct Removal {
        var connection: Data?
        var unpair: ((String) async throws -> Void)?
        var removeKey: ((String) async throws -> Int)?
    }

    init(
        keys: any DeviceKeyStore, store: PairingStore, unpairs: PendingUnpairStore, registrar: PushRegistrar,
        settings: @escaping @MainActor () -> AppSettings,
        liveTokens: @escaping @MainActor (String?) -> (update: String?, start: String?),
        watch: @escaping @MainActor () -> [WatchDevice],
        keyRemovals: PendingKeyRemovalStore = PendingKeyRemovalStore(file: nil),
        register: (any PushTokenRegistering)? = nil,
        hostedRelay: URL? = PushEnvironment.defaultRelay,
        sentChoices: UserDefaults = .standard,
        deviceName: String = UIDevice.current.name,
        clock: @escaping @Sendable () -> Date = { Date() },
        foreground: (@MainActor () -> Bool)? = nil
    ) {
        self.keys = keys
        self.store = store
        self.unpairs = unpairs
        self.keyRemovals = keyRemovals
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
        self.foreground = foreground
    }

    /// Machines with a stored pairing, by the app's id.
    var pairedMachines: Set<HostID> {
        Set(store.all().compactMap(\.appHostID))
    }

    /// The id of the helper the machine is paired with, for telling its records apart.
    func helperHostID(for id: HostID) -> String? {
        store.all().first { $0.appHostID == id }?.hostId
    }

    /// `dropped` was a second record of `kept`'s machine. Its pairing moves to `kept` when that
    /// has none; the helper is never told to forget this device, which would end both.
    func machineMerged(_ dropped: HostID, into kept: HostID) {
        let all = store.all()
        guard var moved = all.first(where: { $0.appHostID == dropped }),
              !all.contains(where: { $0.appHostID == kept }) else { return }
        moved.appHostID = kept
        store.put(moved)
        onChange()
        onPairingsChanged(store.all())
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

    /// A machine's helper connected: pairs it once notifications are allowed. It never asks for
    /// permission itself; the question follows a tap, after adding a machine or in Settings.
    /// `sshKey` is this device's public key on the machine, which only a helper that can
    /// remove it is given. `reconnected` marks the first report after the machine was out of
    /// reach: a running Live Activity's token then goes again even when nothing changed here,
    /// since a helper that restarted may have lost it.
    func machineOnline(
        hostID: HostID, name: String, capabilities: Set<String>, sshKey: String? = nil, reconnected: Bool = false,
        pair: (PushPairParams) async throws -> PushPairResult
    ) async {
        guard capabilities.contains(Capability.push), !pairing.contains(hostID) else { return }
        pairing.insert(hostID)
        defer {
            pairing.remove(hostID)
            removedWhilePairing[hostID] = nil
        }
        guard registrar.authorization.allowsPush, let apnsToken = registrar.apnsToken else { return }

        let settings = settings()
        guard let relay = PushEnvironment.relay(settings, hosted: hostedRelay) else { return }
        let command = settings.liveActivityCommand(for: hostID)
        let stored = store.all().first { $0.appHostID == hostID }
        let live = settings.liveActivities ? liveTokens(stored?.hostId) : (update: nil, start: nil)
        let watches = watch()
        let sshKey = capabilities.contains(Capability.authkeys) ? sshKey : nil
        let doneSilent = capabilities.contains(Capability.doneSilent) && settings.doneSilent
        let hash = Self.tokenHash(apns: apnsToken, live: live, watches: watches, sshKey: sshKey, doneSilent: doneSilent)
        // A choice sent through the mailbox is confirmed over SSH, even when it matches the old one.
        let reportsLive = reconnected && live.update != nil
        if let stored, stored.tokenHash == hash, stored.relayURL == relay, stored.liveActivityCommand == command,
           sentChoice[stored.hostId] == nil, !reportsLive {
            return
        }

        guard let phone = try? params(
            apnsToken: apnsToken, live: live, relay: relay, command: command, sshKey: sshKey, doneSilent: doneSilent
        ),
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
            pairedAt: clock(), tokenHash: Self.tokenHash(
                apns: apnsToken, live: live, watches: pairedWatches, sshKey: sshKey, doneSilent: doneSilent
            ),
            liveActivityCommand: command
        )
        if let removal = removedWhilePairing.removeValue(forKey: hostID) {
            // Removed while the helper was pairing: it holds this device now, so tell it to forget.
            let deviceIDs = [try? keys.deviceID()].compactMap(\.self) + pairedWatches.map(\.deviceId)
            let entry = PendingUnpair(pairing: fresh, deviceIds: deviceIDs, connection: removal.connection)
            unpairs.put(entry)
            onRetired(entry.id)
            onChange()
            if let unpair = removal.unpair {
                await attempt(entry, unpair, removeKey: removal.removeKey.map(Self.byKey))
            }
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
    /// `key` is this device's key on the machine: it goes once the helper forgot this device,
    /// over the same channel, and waits for a retry while the unpair does.
    func machineRemoved(
        hostID: HostID, connection: Data?, key: KeyRemovalStep? = nil, unpair: ((String) async throws -> Void)?
    ) async {
        if let key { recordKey(key, hostID: hostID, connection: connection) }
        if pairing.contains(hostID) {
            removedWhilePairing[hostID] = Removal(connection: connection, unpair: unpair, removeKey: key?.removeKey)
        }
        let stored = store.all().filter { $0.appHostID == hostID }
        guard !stored.isEmpty else {
            // Nothing to unpair: the key can go now, unless a pairing is still landing.
            if let removeKey = key?.removeKey, !pairing.contains(hostID) { await attemptKey(hostID, Self.byKey(removeKey)) }
            return
        }
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
        for entry in entries { await attempt(entry, unpair, removeKey: key?.removeKey.map(Self.byKey)) }
    }

    /// Runs `job` after every removal and retry queued before it.
    func enqueue(_ job: @escaping @MainActor () async -> Void) {
        let previous = work
        work = Task { @MainActor in
            await previous?.value
            await job()
        }
    }

    /// Returns once every queued removal and retry has finished.
    func settled() async {
        while let current = work {
            await current.value
            if work == current { return }
        }
    }

    private static func byKey(_ removeKey: @escaping (String) async throws -> Int) -> (PendingKeyRemoval) async throws -> Int {
        { try await removeKey($0.key.publicKey) }
    }

    /// Asks each removed machine's helper again to forget this device. Runs when the app comes
    /// forward and when the network returns; never asks the user anything. Each machine's key
    /// goes after its unpair; keys of machines in Togo again under `current` stay.
    func retryUnpairs(
        current: [HostRecord] = [], removeKey: ((PendingKeyRemoval) async throws -> Int)? = nil,
        _ unpair: (PendingUnpair, String) async throws -> Void
    ) async {
        guard !retrying else { return }
        retrying = true
        defer { retrying = false }
        dropReturnedKeys(current: current)
        for entry in unpairs.all() {
            if store.pairing(hostId: entry.pairing.hostId) != nil {
                unpairs.remove(hostId: entry.id)
                continue
            }
            await attempt(entry, { try await unpair(entry, $0) }, removeKey: removeKey)
        }
        if let removeKey { await retryLoneKeys(removeKey) }
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

    /// Unpairs every device id, then, once none is left, takes this device's key off the
    /// machine before the local forget.
    private func attempt(
        _ entry: PendingUnpair, _ unpair: (String) async throws -> Void,
        removeKey: ((PendingKeyRemoval) async throws -> Int)? = nil
    ) async {
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
            if let removeKey, let host = entry.pairing.appHostID { await attemptKey(host, removeKey) }
            onUnpaired(entry)
            onChange()
        } else {
            var left = entry
            left.deviceIds = remaining
            unpairs.put(left)
        }
    }

    private var inForeground: Bool { foreground?() ?? isForeground }

    /// The APNs or Live Activity tokens changed.
    func tokensChanged() async {
        guard !inForeground else {
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

    /// The machine's Live Activity ended or the user dismissed it. Its helper hears at once
    /// through the relay mailbox, so the next event comes as a notification instead of an
    /// update to an activity nobody sees; in front, the next pairing over SSH confirms it.
    func liveActivityEnded(hostId: String, token: String) async {
        guard let pairing = store.pairing(hostId: hostId) else { return }
        let settings = settings()
        let live = settings.liveActivities ? liveTokens(hostId) : (update: nil, start: nil)
        let tokens = (apns: registrar.apnsToken, liveActivity: live.update, liveActivityStart: live.start)
        let command = pairing.appHostID.map(settings.liveActivityCommand(for:)) ?? pairing.liveActivityCommand
        try? await register.send(tokens, liveActivityCommand: command, liveActivityEnded: token, pairing: pairing)
        if inForeground { onRepairNeeded() }
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
        apnsToken: String, live: (update: String?, start: String?), relay: URL, command: Bool, sshKey: String?,
        doneSilent: Bool = false
    ) throws -> PushPairParams {
        PushPairParams(
            deviceId: try keys.deviceID(), name: deviceName,
            x25519: Base64URL.encode(try keys.agreementKey().publicKey.rawRepresentation),
            approvalKey: Base64URL.encode(try keys.signingKey().publicKey.rawRepresentation),
            apnsToken: apnsToken, env: PushEnvironment.apns, relayUrl: relay.absoluteString,
            liveActivityToken: live.update, platform: .ios, liveActivityStartToken: live.start,
            liveActivityCommand: command, sshKey: sshKey, doneSilent: doneSilent
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
    /// whether it is out of date without keeping the tokens. The SSH key counts only once it
    /// is sent, so pairings from before it existed stay current until the helper can take it.
    /// "Done silent" counts only while on, for the same reason.
    static func tokenHash(
        apns: String, live: (update: String?, start: String?), watches: [WatchDevice], sshKey: String? = nil,
        doneSilent: Bool = false
    ) -> String {
        var parts = [apns, live.update ?? "", live.start ?? ""]
        parts += watches.map { "\($0.deviceId):\($0.apnsToken)" }
        if let sshKey { parts.append("ssh:" + sshKey) }
        if doneSilent { parts.append("done-silent") }
        let digest = SHA256.hash(data: Data(parts.joined(separator: "|").utf8))
        return digest.prefix(3).map { String(format: "%02x", $0) }.joined()
    }
}
