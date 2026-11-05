import Foundation
import TogoPush
import WatchConnectivity

/// The parts of `WCSession` the bridge uses, so tests can stand in for the Watch.
@MainActor protocol WatchLink: AnyObject {
    /// The session is active, a Watch is paired and the Togo Watch app is on it.
    var canReachWatchApp: Bool { get }
    func updateApplicationContext(_ context: [String: Any]) throws
}

/// The phone's side of the Watch: it takes the Watch's push identity and hands it to pairing,
/// sends the Watch the resulting pairings so it can answer through the relay itself, and keeps
/// the Watch's session list current.
///
/// Everything goes in the application context, which the system delivers once the Watch app
/// runs and keeps only the latest of, so the bridge always sends the pairings and the session
/// context together.
@MainActor final class WatchBridge: NSObject, WCSessionDelegate {
    private(set) var devices: [WatchDevice]
    /// A Watch reported new keys or a new push token; its machines need pairing again.
    var onDevicesChanged: @MainActor () -> Void = {}
    /// The phone's pending requests as they are now; a clear republishes from these, since
    /// requests may have arrived since the last publish.
    var currentRequests: (@MainActor () -> [PendingRequest])?

    private let link: (any WatchLink)?
    private let defaults: UserDefaults
    private let clock: @Sendable () -> Date
    private var pairings: Data?
    private var context: WatchContext?
    private var lastRequests: [PendingRequest] = []
    /// Requests cleared on the phone, by when. A clear can remove the request from the phone's
    /// store, so the bridge keeps the id until the request is past answering anyway.
    private var cleared: [String: Date] = [:]

    private static let devicesKey = "watch.devices"

    init(link: (any WatchLink)?, defaults: UserDefaults = .standard, clock: @escaping @Sendable () -> Date = { Date() }) {
        self.link = link
        self.defaults = defaults
        self.clock = clock
        devices = defaults.data(forKey: Self.devicesKey)
            .flatMap { try? JSONDecoder().decode([WatchDevice].self, from: $0) } ?? []
    }

    /// The bridge over `WCSession.default`, or nil on devices that cannot pair a Watch.
    static func live() -> WatchBridge? {
        guard WCSession.isSupported() else { return nil }
        let session = WCSession.default
        let bridge = WatchBridge(link: SystemWatchLink(session: session))
        session.delegate = bridge
        session.activate()
        return bridge
    }

    /// What the phone knows now: every session, and which requests it knows are closed. Only
    /// those are named: the phone can miss a request the Watch got, so leaving a request out
    /// must never close it on the Watch.
    func publish(sessions: [WatchSession], requests: [PendingRequest]) {
        let now = clock()
        lastRequests = requests
        cleared = cleared.filter { now.timeIntervalSince($0.value) < RequestLifetime.seconds }
        let waiting = Set(RequestLifetime.waiting(requests, now: now).map(\.requestId))
        let closed = requests.filter { $0.answered != .sending && !waiting.contains($0.requestId) }.map(\.requestId)
        context = WatchContext(
            sessions: sessions, closedRequestIds: Set(closed).union(cleared.keys).sorted(), at: now
        )
        flush()
    }

    /// A request was cleared on the phone; the Watch hears it without waiting for the next
    /// session change.
    func cleared(_ requestId: String) {
        cleared[requestId] = clock()
        guard let context else { return }
        publish(sessions: context.sessions, requests: currentRequests?() ?? lastRequests)
    }

    /// The phone's pairings, for the Watch to answer with. They carry no tokens or app ids.
    func sendPairings(_ list: [Pairing]) {
        let forWatch = list.map { pairing in
            var pairing = pairing
            pairing.appHostID = nil
            pairing.tokenHash = ""
            return pairing
        }
        pairings = try? JSONEncoder().encode(forWatch)
        flush()
    }

    /// The Watch sent its push identity. A Watch id seen before is replaced.
    func received(device data: Data) {
        guard let device = try? JSONDecoder().decode(WatchDevice.self, from: data),
              device.deviceId.hasPrefix("w_"), !device.apnsToken.isEmpty
        else { return }
        guard !devices.contains(device) else { return }
        devices.removeAll { $0.deviceId == device.deviceId }
        devices.append(device)
        defaults.set(try? JSONEncoder().encode(devices), forKey: Self.devicesKey)
        onDevicesChanged()
    }

    /// Sends the latest pairings and context when the Watch app can get them; otherwise they
    /// wait for the session to come up.
    func flush() {
        guard let link, link.canReachWatchApp else { return }
        var message: [String: Any] = [:]
        if let pairings { message[WatchLinkKey.pairings] = pairings }
        if let context, let data = try? JSONEncoder().encode(context) { message[WatchLinkKey.context] = data }
        guard !message.isEmpty else { return }
        try? link.updateApplicationContext(message)
    }

    // MARK: WCSessionDelegate

    nonisolated func session(
        _ session: WCSession, activationDidCompleteWith activationState: WCSessionActivationState, error: (any Error)?
    ) {
        Task { @MainActor in self.flush() }
    }

    nonisolated func sessionDidBecomeInactive(_ session: WCSession) {}

    /// The user switched to another Watch; the session starts again for it.
    nonisolated func sessionDidDeactivate(_ session: WCSession) {
        session.activate()
    }

    nonisolated func sessionWatchStateDidChange(_ session: WCSession) {
        Task { @MainActor in self.flush() }
    }

    nonisolated func session(_ session: WCSession, didReceiveUserInfo userInfo: [String: Any]) {
        guard let data = userInfo[WatchLinkKey.device] as? Data else { return }
        Task { @MainActor in self.received(device: data) }
    }
}

/// `WatchLink` over a real `WCSession`.
@MainActor private final class SystemWatchLink: WatchLink {
    private let session: WCSession

    init(session: WCSession) {
        self.session = session
    }

    var canReachWatchApp: Bool {
        session.activationState == .activated && session.isPaired && session.isWatchAppInstalled
    }

    func updateApplicationContext(_ context: [String: Any]) throws {
        try session.updateApplicationContext(context)
    }
}

extension WatchSession {
    /// The Watch's rows for the phone's machines. Activity passes `PromptText.clean`, like
    /// every other string the app shows outside its terminal.
    static func rows(_ machines: [MachineSection]) -> [WatchSession] {
        machines.flatMap { machine in
            machine.sessions.map { row in
                WatchSession(
                    hostName: machine.name, bib: row.bib ?? row.name, agentName: row.agentName,
                    status: row.status.rawValue,
                    activity: row.activity.map { PromptText.clean($0, maxLines: 1) }.flatMap { $0.isEmpty ? nil : $0 }
                )
            }
        }
    }
}
