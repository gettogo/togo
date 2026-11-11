import Foundation
import TogoKit

/// The demo session's project: a small status page in git (`demo-project.json`, built by
/// scripts/demo-fixture.py). `head` is the last commit; `work` is what the recorded session
/// left in the working tree. Only files the session has written so far show as changed.
struct DemoProject: Decodable, Sendable {
    struct StatusFile: Decodable, Sendable {
        var path: String
        var index: String
        var worktree: String
    }

    struct Commit: Decodable, Sendable {
        var hash: String
        var short: String
        var subject: String
        /// Seconds before the demo started.
        var age: Double
        var parents: [String]
        var paths: [String]
        var diffs: [String: String]
    }

    var root: String
    var branch: String
    var author: String
    var head: [String: String]
    var work: [String: String]
    var status: [StatusFile]
    var diffs: [String: String]
    var commits: [Commit]

    static func load(bundle: Bundle = .main) -> DemoProject? {
        guard let url = bundle.url(forResource: "demo-project", withExtension: "json"),
              let data = try? Data(contentsOf: url) else { return nil }
        return try? JSONDecoder().decode(DemoProject.self, from: data)
    }

    /// Every file in the tree once `written` paths exist, sorted.
    func paths(written: Set<String>) -> [String] {
        Set(head.keys).union(work.keys.filter { written.contains($0) }).sorted()
    }

    /// The text of `path` with `written` applied.
    func text(_ path: String, written: Set<String>) -> String? {
        written.contains(path) ? work[path] ?? head[path] : head[path]
    }

    /// The folder's entries, folders first, as `fs.list` gives them.
    func entries(in folder: String, written: Set<String>) -> [DirEntry] {
        let prefix = folder.isEmpty ? "" : folder + "/"
        var dirs = Set<String>(), files: [DirEntry] = []
        for path in paths(written: written) where path.hasPrefix(prefix) {
            let rest = path.dropFirst(prefix.count)
            if let slash = rest.firstIndex(of: "/") {
                dirs.insert(String(rest[..<slash]))
            } else {
                files.append(DirEntry(name: String(rest), dir: false, kind: .file, size: text(path, written: written)?.utf8.count))
            }
        }
        return dirs.sorted().map { DirEntry(name: $0, dir: true, kind: .dir) } + files
    }

    /// `git diff` for a file the session added: every line new.
    static func addedDiff(_ path: String, _ text: String) -> String {
        var lines = text.components(separatedBy: "\n")
        if lines.last == "" { lines.removeLast() }
        let body = lines.map { "+" + $0 }.joined(separator: "\n")
        return """
            diff --git a/\(path) b/\(path)
            new file mode 100644
            --- /dev/null
            +++ b/\(path)
            @@ -0,0 +1,\(lines.count) @@
            \(body)

            """
    }
}

/// The review screens over `DemoProject`. Reads follow the script: a file shows as changed once
/// the agent has written it. Stage, unstage, discard and commit change only this object's
/// memory, so the screens work as they would and nothing leaves the phone. A preview of a port
/// is not offered: nothing runs behind it.
@MainActor final class DemoReviewServices: ReviewServices {
    struct Unavailable: LocalizedError {
        var errorDescription: String? {
            String(localized: "tutorial.demo.preview", defaultValue: "Previews need a real machine. Add one to open its servers here.")
        }
    }

    let project: DemoProject
    /// Where the project is on the machine, as git status names it.
    let root: String
    /// The paths the agent has written so far.
    private let written: () -> Set<String>
    private let started: Date
    private var staged: Set<String> = []
    private var discarded: Set<String> = []
    private var committed: Set<String> = []
    private var local: [(commit: GitCommit, diffs: [String: String])] = []
    /// What the screens asked to change, for tests.
    private(set) var writes: [String] = []

    init(project: DemoProject, root: String? = nil, started: Date = .now, written: @escaping () -> Set<String>) {
        self.project = project
        self.root = root ?? project.root
        self.started = started
        self.written = written
    }

    private var shown: Set<String> {
        written().subtracting(discarded)
    }

    private var current: GitStatus {
        let shown = shown
        let files = project.status.filter { shown.contains($0.path) && !committed.contains($0.path) }.map { file in
            let added = file.index == "?"
            return staged.contains(file.path)
                ? GitStatus.File(path: file.path, origPath: nil, index: added ? "A" : file.worktree, worktree: ".")
                : GitStatus.File(path: file.path, origPath: nil, index: file.index, worktree: file.worktree)
        }
        return GitStatus(repo: true, root: root, branch: project.branch, ahead: local.count, behind: 0, files: files)
    }

    func status(_ s: ReviewSubject) async throws -> GitStatus { current }

