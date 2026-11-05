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

    public init(hostName: String, bib: String, agentName: String, status: String, activity: String?) {
        self.hostName = hostName
        self.bib = bib
        self.agentName = agentName
        self.status = status
        self.activity = activity
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

    public init(sessions: [WatchSession], closedRequestIds: [String], at: Date) {
        self.sessions = sessions
        self.closedRequestIds = closedRequestIds
        self.at = at
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
