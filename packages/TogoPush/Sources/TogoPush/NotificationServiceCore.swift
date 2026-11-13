import Foundation
import Synchronization
import UserNotifications

/// What the notification service extension does with one push, kept out of the extension so
/// it can be tested: open the envelope, build the text, remember the request.
///
/// Every failure (keys unreadable before first unlock, no pairing for the sender, an envelope
/// that does not open or decode) gives the generic content: no actions, no ciphertext and
/// nothing of the plaintext.
public struct NotificationServiceCore: Sendable {
    /// The push opened, and the machine whose envelope it was.
    public struct Opened: Sendable {
        public var payload: PushPayload
        public var pairing: Pairing
    }

    public struct Result {
        public var content: UNNotificationContent
        /// True when a request was added to `PendingStore`, so widgets should reload.
        public var pendingChanged: Bool
        /// The machine whose delivered "stopped answering" notifications should be removed,
        /// because it just sent something else.
        public var clearsOfflineOf: String?

        public init(content: UNNotificationContent, pendingChanged: Bool, clearsOfflineOf: String? = nil) {
            self.content = content
            self.pendingChanged = pendingChanged
            self.clearsOfflineOf = clearsOfflineOf
        }
    }

    /// The `userInfo` key holding the envelope.
    public static let envelopeKey = "c"
    /// The `userInfo` key the app reads when the user acts on a notification.
    public static let infoKey = "togo"
    /// A push older than this is a replay or arrived too late to answer; the helper refuses
    /// answers to it anyway.
    public static let maxAge: TimeInterval = 600
    /// An offline push is sealed ahead of time. The helper replaces it every 6 hours
    /// (`willRefresh` in helper/internal/push/will.go) and the relay drops one unsent after 24
    /// hours (`WILL_TTL_MS` in relay/src/will.ts), so the oldest seal that can still arrive is
    /// the 24 hour limit plus one refresh interval of slack.
    public static let offlineMaxAge: TimeInterval = 30 * 3600
    /// How far ahead of this device's clock a push may claim to be.
    public static let maxSkew: TimeInterval = 60

    private let keys: any DeviceKeyStore
    private let pairings: PairingStore
    private let pending: PendingStore
    private let clock: @Sendable () -> Date
    private let doneSilent: @Sendable () -> Bool

    /// - Parameter doneSilent: whether a "done" is delivered without sound; read for each push
    ///   so a change in the app applies at once.
    public init(
        keys: any DeviceKeyStore, pairings: PairingStore, pending: PendingStore, clock: @escaping @Sendable () -> Date,
        doneSilent: @escaping @Sendable () -> Bool = { SharedSettings.readDoneSilent() }
    ) {
        self.doneSilent = doneSilent
        self.keys = keys
        self.pairings = pairings
        self.pending = pending
        self.clock = clock
    }

    /// The text shown when a push cannot be read.
    public static func generic() -> UNNotificationContent {
        let content = UNMutableNotificationContent()
        content.title = "Togo"
        content.body = String(localized: "notif.generic", defaultValue: "A session needs you")
        content.sound = .default
        content.threadIdentifier = "togo"
        return content
    }

    /// The text shown for a push that opened but is too old or seen already. The service
    /// cannot drop it, so it claims nothing, makes no sound and stays out of the way.
    public static func quiet() -> UNNotificationContent {
        let content = UNMutableNotificationContent()
        content.title = "Togo"
        content.body = String(localized: "notif.quiet", defaultValue: "Open Togo for the latest.")
        content.interruptionLevel = .passive
        content.threadIdentifier = "togo"
        return content
    }

    /// Opens the envelope in `userInfo`. The plaintext names no machine, so each pairing is
    /// tried with its own aad (`hostId|deviceId`) and only the sender's opens.
    public func open(userInfo: [AnyHashable: Any]) -> Opened? {
        guard let envelope = userInfo[Self.envelopeKey] as? String,
              let deviceID = try? keys.deviceID(),
              let key = try? keys.agreementKey()
        else { return nil }
        for pairing in pairings.all() {
            let aad = Data("\(pairing.hostId)|\(deviceID)".utf8)
            guard let plaintext = try? Seal.open(envelope, with: key, label: Labels.push, aad: aad) else { continue }
            guard let payload = try? JSONDecoder().decode(PushPayload.self, from: plaintext) else { return nil }
            return Opened(payload: payload, pairing: pairing)
        }
        return nil
    }

