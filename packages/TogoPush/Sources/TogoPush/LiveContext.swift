import Foundation

/// What the Live Activity shows beyond its plain fields, sealed by the helper for this device:
/// the session's title, its current step, and the request it is waiting on. Every string is
/// cleaned again before it is drawn.
public struct LiveContext: Codable, Sendable, Equatable {
    /// The buttons that can answer the request without the app.
    public enum Answer: String, Codable, Sendable {
        case approveDeny = "approve_deny"
        case deny
    }

    public var title: String?
    public var step: String?
    public var ask: String?
    public var subject: PushSubject?
    public var answer: Answer?

    public init(
        title: String? = nil, step: String? = nil, ask: String? = nil, subject: PushSubject? = nil,
        answer: Answer? = nil
    ) {
        self.title = title
        self.step = step
        self.ask = ask
        self.subject = subject
        self.answer = answer
    }

    /// An answer word a newer helper adds reads as no buttons, never as a failed decode.
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        title = try? c.decodeIfPresent(String.self, forKey: .title)
        step = try? c.decodeIfPresent(String.self, forKey: .step)
        ask = try? c.decodeIfPresent(String.self, forKey: .ask)
        subject = try? c.decodeIfPresent(PushSubject.self, forKey: .subject)
        answer = try? c.decodeIfPresent(Answer.self, forKey: .answer)
    }

    /// The request as the notification service extension stored it: what the activity shows
    /// when it carries no context, including an activity started by the app. The same roles
    /// that offer notification actions offer Live Activity actions while the request is open.
    public init(pending: PendingRequest, now: Date = Date()) {
        let age = now.timeIntervalSince(pending.receivedAt)
        var answer: Answer?
        if pending.isOpen(at: now), age >= -NotificationServiceCore.maxSkew, age < RequestLifetime.seconds {
            let roles = Set(pending.options.map(\.role))
            if roles.contains("deny") {
                answer = roles.contains("approve") ? .approveDeny : .deny
            }
        }
        self.init(ask: pending.title.isEmpty ? nil : pending.title, subject: pending.subject, answer: answer)
    }

    /// Opens a context sealed for this device by the machine `hostId`. Nil when the keys are
    /// unreadable (before first unlock) or the envelope does not open or decode.
    public static func open(_ envelope: String, hostId: String, keys: any DeviceKeyStore) -> LiveContext? {
        guard let deviceID = try? keys.deviceID(), let key = try? keys.agreementKey(),
              let plain = try? Seal.open(envelope, with: key, label: Labels.live, aad: Data("\(hostId)|\(deviceID)".utf8))
        else { return nil }
        return try? JSONDecoder().decode(LiveContext.self, from: plain)
    }
}
