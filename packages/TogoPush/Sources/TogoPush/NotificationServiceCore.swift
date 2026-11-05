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
    }

    /// The `userInfo` key holding the envelope.
    public static let envelopeKey = "c"
    /// The `userInfo` key the app reads when the user acts on a notification.
    public static let infoKey = "togo"
    /// A push older than this is a replay or arrived too late to answer; the helper refuses
    /// answers to it anyway.
    public static let maxAge: TimeInterval = 600
    /// How far ahead of this device's clock a push may claim to be.
    public static let maxSkew: TimeInterval = 60

    private let keys: any DeviceKeyStore
    private let pairings: PairingStore
    private let pending: PendingStore
    private let clock: @Sendable () -> Date

    public init(
        keys: any DeviceKeyStore, pairings: PairingStore, pending: PendingStore, clock: @escaping @Sendable () -> Date
    ) {
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
        guard age <= Self.maxAge, age >= -Self.maxSkew else {
            return Result(content: Self.generic(), pendingChanged: false)
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
        if let bib = p.bib, var link = URLComponents(string: "togo://session/\(bib)") {
            link.queryItems = [URLQueryItem(name: "host", value: opened.pairing.machineName)]
            content.targetContentIdentifier = link.string
        }

        var changed = false
        if p.kind == .needsYou, let requestId = p.requestId {
            if let known = pending.request(requestId), known.answered != nil {
                // Answered already, here or in another process: show it, offer nothing.
                content.categoryIdentifier = NotificationCategoryID.info
                return Result(content: content, pendingChanged: false)
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
                subject: p.subject, options: p.options ?? [], receivedAt: clock(), answered: nil
            ))
            changed = true
        }
        return Result(content: content, pendingChanged: changed)
    }
}

/// Hands the system exactly one content per push: the finished one, or the generic one when
/// time runs out first.
public final class DeliveryGate: Sendable {
    private struct Pending {
        var deliver: (@Sendable (UNNotificationContent) -> Void)?
        var fallback: UNNotificationContent
    }

    private let state: Mutex<Pending>

    public init(deliver: @escaping @Sendable (UNNotificationContent) -> Void) {
        state = Mutex(Pending(deliver: deliver, fallback: NotificationServiceCore.generic()))
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
        if let deliver, let fallback { deliver(fallback) }
    }
}