    public func process(_ original: UNNotificationContent) -> Result {
        guard let opened = open(userInfo: original.userInfo),
              let text = NotificationTextBuilder.build(
                  opened.payload, hostId: opened.pairing.hostId, hostName: opened.pairing.machineName,
                  multipleHosts: pairings.all().count > 1
              )
        else { return Result(content: Self.generic(), pendingChanged: false) }

        let p = opened.payload
        let age = clock().timeIntervalSince1970 - Double(p.ts) / 1000
        let limit = p.kind == .offline ? Self.offlineMaxAge : Self.maxAge
        guard age <= limit, age >= -Self.maxSkew else {
            return Result(content: Self.quiet(), pendingChanged: false)
        }
        if p.kind == .offline {
            guard pending.claimOffline(hostId: opened.pairing.hostId, ts: p.ts) else {
                return Result(content: Self.quiet(), pendingChanged: false)
            }
        } else {
            // Heard from the machine: its next silence is a new outage.
            pending.clearOfflineMark(hostId: opened.pairing.hostId)
        }
        let content = UNMutableNotificationContent()
        content.title = text.title
        content.subtitle = text.subtitle
        content.body = text.body
        content.sound = original.sound
        content.categoryIdentifier = text.category
        content.threadIdentifier = text.threadID
        content.interruptionLevel = switch text.interruption {
        case .active: .active
        case .timeSensitive: .timeSensitive
        case .passive: .passive
        }
        if p.kind == .done, doneSilent() {
            // Only a done is quieted; requests, stops and outages keep their sound.
            content.sound = nil
            content.interruptionLevel = .passive
        }
        if let bib = p.bib, var link = URLComponents(string: "togo://session/\(bib)") {
            link.queryItems = [URLQueryItem(name: "host", value: opened.pairing.machineName)]
            content.targetContentIdentifier = link.string
        }

        if text.category == NotificationCategoryID.ended || text.category == NotificationCategoryID.prompt,
           let sessionId = p.sessionId, !sessionId.isEmpty {
            // Run again and Reply name the session they act on, by identifiers only.
            content.userInfo = [Self.infoKey: ["hostId": opened.pairing.hostId, "sessionId": sessionId]]
        }

        if p.kind == .offline {
            content.userInfo = [Self.infoKey: ["hostId": opened.pairing.hostId, OfflineNotifications.flag: "1"]]
        }
        let clearsOffline = p.kind == .offline ? nil : opened.pairing.hostId

        var changed = false
        if p.kind == .needsYou, let requestId = p.requestId {
            if let known = pending.request(requestId), !known.isOpen(at: clock()) {
                // Answered already, here or in another process: show it, offer nothing.
                content.categoryIdentifier = NotificationCategoryID.info
                return Result(content: content, pendingChanged: false, clearsOfflineOf: clearsOffline)
            }
            // Only identifiers and option labels: the app answers from these, and the
            // question and command stay out of anything the system keeps beside the text.
            content.userInfo = [Self.infoKey: [
                "requestId": requestId,
                "hostId": opened.pairing.hostId,
                "sessionId": p.sessionId ?? "",
                "options": (p.options ?? []).map { ["n": $0.n, "label": $0.label, "role": $0.role] },
            ] as [String: Any]]
            pending.upsert(PendingRequest(
                requestId: requestId, hostId: opened.pairing.hostId, sessionId: p.sessionId, bib: p.bib, agent: p.agent,
                host: p.host ?? opened.pairing.machineName, title: PromptText.clean(p.title, maxLines: 1),
                // The window runs from when the machine sent it: a push can arrive late.
                subject: p.subject, options: p.options ?? [],
                receivedAt: min(clock(), Date(timeIntervalSince1970: Double(p.ts) / 1000)), answered: nil,
                allowText: p.allowText == true ? true : nil
            ))
            changed = true
        }
        return Result(content: content, pendingChanged: changed, clearsOfflineOf: clearsOffline)
    }
}

/// Hands the system exactly one content per push: the finished one, or the generic one when
/// time runs out first.
public final class DeliveryGate: Sendable {
    /// UNNotificationContent is immutable; copying also freezes mutable subclass inputs.
    private struct Snapshot: @unchecked Sendable {
        let value: UNNotificationContent
        init(_ content: UNNotificationContent) { value = content.copy() as! UNNotificationContent }
    }

    private struct Pending {
        var deliver: (@Sendable (UNNotificationContent) -> Void)?
        var fallback: Snapshot
    }

    private let state: Mutex<Pending>

    public init(deliver: @escaping @Sendable (UNNotificationContent) -> Void) {
        state = Mutex(Pending(deliver: deliver, fallback: Snapshot(NotificationServiceCore.generic())))
    }

    /// Keep already decoded actions if the OS expires the extension while its receipt is sent.
    public func prepare(_ content: UNNotificationContent) {
        let snapshot = Snapshot(content)
        state.withLock { pending in
            if pending.deliver != nil { pending.fallback = snapshot }
        }
    }

    public func finish(_ content: UNNotificationContent) {
        let deliver = state.withLock { pending in
            defer { pending.deliver = nil }
            return pending.deliver
        }
        deliver?(content)
    }

    /// `serviceExtensionTimeWillExpire`: the generic content, unless already delivered.
    public func expire() {
        let (deliver, fallback) = state.withLock { pending in
            defer { pending.deliver = nil }
            return (pending.deliver, pending.deliver == nil ? nil : pending.fallback)
        }
        if let deliver, let fallback { deliver(fallback.value) }
    }
}

/// Finds the delivered "stopped answering" notifications of a machine, so a later push from it
/// can take them away.
public enum OfflineNotifications {
    /// The `userInfo` entry that marks one.
    public static let flag = "offline"

    public static func identifiers(
        in delivered: [(identifier: String, userInfo: [AnyHashable: Any])], hostId: String
    ) -> [String] {
        delivered.compactMap { item in
            guard let info = item.userInfo[NotificationServiceCore.infoKey] as? [String: Any],
                  info[flag] != nil, info["hostId"] as? String == hostId
            else { return nil }
            return item.identifier
        }
    }

    /// Removes them from the notification center.
    public static func removeDelivered(hostId: String, center: UNUserNotificationCenter = .current()) async {
        let delivered = await center.deliveredNotifications().map { ($0.request.identifier, $0.request.content.userInfo) }
        let ids = identifiers(in: delivered.map { (identifier: $0.0, userInfo: $0.1) }, hostId: hostId)
        if !ids.isEmpty { center.removeDeliveredNotifications(withIdentifiers: ids) }
    }
}
