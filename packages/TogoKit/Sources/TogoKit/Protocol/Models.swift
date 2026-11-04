import Foundation

// Field names follow protocol/v1/schema.json exactly.

/// `{}` on the wire, for methods that take no params.
public struct EmptyParams: Codable, Hashable, Sendable {
    public init() {}

    public init(from decoder: any Decoder) throws {
        _ = try decoder.container(keyedBy: EmptyKeys.self)
    }

    public func encode(to encoder: any Encoder) throws {
        _ = encoder.container(keyedBy: EmptyKeys.self)
    }
}

/// `{}` on the wire, for methods that return nothing (session.kill).
public struct EmptyResult: Codable, Hashable, Sendable {
    public init() {}

    public init(from decoder: any Decoder) throws {
        _ = try decoder.container(keyedBy: EmptyKeys.self)
    }

    public func encode(to encoder: any Encoder) throws {
        _ = encoder.container(keyedBy: EmptyKeys.self)
    }
}

private enum EmptyKeys: CodingKey {}

// MARK: - hello

public struct ClientInfo: Codable, Hashable, Sendable {
    public var name: String
    public var version: String

    public init(name: String, version: String) {
        self.name = name
        self.version = version
    }
}

public struct HelloParams: Codable, Hashable, Sendable {
    public var `protocol`: Int
    public var client: ClientInfo
    public var capabilities: [String]

    public init(protocol: Int, client: ClientInfo, capabilities: [String]) {
        self.protocol = `protocol`
        self.client = client
        self.capabilities = capabilities
    }
}

public struct HelperInfo: Codable, Hashable, Sendable {
    public var version: String
    public var os: String
    public var arch: String
    public var hostname: String

    public init(version: String, os: String, arch: String, hostname: String) {
        self.version = version
        self.os = os
        self.arch = arch
        self.hostname = hostname
    }
}

public struct HelloResult: Codable, Hashable, Sendable {
    public var `protocol`: Int
    public var helper: HelperInfo
    public var capabilities: [String]

    public init(protocol: Int, helper: HelperInfo, capabilities: [String]) {
        self.protocol = `protocol`
        self.helper = helper
        self.capabilities = capabilities
    }
}

// MARK: - Sessions

/// Unknown strings are kept so a newer helper's status does not fail the whole list.
public enum SessionStatus: Codable, Hashable, Sendable {
    case working, needsYou, done, idle, exited
    case other(String)

    public init(rawValue: String) {
        switch rawValue {
        case "working": self = .working
        case "needs_you": self = .needsYou
        case "done": self = .done
        case "idle": self = .idle
        case "exited": self = .exited
        default: self = .other(rawValue)
        }
    }

    public var rawValue: String {
        switch self {
        case .working: "working"
        case .needsYou: "needs_you"
        case .done: "done"
        case .idle: "idle"
        case .exited: "exited"
        case .other(let value): value
        }
    }

    public init(from decoder: any Decoder) throws {
        self.init(rawValue: try decoder.singleValueContainer().decode(String.self))
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(rawValue)
    }
}

public struct Session: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var bib: String?
    public var name: String
    public var agent: String
    public var cwd: String
    public var createdAt: Date
    public var status: SessionStatus
    public var activity: String?
    public var attachedClients: Int
    public var managed: Bool

    public init(
        id: String, bib: String?, name: String, agent: String, cwd: String, createdAt: Date,
        status: SessionStatus, activity: String?, attachedClients: Int, managed: Bool
    ) {
        self.id = id
        self.bib = bib
        self.name = name
        self.agent = agent
        self.cwd = cwd
        self.createdAt = createdAt
        self.status = status
        self.activity = activity
        self.attachedClients = attachedClients
        self.managed = managed
    }

    private enum CodingKeys: String, CodingKey {
        case id, bib, name, agent, cwd, createdAt, status, activity, attachedClients, managed
    }

    // createdAt is parsed here rather than through a JSONDecoder date strategy so the
    // model decodes the same way whichever decoder the caller configured.
    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        bib = try container.decodeIfPresent(String.self, forKey: .bib)
        name = try container.decode(String.self, forKey: .name)
        agent = try container.decode(String.self, forKey: .agent)
        cwd = try container.decode(String.self, forKey: .cwd)
        let stamp = try container.decode(String.self, forKey: .createdAt)
        guard let date = RFC3339.parse(stamp) else {
            throw DecodingError.dataCorruptedError(
                forKey: .createdAt, in: container, debugDescription: "not an RFC 3339 timestamp: \(stamp)"
            )
        }
        createdAt = date
        status = try container.decode(SessionStatus.self, forKey: .status)
        activity = try container.decodeIfPresent(String.self, forKey: .activity)
        attachedClients = try container.decode(Int.self, forKey: .attachedClients)
        managed = try container.decode(Bool.self, forKey: .managed)
    }

    // bib and activity are required keys in the schema, so nil encodes as null, not as a missing key.
    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encode(bib, forKey: .bib)
        try container.encode(name, forKey: .name)
        try container.encode(agent, forKey: .agent)
        try container.encode(cwd, forKey: .cwd)
        try container.encode(RFC3339.format(createdAt), forKey: .createdAt)
        try container.encode(status, forKey: .status)
        try container.encode(activity, forKey: .activity)
        try container.encode(attachedClients, forKey: .attachedClients)
        try container.encode(managed, forKey: .managed)
    }
}

