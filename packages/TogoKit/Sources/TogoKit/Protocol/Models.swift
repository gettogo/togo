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

public enum SessionStatus: WireEnum {
    case working, needsYou, done, idle, exited
    /// The agent exited on its own with code 0 or after Ctrl-C (lifecycle.v1).
    case ended
    /// The agent crashed, reported an error or stopped on a usage limit (lifecycle.v1).
    case failed
    case unknown(String)

    public static let known: [SessionStatus] = [.working, .needsYou, .done, .idle, .exited, .ended, .failed]

    public var rawValue: String {
        switch self {
        case .working: "working"
        case .needsYou: "needs_you"
        case .done: "done"
        case .idle: "idle"
        case .exited: "exited"
        case .ended: "ended"
        case .failed: "failed"
        case .unknown(let value): value
        }
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
    public var muted: Bool = false
    public var meta: SessionMeta = .init()
    /// launch.v1: the argv the agent was started with, program then args; nil for shell and
    /// unmanaged sessions and from helpers without launch.v1.
    public var command: [String]? = nil
    /// lifecycle.v1: how the agent ended while `status` is `ended` or `failed`, else nil.
    public var exit: SessionExit? = nil
    /// Recent output volume, oldest first: up to 16 values in 0...1, one per 30 seconds. Empty
    /// means unknown (an older helper, or no output seen yet).
    public var activityHistory: [Double] = [] {
        didSet { activityHistory = Self.normalized(activityHistory) }
    }
    /// harness.v1: the activity pill and roster summary; nil when no adapter serves the session.
    public var mission: MissionSummary? = nil
    /// Whether the helper sent `command` and `exit` (as null or a value), so a session
    /// encodes back the way it arrived. Never part of equality.
    private var launchKeys = KeyPresence()

    public init(
        id: String, bib: String?, name: String, agent: String, cwd: String, createdAt: Date,
        status: SessionStatus, activity: String?, attachedClients: Int, managed: Bool,
        muted: Bool = false, meta: SessionMeta = .init(), command: [String]? = nil, exit: SessionExit? = nil,
        activityHistory: [Double] = []
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
        self.muted = muted
        self.meta = meta
        self.command = command
        self.exit = exit
        self.activityHistory = Self.normalized(activityHistory)
        launchKeys = KeyPresence(present: command != nil || exit != nil)
    }

    /// The newest 16 values, each clamped to 0...1 (anything not a number counts as 0).
    static func normalized(_ values: [Double]) -> [Double] {
        values.suffix(16).map { $0.isNaN ? 0 : min(max($0, 0), 1) }
    }

    private enum CodingKeys: String, CodingKey {
        case id, bib, name, agent, cwd, createdAt, status, activity, attachedClients, managed, muted, meta
        case command, exit, activityHistory, mission
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(String.self, forKey: .id)
        bib = try container.decodeIfPresent(String.self, forKey: .bib)
        name = try container.decode(String.self, forKey: .name)
        agent = try container.decode(String.self, forKey: .agent)
        cwd = try container.decode(String.self, forKey: .cwd)
        createdAt = try container.decodeTimestamp(forKey: .createdAt)
        status = try container.decode(SessionStatus.self, forKey: .status)
        activity = try container.decodeIfPresent(String.self, forKey: .activity)
        attachedClients = try container.decode(Int.self, forKey: .attachedClients)
        managed = try container.decode(Bool.self, forKey: .managed)
        // Helpers without mute.v1 and events.v1 omit both keys.
        muted = try container.decodeIfPresent(Bool.self, forKey: .muted) ?? false
        meta = try container.decodeIfPresent(SessionMeta.self, forKey: .meta) ?? .init()
        command = try container.decodeIfPresent([String].self, forKey: .command)
        exit = try container.decodeIfPresent(SessionExit.self, forKey: .exit)
        activityHistory = Self.normalized(try container.decodeIfPresent([Double].self, forKey: .activityHistory) ?? [])
        mission = try container.decodeIfPresent(MissionSummary.self, forKey: .mission)
        launchKeys = KeyPresence(present: container.contains(.command) || container.contains(.exit))
    }

    // bib and activity are required keys in the schema, so nil encodes as null, not as a missing key.
    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encode(bib, forKey: .bib)
        try container.encode(name, forKey: .name)
        try container.encode(agent, forKey: .agent)
        try container.encode(cwd, forKey: .cwd)
        try container.encodeTimestamp(createdAt, forKey: .createdAt)
        try container.encode(status, forKey: .status)
        try container.encode(activity, forKey: .activity)
        try container.encode(attachedClients, forKey: .attachedClients)
        try container.encode(managed, forKey: .managed)
        try container.encode(muted, forKey: .muted)
        try container.encode(meta, forKey: .meta)
        if launchKeys.present || command != nil || exit != nil {
            try container.encode(command, forKey: .command)
            try container.encode(exit, forKey: .exit)
        }
        if !activityHistory.isEmpty {
            try container.encode(activityHistory, forKey: .activityHistory)
        }
        try container.encodeIfPresent(mission, forKey: .mission)
    }
}

