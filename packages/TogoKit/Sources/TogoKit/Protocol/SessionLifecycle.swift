import Foundation

// How a session's agent stopped (lifecycle.v1), sessions a host restart ended (restore.v1) and
// words typed at an agent's prompt (reply.v1). Field names follow protocol/v1/schema.json
// exactly.

public enum ExitReason: WireEnum {
    case exit, interrupted, error, usageLimit, restart
    case unknown(String)

    public static let known: [ExitReason] = [.exit, .interrupted, .error, .usageLimit, .restart]

    public var rawValue: String {
        switch self {
        case .exit: "exit"
        case .interrupted: "interrupted"
        case .error: "error"
        case .usageLimit: "usage_limit"
        case .restart: "restart"
        case .unknown(let value): value
        }
    }
}

/// How a session's agent ended. `code` is nil when the process reported none; `detail` is
/// a short plain-text line the helper wrote, never raw terminal text.
public struct SessionExit: Codable, Hashable, Sendable {
    public var code: Int?
    public var reason: ExitReason
    public var detail: String?

    public init(code: Int?, reason: ExitReason, detail: String? = nil) {
        self.code = code
        self.reason = reason
        self.detail = detail
    }

    private enum CodingKeys: String, CodingKey { case code, reason, detail }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        code = try container.decodeIfPresent(Int.self, forKey: .code)
        reason = try container.decode(ExitReason.self, forKey: .reason)
        detail = try container.decodeIfPresent(String.self, forKey: .detail)
    }

    // All three keys are required by the schema, so nil encodes as null.
    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(code, forKey: .code)
        try container.encode(reason, forKey: .reason)
        try container.encode(detail, forKey: .detail)
    }
}

// MARK: - Restore

/// A managed session a host restart ended, which the user can restore or dismiss.
public struct RestorableSession: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var bib: String
    public var name: String
    public var agent: String
    public var command: [String]?
    public var cwd: String
    public var createdAt: Date
    public var endedAt: Date
    /// The agent can continue its previous conversation (`restore` with `resume`).
    public var resumable: Bool

    public init(
        id: String, bib: String, name: String, agent: String, command: [String]?, cwd: String,
        createdAt: Date, endedAt: Date, resumable: Bool
    ) {
        self.id = id
        self.bib = bib
        self.name = name
        self.agent = agent
        self.command = command
        self.cwd = cwd
        self.createdAt = createdAt
        self.endedAt = endedAt
        self.resumable = resumable
    }

    private enum CodingKeys: String, CodingKey {
        case id, bib, name, agent, command, cwd, createdAt, endedAt, resumable
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        bib = try container.decode(String.self, forKey: .bib)
        name = try container.decode(String.self, forKey: .name)
        agent = try container.decode(String.self, forKey: .agent)
        command = try container.decodeIfPresent([String].self, forKey: .command)
        cwd = try container.decode(String.self, forKey: .cwd)
        createdAt = try container.decodeTimestamp(forKey: .createdAt)
        endedAt = try container.decodeTimestamp(forKey: .endedAt)
        resumable = try container.decode(Bool.self, forKey: .resumable)
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encode(bib, forKey: .bib)
        try container.encode(name, forKey: .name)
        try container.encode(agent, forKey: .agent)
        try container.encode(command, forKey: .command)
        try container.encode(cwd, forKey: .cwd)
        try container.encodeTimestamp(createdAt, forKey: .createdAt)
        try container.encodeTimestamp(endedAt, forKey: .endedAt)
        try container.encode(resumable, forKey: .resumable)
    }
}

struct RestorableList: Codable, Hashable, Sendable {
    var sessions: [RestorableSession]
}

struct RestoreParams: Codable, Hashable, Sendable {
    var id: String
    var resume: Bool
}

struct SendTextParams: Codable, Hashable, Sendable {
    var id: String
    var text: String
    var submit: Bool
}
