import Foundation

// git.*, fs.read and ports.list. Git paths are relative to the repository root; fs.read
// paths to the session cwd unless `root` is set. Keys the
// schema requires but allows to be null encode as null rather than going missing.

public struct GitStatus: Codable, Hashable, Sendable {
    public struct File: Codable, Hashable, Sendable {
        public var path: String
        public var origPath: String?
        /// Porcelain v2 status letters, `.` when unchanged.
        public var index: String
        public var worktree: String
        /// Identifies the working-tree content (`blob:<hash>` or `stat:<size>:<mtime_ns>`),
        /// compared only for equality. Nil when there is no file in the working tree, or
        /// from a helper without `gitwrite.v1`.
        public var fingerprint: String?

        public init(path: String, origPath: String?, index: String, worktree: String, fingerprint: String? = nil) {
            self.path = path
            self.origPath = origPath
            self.index = index
            self.worktree = worktree
            self.fingerprint = fingerprint
        }

        private enum CodingKeys: String, CodingKey {
            case path, origPath, index, worktree, fingerprint
        }

        public func encode(to encoder: any Encoder) throws {
            var container = encoder.container(keyedBy: CodingKeys.self)
            try container.encode(path, forKey: .path)
            try container.encode(origPath, forKey: .origPath)
            try container.encode(index, forKey: .index)
            try container.encode(worktree, forKey: .worktree)
            try container.encodeIfPresent(fingerprint, forKey: .fingerprint)
        }
    }

    public var repo: Bool
    public var root: String?
    /// Nil on a detached HEAD or outside a repository.
    public var branch: String?
    public var ahead: Int
    public var behind: Int
    /// True when the helper left files out (it lists at most 2,000); nil from a helper
    /// without `gitwrite.v1`. Read `isTruncated`.
    public var truncated: Bool?
    /// Every changed path, untracked files one by one (never a folder entry) from a
    /// helper with `gitwrite.v1`.
    public var files: [File]

    /// Some changes are not in `files`, so nothing may act on "all changes" (such as
    /// discarding everything): the user would destroy work they never saw.
    public var isTruncated: Bool { truncated == true }

    public init(
        repo: Bool, root: String?, branch: String?, ahead: Int, behind: Int, truncated: Bool? = nil, files: [File]
    ) {
        self.repo = repo
        self.root = root
        self.branch = branch
        self.ahead = ahead
        self.behind = behind
        self.truncated = truncated
        self.files = files
    }

    private enum CodingKeys: String, CodingKey {
        case repo, root, branch, ahead, behind, truncated, files
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(repo, forKey: .repo)
        try container.encode(root, forKey: .root)
        try container.encode(branch, forKey: .branch)
        try container.encode(ahead, forKey: .ahead)
        try container.encode(behind, forKey: .behind)
        try container.encodeIfPresent(truncated, forKey: .truncated)
        try container.encode(files, forKey: .files)
    }
}

public struct GitDiffParams: Codable, Hashable, Sendable {
    public var id: String
    public var path: String?
    public var staged: Bool?
    public var ref: String?
    /// Diffs this commit against its first parent. Not combined with `staged` or `ref`.
    public var commit: String?