/// Remembers whether a decoded value carried optional keys. Always equal, so it never
/// changes how two values compare or hash.
struct KeyPresence: Hashable, Sendable {
    var present = false

    static func == (lhs: KeyPresence, rhs: KeyPresence) -> Bool { true }
    func hash(into hasher: inout Hasher) {}
}

public struct SessionMeta: Codable, Hashable, Sendable {
    public var model: String?
    public var contextPercent: Double?
    public var costUsd: Double?

    public init(model: String? = nil, contextPercent: Double? = nil, costUsd: Double? = nil) {
        self.model = model
        self.contextPercent = contextPercent
        self.costUsd = costUsd
    }

    private enum CodingKeys: String, CodingKey {
        case model, contextPercent, costUsd
    }

    // Every key is required by the schema, so nil encodes as null.
    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(model, forKey: .model)
        try container.encode(contextPercent, forKey: .contextPercent)
        try container.encode(costUsd, forKey: .costUsd)
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

// Timestamps are parsed here rather than through a JSONDecoder date strategy so the
// models decode the same way whichever decoder the caller configured.
extension KeyedDecodingContainer {
    func decodeTimestamp(forKey key: Key) throws -> Date {
        let stamp = try decode(String.self, forKey: key)
        guard let date = RFC3339.parse(stamp) else {
            throw DecodingError.dataCorruptedError(
                forKey: key, in: self, debugDescription: "not an RFC 3339 timestamp: \(stamp)"
            )
        }
        return date
    }

    func decodeTimestampIfPresent(forKey key: Key) throws -> Date? {
        guard try contains(key) && !decodeNil(forKey: key) else { return nil }
        return try decodeTimestamp(forKey: key)
    }
}

extension KeyedEncodingContainer {
    mutating func encodeTimestamp(_ date: Date?, forKey key: Key) throws {
        if let date {
            try encode(RFC3339.format(date), forKey: key)
        } else {
            try encodeNil(forKey: key)
        }
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
    /// With agent `custom`: the program to run, a bare name looked up on the host's login PATH.
    public var program: String?
    /// Flags for the program, each delivered as one argv entry and never read by a shell.
    public var args: [String]?

    public init(
        agent: String? = nil, cwd: String? = nil, name: String? = nil, cols: Int? = nil, rows: Int? = nil,
        program: String? = nil, args: [String]? = nil
    ) {
        self.program = program
        self.args = args
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
    /// Lists files and symlinks too, each entry with its `kind`. Without it, folders only.
    public var includeFiles: Bool?
    /// With `ref`, the session whose cwd `path` is relative to.
    public var id: String?
    /// Lists the tree at this commit instead of the folder on disk. Needs `id`.
    public var ref: String?
    /// `path` is relative to the root of the repository holding the session cwd. Needs `id`.
    public var root: Bool?

    public init(
        path: String, showHidden: Bool? = nil, includeFiles: Bool? = nil, id: String? = nil, ref: String? = nil,
        root: Bool? = nil
    ) {
        self.path = path
        self.showHidden = showHidden
        self.includeFiles = includeFiles
        self.id = id
        self.ref = ref
        self.root = root
    }
}

public struct DirEntry: Codable, Hashable, Sendable {
    public enum Kind: WireEnum {
        case dir, file
        /// Never followed: where it points is not listed.
        case symlink
        case unknown(String)

        public static let known: [Kind] = [.dir, .file, .symlink]

        public var rawValue: String {
            switch self {
            case .dir: "dir"
            case .file: "file"
            case .symlink: "symlink"
            case .unknown(let value): value
            }
        }
    }

    public var name: String
    public var dir: Bool
    /// Present when the listing included files.
    public var kind: Kind?
    /// The file's length in bytes; files only.
    public var size: Int?

    public init(name: String, dir: Bool, kind: Kind? = nil, size: Int? = nil) {
        self.name = name
        self.dir = dir
        self.kind = kind
        self.size = size
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
