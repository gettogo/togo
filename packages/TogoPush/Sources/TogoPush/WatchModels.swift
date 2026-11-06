import Foundation

/// A paired Apple Watch, as the Watch reports it to the phone. Keys are base64url without
/// padding; the token is lowercase hex.
public struct WatchDevice: Codable, Equatable, Sendable {
    public var deviceId: String
    public var name: String
    public var x25519: String
    public var approvalKey: String
    public var apnsToken: String

    public init(deviceId: String, name: String, x25519: String, approvalKey: String, apnsToken: String) {
        self.deviceId = deviceId
        self.name = name
        self.x25519 = x25519
        self.approvalKey = approvalKey
        self.apnsToken = apnsToken
    }
}

/// One session as the Watch lists it. `status` is the wire value (`working`, `needs_you`, ...).
public struct WatchSession: Codable, Sendable, Equatable {
    public var hostName: String
    public var bib: String
    public var agentName: String
    public var status: String
    public var activity: String?
    /// How the agent stopped while `status` is `ended` or `failed`: the exit reason and code
    /// as the helper reports them. Absent from phones that predate them.
    public var exitReason: String?
    public var exitCode: Int?
    /// The phone's id for the machine and the helper's id for the session, so a reply from the
    /// Watch names where it goes. Absent from phones that predate replies.
    public var machineId: String?
    public var sessionId: String?

    public init(
        hostName: String, bib: String, agentName: String, status: String, activity: String?,
        exitReason: String? = nil, exitCode: Int? = nil, machineId: String? = nil, sessionId: String? = nil
    ) {
        self.hostName = hostName
        self.bib = bib
        self.agentName = agentName
        self.status = status
        self.activity = activity
        self.exitReason = exitReason
        self.exitCode = exitCode
        self.machineId = machineId
        self.sessionId = sessionId
    }

    /// The agent waits at its prompt for words: it is done, or it stopped on a usage limit or
    /// an error and is still alive. The same rule picks the notifications that offer Reply.
    public var acceptsReply: Bool {
        guard machineId != nil, let sessionId, !sessionId.isEmpty else { return false }
        switch status {
        case "done", "idle": return true
        default: return LifecycleWords.agentWaiting(status: status, reason: exitReason, code: exitCode)
        }
    }
}

/// What the phone last knew, sent to the Watch as its application context. It lists the ids
/// of requests the phone knows are closed (answered, expired or cleared) so the Watch can drop
/// them. The phone can miss a request the Watch got, so it never lists open ones, and it never
/// carries a request itself.
public struct WatchContext: Codable, Sendable, Equatable {
    public var sessions: [WatchSession]
    public var closedRequestIds: [String]
    public var at: Date
    /// Open requests the phone knows take a typed answer. A push from a helper that predates
    /// `allowText` does not say, so the Watch offers Reply on a request when its push says so
    /// or the phone has named it here.
    public var textRequestIds: [String]?

    public init(sessions: [WatchSession], closedRequestIds: [String], at: Date, textRequestIds: [String]? = nil) {
        self.sessions = sessions
        self.closedRequestIds = closedRequestIds
        self.at = at
        self.textRequestIds = textRequestIds
    }
}

/// How a reply went, on the phone and as the Watch hears it back. `refused` and `failed`
/// carry the sentence to show.
public enum ReplyOutcome: Equatable, Sendable {
    case sent
    /// The machine typed nothing, for a reason it named, such as the agent having ended.
    case refused(String)
    /// The phone could not reach the machine without asking the user something.
    case needsPhone
    case failed(String)

    /// The reply the phone sends back over WatchConnectivity.
    public var message: [String: Any] {
        switch self {
        case .sent: ["outcome": "sent"]
        case .refused(let text): ["outcome": "refused", "text": text]
        case .needsPhone: ["outcome": "needsPhone"]
        case .failed(let text): ["outcome": "failed", "text": text]
        }
    }

    /// Anything unreadable reads as needing the phone, which keeps the text on the Watch.
    public init(message: [String: Any]) {
        let text = message["text"] as? String ?? ""
        switch message["outcome"] as? String {
        case "sent": self = .sent
        case "refused": self = .refused(text)
        case "failed": self = .failed(text)
        default: self = .needsPhone
        }
    }
}

/// Keys of the dictionaries the phone and the Watch exchange over WatchConnectivity. Each
/// value is JSON data.
public enum WatchLinkKey {
    /// Watch to phone, `transferUserInfo`: a `WatchDevice`.
    public static let device = "device"
    /// Phone to Watch, application context: `[Pairing]` for the Watch's own keys.
    public static let pairings = "pairings"
    /// Phone to Watch, application context: a `WatchContext`.
    public static let context = "context"
    /// Phone to Watch, application context: a `Bool`, whether "done" notifications are silent.
    public static let doneSilent = "doneSilent"
}

/// How long a request stays answerable. The helper refuses answers after this anyway.
public enum RequestLifetime {
    public static let seconds: TimeInterval = 600

    /// Requests still waiting on the user at `now`, oldest first.
    public static func waiting(_ requests: [PendingRequest], now: Date) -> [PendingRequest] {
        requests
            .filter { $0.answered == nil && now.timeIntervalSince($0.receivedAt) < seconds }
            .sorted { $0.receivedAt < $1.receivedAt }
    }
}