    func diff(_ s: ReviewSubject, _ source: DiffSource) async throws -> GitDiff {
        let text: String
        switch source {
        case .worktree(let path), .staged(let path), .untracked(let path):
            text = project.diffs[path] ?? project.work[path].map { DemoProject.addedDiff(path, $0) } ?? ""
        case .commit(let hash, let path):
            let diffs = local.first { $0.commit.hash == hash }?.diffs ?? project.commits.first { $0.hash == hash }?.diffs ?? [:]
            text = path.map { diffs[$0] ?? "" } ?? diffs.keys.sorted().compactMap { diffs[$0] }.joined()
        }
        return GitDiff(diff: text, truncated: false, binary: [])
    }

    func read(_ s: ReviewSubject, path: String, ref: String?, base: ReviewPathBase) async throws -> FsReadResult {
        let path = Self.relative(path, root: root)
        let text = (ref == nil ? project.text(path, written: shown) : project.head[path]) ?? ""
        return FsReadResult(path: path, size: text.utf8.count, encoding: .utf8, content: text, truncated: false)
    }

    func listDir(_ s: ReviewSubject, path: String, ref: String?, base: ReviewPathBase) async throws -> ListDirResult {
        let path = Self.relative(path, root: root)
        return ListDirResult(path: path, entries: project.entries(in: path, written: ref == nil ? shown : []))
    }

    func files(_ s: ReviewSubject, ref: String?) async throws -> GitFiles {
        GitFiles(paths: project.paths(written: ref == nil ? shown : []), truncated: false)
    }

    func log(_ s: ReviewSubject, ref: String?, limit: Int) async throws -> [GitCommit] {
        let recorded = project.commits.map { commit in
            GitCommit(
                hash: commit.hash, short: commit.short, author: project.author,
                date: started.addingTimeInterval(-commit.age), subject: commit.subject,
                parents: commit.parents, files: commit.paths.count
            )
        }
        return Array((local.map(\.commit).reversed() + recorded).prefix(limit))
    }

    func branches(_ s: ReviewSubject) async throws -> GitBranches {
        GitBranches(current: project.branch, branches: [GitBranches.Branch(name: project.branch, upstream: nil, current: true)])
    }

    func stage(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus {
        writes.append("stage")
        staged.formUnion(paths)
        return current
    }

    func unstage(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus {
        writes.append("unstage")
        staged.subtract(paths)
        return current
    }

    func discard(_ s: ReviewSubject, paths: [String], seen: [String: GitSeen]) async throws -> GitStatus {
        writes.append("discard")
        discarded.formUnion(paths)
        staged.subtract(paths)
        return current
    }

    func commit(_ s: ReviewSubject, message: String, amend: Bool, head: String?) async throws -> GitCommitResult {
        writes.append("commit")
        let paths = current.files.filter { $0.index != "." && $0.index != "?" }.map(\.path)
        guard !amend else { throw DemoServices.ReadOnly() }
        guard !paths.isEmpty else { throw HelperError.conflict(GitConflict.nothingStaged.rawValue) }
        let parent = local.last?.commit.hash ?? project.commits.first?.hash
        let hash = Self.hash(message + "\(local.count)")
        let subject = message.split(separator: "\n", omittingEmptySubsequences: true).first.map(String.init) ?? message
        let diffs = Dictionary(uniqueKeysWithValues: paths.map { path in
            (path, project.diffs[path] ?? project.work[path].map { DemoProject.addedDiff(path, $0) } ?? "")
        })
        local.append((
            GitCommit(hash: hash, short: String(hash.prefix(7)), author: project.author, date: .now, subject: subject,
                      parents: parent.map { [$0] } ?? [], files: paths.count),
            diffs
        ))
        committed.formUnion(paths)
        staged.subtract(paths)
        return GitCommitResult(hash: hash, short: String(hash.prefix(7)))
    }

    /// The status page's own server, as `npm start` runs it.
    func ports(_ s: ReviewSubject) async throws -> [ListeningPort] {
        [ListeningPort(port: 3000, address: "*", pid: 4211, command: "node")]
    }

    func forward(_ s: ReviewSubject, port: ListeningPort) async throws -> ForwardInfo {
        writes.append("forward")
        throw Unavailable()
    }

    func stopForward(localPort: Int) async {}

    func forwardEvents() -> AsyncStream<(localPort: Int, message: String)> {
        AsyncStream { $0.finish() }
    }

    /// A path under the project's root, as the review names it.
    static func relative(_ path: String, root: String) -> String {
        var path = path
        for prefix in [root + "/", root, "~/status/", "~/status"] where path.hasPrefix(prefix) {
            path = String(path.dropFirst(prefix.count))
            break
        }
        return path.hasPrefix("/") ? String(path.dropFirst()) : path
    }

    /// A stable 40-digit hex name for a commit made in the demo.
    private static func hash(_ text: String) -> String {
        var value: UInt64 = 0xcbf29ce484222325
        var out = ""
        for round in 0..<3 {
            for byte in (text + "\(round)").utf8 {
                value ^= UInt64(byte)
                value = value &* 0x100000001b3
            }
            out += String(format: "%016llx", value)
        }
        return String(out.prefix(40))
    }
}