    public init(id: String, path: String? = nil, staged: Bool? = nil, ref: String? = nil, commit: String? = nil) {
        self.id = id
        self.path = path
        self.staged = staged
        self.ref = ref
        self.commit = commit
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

extension GitStatus.File {
    /// The index and worktree letters together, index first, as `GitSeen.status` sends them.
    public var letters: String { index + worktree }
}

/// What the user saw for one path when they chose it. A file edited again keeps its
/// letters but not its fingerprint, so the helper can still tell it changed.
public struct GitSeen: Codable, Hashable, Sendable {
    public var status: String
    public var fingerprint: String?

    public init(status: String, fingerprint: String? = nil) {
        self.status = status
        self.fingerprint = fingerprint
    }

    public init(_ file: GitStatus.File) {
        self.init(status: file.letters, fingerprint: file.fingerprint)
    }
}

/// `git.stage`, `git.unstage` and `git.discard`. Each path names one file relative to the
/// session cwd; a folder (or a path ending in `/`) is refused as invalid params.
///
/// `git.discard` restores a path in HEAD to HEAD in both the index and the working tree,
/// and deletes a path not in HEAD (untracked, or only added to the index). Nothing else.
public struct GitPathsParams: Codable, Hashable, Sendable {
    public var id: String
    public var paths: [String]
    /// What `git.status` showed for each path when the user chose it. The helper refuses
    /// the call as stale when the file's state or content has changed since.
    public var seen: [String: GitSeen]?

    /// The longest path a git write accepts, in UTF-8 bytes.
    public static let maxPathBytes = 4096

    /// A single file path of 1 to `maxPathBytes` bytes with no NUL and no trailing `/`.
    public static func isValidPath(_ path: String) -> Bool {
        !path.isEmpty && path.utf8.count <= maxPathBytes && !path.utf8.contains(0) && !path.hasSuffix("/")
    }

    public init(id: String, paths: [String], seen: [String: GitSeen]? = nil) {
        self.id = id
        self.paths = paths
        self.seen = seen
    }

    /// Acts on these status entries and tells the helper what they looked like.
    public init(id: String, files: [GitStatus.File]) {
        self.init(
            id: id, paths: files.map(\.path),
            seen: Dictionary(files.map { ($0.path, GitSeen($0)) }, uniquingKeysWith: { _, last in last })
        )
    }
}

/// `git.commit`. Hooks and signing never run.
public struct GitCommitParams: Codable, Hashable, Sendable {
    public var id: String
    public var message: String
    public var amend: Bool
    /// The HEAD hash the user saw: required with `amend` (the commit it replaces). The
    /// helper refuses the commit as stale when HEAD has moved since.
    public var head: String?

    /// A full SHA-1 or SHA-256 commit id in lowercase hex.
    public static func isValidHash(_ hash: String) -> Bool {
        (hash.utf8.count == 40 || hash.utf8.count == 64)
            && hash.utf8.allSatisfy { (48...57).contains($0) || (97...102).contains($0) }
    }

    /// `amend` names the commit it replaces, and any `head` is a full hash.
    public var isValid: Bool {
        if let head { Self.isValidHash(head) } else { !amend }
    }

    public init(id: String, message: String, amend: Bool, head: String? = nil) {
        self.id = id
        self.message = message
        self.amend = amend
        self.head = head
    }
}

public struct GitCommitResult: Codable, Hashable, Sendable {
    public var hash: String
    public var short: String

    public init(hash: String, short: String) {
        self.hash = hash
        self.short = short
    }
}

/// Why the helper refused a git write. Nothing in the repository changed, except for
/// `partial` and `raced`.
public enum GitConflict: String, Sendable {
    /// A path or HEAD no longer looks the way the user saw it.
    case stale
    /// Another git process holds the index lock.
    case locked
    /// The path uses a clean or smudge filter, such as Git LFS.
    case filtered
    /// Discarding a path with an unresolved merge conflict.
    case unmerged
    case nothingStaged = "nothing_staged"
    /// git has no user name or email on the host.
    case identity
    /// Amending before the first commit.
    case unbornAmend = "unborn_amend"
    /// The write failed after changing some of the paths. The status should be read again.
    case partial
    /// The commit was made, but another process moved HEAD while it ran, so its parent is not
    /// the commit the user saw. Nothing was rewritten.
    case raced
    /// Undoing a rename that only changes letter case; both names are one file on a
    /// case-insensitive disk. Nothing changed.
    case caseRename = "case_rename"
}

public struct GitLogParams: Codable, Hashable, Sendable {
    public var id: String
    public var ref: String?
    public var limit: Int?
    /// `gitfiles.v1`: topological order, so a commit always comes before its parents, and a
    /// changed-file count on each commit. An older helper refuses the unknown field.
    public var graph: Bool?

    public init(id: String, ref: String? = nil, limit: Int? = nil, graph: Bool? = nil) {
        self.id = id
        self.ref = ref
        self.limit = limit
        self.graph = graph
    }
}

/// `git.files` (`gitfiles.v1`): the repository's files, or a commit's with `ref`.
public struct GitFilesParams: Codable, Hashable, Sendable {
    public var id: String
    public var ref: String?

    public init(id: String, ref: String? = nil) {
        self.id = id
        self.ref = ref
    }
}

/// Paths relative to the repository root, sorted; `truncated` when the helper left some out.
public struct GitFiles: Codable, Hashable, Sendable {
    public var paths: [String]
    public var truncated: Bool

    public init(paths: [String], truncated: Bool) {
        self.paths = paths
        self.truncated = truncated
    }
}

public struct GitCommit: Codable, Hashable, Sendable {
    public var hash: String
    public var short: String
    public var author: String
    public var date: Date
    public var subject: String
    /// Full hashes, first parent first; empty for a root commit, and from a helper before
    /// `gitfiles.v1`, which does not send them.
    public var parents: [String]
    /// How many files the commit changes, when the log was asked for as a graph. Nil for
    /// merges, empty commits and older helpers.
    public var files: Int?

    public init(
        hash: String, short: String, author: String, date: Date, subject: String, parents: [String] = [], files: Int? = nil
    ) {
        self.hash = hash
        self.short = short
        self.author = author
        self.date = date
        self.subject = subject
        self.parents = parents
        self.files = files
    }

    private enum CodingKeys: String, CodingKey {
        case hash, short, author, date, subject, parents, files
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        hash = try container.decode(String.self, forKey: .hash)
        short = try container.decode(String.self, forKey: .short)
        author = try container.decode(String.self, forKey: .author)
        date = try container.decodeTimestamp(forKey: .date)
        subject = try container.decode(String.self, forKey: .subject)
        parents = try container.decodeIfPresent([String].self, forKey: .parents) ?? []
        files = try container.decodeIfPresent(Int.self, forKey: .files)
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(hash, forKey: .hash)
        try container.encode(short, forKey: .short)
        try container.encode(author, forKey: .author)
        try container.encodeTimestamp(date, forKey: .date)
        try container.encode(subject, forKey: .subject)
        if !parents.isEmpty { try container.encode(parents, forKey: .parents) }
        try container.encodeIfPresent(files, forKey: .files)
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
    /// `path` is relative to the root of the repository holding the session cwd, the form
    /// `GitStatus` paths take. Needs `gitwrite.v1`. Nil, not false, when unset.
    public var root: Bool?

    public init(id: String, path: String, ref: String? = nil, root: Bool = false) {
        self.id = id
        self.path = path
        self.ref = ref
        self.root = root ? true : nil
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
