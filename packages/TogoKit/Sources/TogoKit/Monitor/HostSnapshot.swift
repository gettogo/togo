import Foundation

/// What the control center knows about one machine.
public struct HostSnapshot: Sendable, Equatable {
    public enum Reach: Sendable, Equatable {
        case connecting
        case waking
        case online
        /// Several attempts in a row failed. `sessions` and `lastSeen` are from the last connection.
        case offline(String)
        /// Signing in needs the user: a password, a one-time code or a new host key.
        case authNeeded
        /// Retrying cannot help until something changes on the host.
        case failed(String)
        /// The machine signs in with a key that is not on this device. Only choosing
        /// another key helps.
        case keyMissing
    }

    /// The newest requests closed while this device watched, so a card on screen can say why
    /// it went away. Bounded, and kept across snapshots, because a slow reader may skip the
    /// state that first carried a close.
    public static let closedLimit = 32

    public var hostID: UUID
    public var reach: Reach
    public var helper: HelperPresence
    public var sessions: [Session]
    public var approvals: [Approval]
    public var lastSeen: Date?
    public var capabilities: Set<String>
    public var closed: [ClosedApproval] = []
    /// Keep-awake state; nil until the helper reports it or when it has no `awake.v1`.
    public var awake: AwakeStatus?
    /// The machine's interfaces for Wake-on-LAN; nil until read or when it has no `hostinfo.v1`.
    public var hostInfo: HostInfo?

    public init(
        hostID: UUID, reach: Reach = .connecting, helper: HelperPresence = .unknown, sessions: [Session] = [],
        approvals: [Approval] = [], lastSeen: Date? = nil, capabilities: Set<String> = []
    ) {
        self.hostID = hostID
        self.reach = reach
        self.helper = helper
        self.sessions = sessions
        self.approvals = approvals
        self.lastSeen = lastSeen
        self.capabilities = capabilities
    }

    /// Folds one event in. Upserts keep a row's position so the list does not jump.
    mutating func apply(_ envelope: EventEnvelope) {
        switch envelope.event {
        case .session(let session):
            upsert(session)
        case .sessionRemoved:
            guard let id = envelope.sessionId else { return }
            sessions.removeAll { $0.id == id }
        case .approvalOpen(let approval):
            if let index = approvals.firstIndex(where: { $0.requestId == approval.requestId }) {
                approvals[index] = approval
            } else {
                approvals.append(approval)
            }
        case .approvalClosed(let requestId, let resolution):
            // A second close, such as a second answer, keeps the first reason.
            guard approvals.contains(where: { $0.requestId == requestId }) else { return }
            removeApproval(requestId)
            closed.append(ClosedApproval(requestId: requestId, resolution: resolution))
            if closed.count > Self.closedLimit { closed.removeFirst(closed.count - Self.closedLimit) }
        case .awake(let status):
            awake = status
        case .notify, .job, .unknown:
            break
        }
    }

    mutating func upsert(_ session: Session) {
        if let index = sessions.firstIndex(where: { $0.id == session.id }) {
            sessions[index] = session
        } else {
            sessions.append(session)
        }
    }

    mutating func removeApproval(_ requestId: String) {
        approvals.removeAll { $0.requestId == requestId }
    }
}

public struct ClosedApproval: Sendable, Equatable {
    public var requestId: String
    public var resolution: CloseResolution

    public init(requestId: String, resolution: CloseResolution) {
        self.requestId = requestId
        self.resolution = resolution
    }
}