enum RFC3339 {
    static func parse(_ string: String) -> Date? {
        if let date = try? Date.ISO8601FormatStyle().parse(string) { return date }
        return try? Date.ISO8601FormatStyle(includingFractionalSeconds: true).parse(string)
    }

    /// Whole seconds in UTC with a `Z` suffix, matching what the helper emits.
    static func format(_ date: Date) -> String {
        Date(timeIntervalSince1970: date.timeIntervalSince1970.rounded(.down)).formatted(.iso8601)
    }
}

public struct SessionList: Codable, Hashable, Sendable {
    public var sessions: [Session]

    public init(sessions: [Session]) {
        self.sessions = sessions
    }
}

public struct CreateSessionParams: Codable, Hashable, Sendable {
    public var agent: String?
    public var cwd: String?
    public var name: String?
    public var cols: Int?
    public var rows: Int?

    public init(agent: String? = nil, cwd: String? = nil, name: String? = nil, cols: Int? = nil, rows: Int? = nil) {
        self.agent = agent
        self.cwd = cwd
        self.name = name
        self.cols = cols
        self.rows = rows
    }
}

public struct SessionIDParams: Codable, Hashable, Sendable {
    public var id: String

    public init(id: String) {
        self.id = id
    }
}

public struct RenameParams: Codable, Hashable, Sendable {
    public var id: String
    public var name: String

    public init(id: String, name: String) {
        self.id = id
        self.name = name
    }
}

public struct AttachCommandResult: Codable, Hashable, Sendable {
    public var argv: [String]

    public init(argv: [String]) {
        self.argv = argv
    }
}

public struct CaptureParams: Codable, Hashable, Sendable {
    public var id: String
    public var lines: Int?
    public var join: Bool?

    public init(id: String, lines: Int? = nil, join: Bool? = nil) {
        self.id = id
        self.lines = lines
        self.join = join
    }
}

public struct CaptureResult: Codable, Hashable, Sendable {
    public var text: String
    public var truncated: Bool

    public init(text: String, truncated: Bool) {
        self.text = text
        self.truncated = truncated
    }
}

// MARK: - Agents and folders

public struct AgentInfo: Codable, Hashable, Sendable {
    public var id: String
    public var name: String
    public var installed: Bool

    public init(id: String, name: String, installed: Bool) {
        self.id = id
        self.name = name
        self.installed = installed
    }
}

public struct AgentList: Codable, Hashable, Sendable {
    public var agents: [AgentInfo]

    public init(agents: [AgentInfo]) {
        self.agents = agents
    }
}

public struct RecentDirs: Codable, Hashable, Sendable {
    public var dirs: [String]

    public init(dirs: [String]) {
        self.dirs = dirs
    }
}

public struct ListDirParams: Codable, Hashable, Sendable {
    public var path: String
    public var showHidden: Bool?

    public init(path: String, showHidden: Bool? = nil) {
        self.path = path
        self.showHidden = showHidden
    }
}

public struct DirEntry: Codable, Hashable, Sendable {
    public var name: String
    public var dir: Bool

    public init(name: String, dir: Bool) {
        self.name = name
        self.dir = dir
    }
}

public struct ListDirResult: Codable, Hashable, Sendable {
    public var path: String
    public var entries: [DirEntry]

    public init(path: String, entries: [DirEntry]) {
        self.path = path
        self.entries = entries
    }
}
