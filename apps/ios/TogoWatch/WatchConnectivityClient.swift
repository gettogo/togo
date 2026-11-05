import CryptoKit
import Foundation
import TogoPush
import WatchConnectivity
import WatchKit

/// The Watch's side of the phone link. It tells the phone the Watch's push identity, so the
/// phone can pair it with every machine, and takes back the pairings and the session context.
@MainActor final class WatchConnectivityClient: NSObject, WCSessionDelegate {
    private let store: WatchStore
    private let pairings: PairingStore
    private let keys: any DeviceKeyStore
    private let register: RelayRegisterSender
    private let defaults: UserDefaults
    private var session: WCSession?
    /// The identity to send once the session is active.
    private var unsent: WatchDevice?

    /// A short hash of the last device id and token sent, never the token itself.
    private static let sentKey = "watch.sentDevice"

    init(store: WatchStore, pairings: PairingStore, keys: any DeviceKeyStore, defaults: UserDefaults = .standard) {
        self.store = store
        self.pairings = pairings
        self.keys = keys
        register = RelayRegisterSender(keys: keys, clock: { Date() })
        self.defaults = defaults
    }

    func start() {
        guard WCSession.isSupported() else { return }
        let session = WCSession.default
        self.session = session
        session.delegate = self
        session.activate()
    }

    /// A push token arrived, once per launch. The phone hears about it every time, since it
    /// may have been set up again; it pairs only when something changed. Machines already
    /// paired also hear about a changed token through their relay mailbox, so it reaches them
    /// while the phone is away.
    func tokenChanged(_ token: String) async {
        guard let device = try? device(token: token) else { return }
        unsent = device
        sendDevice()
        let fingerprint = Self.fingerprint(device)
        let previous = defaults.string(forKey: Self.sentKey)
        defaults.set(fingerprint, forKey: Self.sentKey)
        guard let previous, previous != fingerprint else { return }
        for pairing in pairings.all() {
            // The Watch shows no Live Activities; the choice is passed on as the phone last paired it.
            try? await register.send(
                (apns: token, liveActivity: nil, liveActivityStart: nil), liveActivityCommand: pairing.liveActivityCommand,
                pairing: pairing
            )
        }
    }

    private func device(token: String) throws -> WatchDevice {
        WatchDevice(
            deviceId: try keys.deviceID(), name: WKInterfaceDevice.current().name,
            x25519: Base64URL.encode(try keys.agreementKey().publicKey.rawRepresentation),
            approvalKey: Base64URL.encode(try keys.signingKey().publicKey.rawRepresentation), apnsToken: token
        )
    }

    private func sendDevice() {
        guard let unsent, let session, session.activationState == .activated,
              let data = try? JSONEncoder().encode(unsent)
        else { return }
        session.transferUserInfo([WatchLinkKey.device: data])
        self.unsent = nil
    }

    /// The phone's latest pairings and context. The pairings replace the Watch's own.
    func received(pairings data: Data?, context contextData: Data?) {
        if let data, let list = try? JSONDecoder().decode([Pairing].self, from: data) {
            let keep = Set(list.map(\.hostId))
            for stale in pairings.all() where !keep.contains(stale.hostId) {
                pairings.remove(hostId: stale.hostId)
            }
            for pairing in list { pairings.put(pairing) }
        }
        if let contextData, let context = try? JSONDecoder().decode(WatchContext.self, from: contextData) {
            store.apply(context: context)
        }
    }

    private static func fingerprint(_ device: WatchDevice) -> String {
        let digest = SHA256.hash(data: Data("\(device.deviceId)|\(device.apnsToken)".utf8))
        return digest.prefix(3).map { String(format: "%02x", $0) }.joined()
    }

    // MARK: WCSessionDelegate

    nonisolated func session(
        _ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: (any Error)?
    ) {
        let latest = session.receivedApplicationContext
        let pairings = latest[WatchLinkKey.pairings] as? Data
        let context = latest[WatchLinkKey.context] as? Data
        Task { @MainActor in
            self.received(pairings: pairings, context: context)
            self.sendDevice()
        }
    }

    nonisolated func session(_ session: WCSession, didReceiveApplicationContext applicationContext: [String: Any]) {
        let pairings = applicationContext[WatchLinkKey.pairings] as? Data
        let context = applicationContext[WatchLinkKey.context] as? Data
        Task { @MainActor in self.received(pairings: pairings, context: context) }
    }
}
