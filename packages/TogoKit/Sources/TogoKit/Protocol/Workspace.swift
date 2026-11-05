import Foundation

// git.*, fs.read and ports.list. Paths are relative to the session cwd. Keys the
// schema requires but allows to be null encode as null rather than going missing.

public struct GitStatus: Codable, Hashable, Sendable {
    public struct File: Codable, Hashable, Sendable {
        public var path: String
        public var origPath: String?
        /// Porcelain v2 status letters, `.` when unchanged.
        public var index: String
        public var worktree: String

        public init(path: String, origPath: String?, index: String, worktree: String) {
            self.path = path
            self.origPath = origPath
            self.index = index
            self.worktree = worktree
        }

        private enum CodingKeys: String, CodingKey {
            case path, origPath, index, worktree
        }

        public func encode(to encoder: any Encoder) throws {
            var container = encoder.container(keyedBy: CodingKeys.self)
            try container.encode(path, forKey: .path)
            try container.encode(origPath, forKey: .origPath)
            try container.encode(index, forKey: .index)
            try container.encode(worktree, forKey: .worktree)
        }
    }

    public var repo: Bool
    public var root: String?
    /// Nil on a detached HEAD or outside a repository.
    public var branch: String?
    public var ahead: Int
    public var behind: Int
    public var files: [File]

    public init(repo: Bool, root: String?, branch: String?, ahead: Int, behind: Int, files: [File]) {
        self.repo = repo
        self.root = root
        self.branch = branch
        self.ahead = ahead
        self.behind = behind
        self.files = files
    }

    private enum CodingKeys: String, CodingKey {
        case repo, root, branch, ahead, behind, files
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(repo, forKey: .repo)
        try container.encode(root, forKey: .root)
        try container.encode(branch, forKey: .branch)
        try container.encode(ahead, forKey: .ahead)
        try container.encode(behind, forKey: .behind)
        try container.encode(files, forKey: .files)
    }
}

public struct GitDiffParams: Codable, Hashable, Sendable {
    public var id: String
    public var path: String?
    public var staged: Bool?
    public var ref: String?

    public init(id: String, path: String? = nil, staged: Bool? = nil, ref: String? = nil) {
        self.id = id
        self.path = path
        self.staged = staged
        self.ref = ref
    }
}

public struct GitDiff: Codable, Hashable, Sendable {
    public var diff: String
    public var truncated: Bool
    /// Paths whose content differs but is not shown.
    public var binary: [String]

    public init(diff: String, truncated: Bool, binary: [String]) {
        self.diff = diff
        self.truncated = truncated
        self.binary = binary
    }
}

public struct GitLogParams: Codable, Hashable, Sendable {
    public var id: String
    public var ref: String?
    public var limit: Int?

    public init(id: String, ref: String? = nil, limit: Int? = nil) {
        self.id = id
        self.ref = ref
        self.limit = limit
    }
}

public struct GitCommit: Codable, Hashable, Sendable {
    public var hash: String
    public var short: String
    public var author: String
    public var date: Date
    public var subject: String

    public init(hash: String, short: String, author: String, date: Date, subject: String) {
        self.hash = hash
        self.short = short
        self.author = author
        self.date = date
        self.subject = subject
    }

    private enum CodingKeys: String, CodingKey {
        case hash, short, author, date, subject
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        hash = try container.decode(String.self, forKey: .hash)
        short = try container.decode(String.self, forKey: .short)
        author = try container.decode(String.self, forKey: .author)
        date = try container.decodeTimestamp(forKey: .date)
        subject = try container.decode(String.self, forKey: .subject)
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(hash, forKey: .hash)
        try container.encode(short, forKey: .short)
        try container.encode(author, forKey: .author)
        try container.encodeTimestamp(date, forKey: .date)
        try container.encode(subject, forKey: .subject)
    }
}

public struct GitLog: Codable, Hashable, Sendable {
    public var commits: [GitCommit]

    public init(commits: [GitCommit]) {
        self.commits = commits
    }
}

public struct GitBranches: Codable, Hashable, Sendable {
    public struct Branch: Codable, Hashable, Sendable {
        public var name: String
        public var upstream: String?
        public var current: Bool

        public init(name: String, upstream: String?, current: Bool) {
            self.name = name
            self.upstream = upstream
            self.current = current
        }

        private enum CodingKeys: String, CodingKey {
            case name, upstream, current
        }

        public func encode(to encoder: any Encoder) throws {
            var container = encoder.container(keyedBy: CodingKeys.self)
            try container.encode(name, forKey: .name)
            try container.encode(upstream, forKey: .upstream)
            try container.encode(current, forKey: .current)
        }
    }

    public var current: String?
    public var branches: [Branch]

    public init(current: String?, branches: [Branch]) {
        self.current = current
        self.branches = branches
    }

    private enum CodingKeys: String, CodingKey {
        case current, branches
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(current, forKey: .current)
        try container.encode(branches, forKey: .branches)
    }
}

public struct FsReadParams: Codable, Hashable, Sendable {
    public var id: String
    public var path: String
    /// Reads the file as of this commit instead of the working tree.
    public var ref: String?

    public init(id: String, path: String, ref: String? = nil) {
        self.id = id
        self.path = path
        self.ref = ref
    }
}

public struct FsReadResult: Codable, Hashable, Sendable {
    public enum Encoding: WireEnum {
        case utf8
        /// base64url without padding, used when the bytes are not valid UTF-8.
        case base64
        case unknown(String)

        public static let known: [Encoding] = [.utf8, .base64]

        public var rawValue: String {
            switch self {
            case .utf8: "utf8"
            case .base64: "base64"
            case .unknown(let value): value
            }
        }
    }

    public var path: String
    /// The full file size; `content` holds at most 512 KiB of it.
    public var size: Int
    public var encoding: Encoding
    public var content: String
    public var truncated: Bool

    public init(path: String, size: Int, encoding: Encoding, content: String, truncated: Bool) {
        self.path = path
        self.size = size
        self.encoding = encoding
        self.content = content
        self.truncated = truncated
    }

    /// The bytes `content` carries, nil when base64 content does not decode.
    public var data: Data? {
        switch encoding {
        case .utf8: Data(content.utf8)
        case .base64: Base64URL.decode(content)
        case .unknown: nil
        }
    }
}

public struct ListeningPort: Codable, Hashable, Sendable {
    public var port: Int
    public var address: String
    public var pid: Int
    public var command: String

    public init(port: Int, address: String, pid: Int, command: String) {
        self.port = port
        self.address = address
        self.pid = pid
        self.command = command
    }
}

public struct PortList: Codable, Hashable, Sendable {
    public var ports: [ListeningPort]

    public init(ports: [ListeningPort]) {
        self.ports = ports
    }
}
