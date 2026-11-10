#if DEBUG
import Foundation
import TogoKit

/// Repository content for previews and snapshots: one of each kind of change, a diff per file,
/// a long history, a small tree and a few listening ports.
enum ReviewFixtures {
    static let bigDiffPath = "generated/table.json"
    static let now = Samples.now

    static func file(_ path: String, _ index: String, _ worktree: String, from orig: String? = nil) -> GitStatus.File {
        GitStatus.File(
            path: path, origPath: orig, index: index, worktree: worktree,
            fingerprint: worktree == "." ? nil : "blob:\(path.utf8.reduce(7) { ($0 &* 31 &+ Int($1)) % 1_000_003 })"
        )
    }

    static var status: GitStatus {
        GitStatus(
            repo: true, root: "/Users/sam/src/api", branch: "main", ahead: 2, behind: 0, truncated: false,
            files: [
                file("src/router.ts", "M", "."),
                file("src/handlers/items.ts", ".", "M"),
                file("src/handlers/health.ts", "A", "."),
                file("src/store.ts", "R", ".", from: "src/db.ts"),
                file("assets/logo.png", ".", "M"),
                file(bigDiffPath, ".", "M"),
                file("newpkg/main.go", "?", "?"),
                file("newpkg/util.go", "?", "?"),
                file("notes.txt", "?", "?"),
            ]
        )
    }

    /// Status of a repository with more changes than the helper lists: nothing may act on "all".
    static var truncatedStatus: GitStatus {
        var status = status
        status.truncated = true
        return status
    }

    static let diffs: [String: GitDiff] = [
        "src/router.ts": GitDiff(diff: routerDiff, truncated: false, binary: []),
        "src/handlers/items.ts": GitDiff(diff: itemsDiff, truncated: false, binary: []),
        "src/handlers/health.ts": GitDiff(diff: healthDiff, truncated: false, binary: []),
        "src/store.ts": GitDiff(diff: renameDiff, truncated: false, binary: []),
        "assets/logo.png": GitDiff(diff: "", truncated: false, binary: ["assets/logo.png"]),
        bigDiffPath: GitDiff(diff: bigDiff, truncated: true, binary: []),
        hiddenDiffPath: GitDiff(diff: hiddenDiff, truncated: false, binary: []),
    ]

    /// A change whose comment hides code behind direction overrides.
    static let hiddenDiffPath = "src/access.ts"
    static let hiddenDiff = """
    diff --git a/src/access.ts b/src/access.ts
    index 4c1d2e3..8f9a0b1 100644
    --- a/src/access.ts
    +++ b/src/access.ts
    @@ -3,4 +3,4 @@ export function canEdit(user: User) {
       const role = user.role
    -  if (role !== "admin") return false
    +  if (role !== "admin\u{202E} \u{2066}// Check admin\u{2069} \u{2066}") return false
       return true
     }

    """

    static let routerDiff = """
    diff --git a/src/router.ts b/src/router.ts
    index 3b18e51..9c0d2f4 100644
    --- a/src/router.ts
    +++ b/src/router.ts
    @@ -10,7 +10,8 @@ export function routes(app: App) {
       app.get("/items", list)
    -  app.get("/items/:id", find)
    +  app.get("/items/:id", validateId, find)
    +  app.get("/health", health)
       app.post("/items", create)
       app.delete("/items/:id", remove)
     }

    """

    static let itemsDiff = """
    diff --git a/src/handlers/items.ts b/src/handlers/items.ts
    index 41d9a07..c7f1b2e 100644
    --- a/src/handlers/items.ts
    +++ b/src/handlers/items.ts
    @@ -3,9 +3,10 @@ import { store } from "../store"
     export async function find(req: Request, res: Response) {
    -  const item = await store.find(req.params.id)
    -  res.json(item)
    +  const item = await store.find(req.params.id)
    +  if (!item) return res.status(404).end()
    +  res.json(item)
     }
    \\ No newline at end of file

    """

    static let healthDiff = """
    diff --git a/src/handlers/health.ts b/src/handlers/health.ts
    new file mode 100644
    index 0000000..5d2a8c1
    --- /dev/null
    +++ b/src/handlers/health.ts
    @@ -0,0 +1,3 @@
    +export function health(_: Request, res: Response) {
    +  res.json({ ok: true })
    +}

    """

    static let renameDiff = """
    diff --git a/src/db.ts b/src/store.ts
    similarity index 92%
    rename from src/db.ts
    rename to src/store.ts
    index 7a3c9d0..e41b6f2 100644
    --- a/src/db.ts
    +++ b/src/store.ts
    @@ -1,4 +1,4 @@
    -export const db = new Map<string, Item>()
    +export const store = new Map<string, Item>()
     export function find(id: string) {
    -  return db.get(id)
    +  return store.get(id)
     }

    """

    /// A single hunk of 10,000 changed lines, cut the way the helper cuts a diff at its size limit.
    static let bigDiff: String = {
        var lines = [
            "diff --git a/\(bigDiffPath) b/\(bigDiffPath)", "index 1111111..2222222 100644",
            "--- a/\(bigDiffPath)", "+++ b/\(bigDiffPath)", "@@ -1,5000 +1,5000 @@",
        ]
        lines.reserveCapacity(10_005)
        for n in 0..<5_000 {
            lines.append("-  \"row\(n)\": { \"value\": \(n), \"label\": \"item \(n)\" },")
            lines.append("+  \"row\(n)\": { \"value\": \(n + 1), \"label\": \"item \(n + 1)\" },")
        }
        return lines.joined(separator: "\n") + "\n"
    }()

    /// `bigDiff`'s shape with minified lines 2,000 columns wide, as a bundled build changes.
    static let wideDiff: String = {
        var lines = [
            "diff --git a/\(bigDiffPath) b/\(bigDiffPath)", "index 1111111..2222222 100644",
            "--- a/\(bigDiffPath)", "+++ b/\(bigDiffPath)", "@@ -1,500 +1,500 @@",
        ]
        func row(_ n: Int, _ value: Int) -> String {
            var line = ""
            var cell = 0
            while line.utf8.count < 2_000 {
                line += "\"r\(n)c\(cell)\":{\"v\":\(value + cell),\"t\":\"item\"},"
                cell += 1
            }
            return String(line.prefix(2_000))
        }
        for n in 0..<500 {
            lines.append("-" + row(n, n))
            lines.append("+" + row(n, n + 1))
        }
        return lines.joined(separator: "\n") + "\n"
    }()

    static let log = makeLog(60)

    /// `count` commits, newest first, each with its own 40-digit hash. Each is the child of
    /// the next, except that the second merges a side branch: the third and fourth both come
    /// from the fifth, so the graph shows two lanes meeting.
    static func makeLog(_ count: Int) -> [GitCommit] {
        (0..<count).map { n in
            var entry = Self.commit(n)
            let parents: [Int] = switch n {
            case 1 where count > 4: [2, 3]
            case 2 where count > 4: [4]
            default: n + 1 < count ? [n + 1] : []
            }
            entry.parents = parents.map { Self.hash($0) }
            entry.files = parents.count > 1 ? nil : n % 4 + 1
            return entry
        }
    }

    /// Every file path in `tree`, as `git.files` lists them.
    static let allFiles: [String] = {
        func walk(_ folder: String) -> [String] {
            (tree[folder] ?? []).flatMap { entry -> [String] in
                let path = folder.isEmpty ? entry.name : folder + "/" + entry.name
                return entry.dir ? walk(path) : [path]
            }
        }
        return walk("").sorted()
    }()

    private static func hash(_ n: Int) -> String {
        var hash = ""
        for part in 0..<5 {
            var z = UInt64(n * 5 + part + 1) &* 0x9E37_79B9_7F4A_7C15
            z = (z ^ (z >> 30)) &* 0xBF58_476D_1CE4_E5B9
            z = (z ^ (z >> 27)) &* 0x94D0_49BB_1331_11EB
            hash += String(format: "%08x", UInt32(truncatingIfNeeded: z ^ (z >> 31)))
        }
        return hash
    }

    private static func commit(_ n: Int) -> GitCommit {
        let hash = Self.hash(n)
        let subjects = [
            "Return 404 for unknown ids", "Validate ids before the lookup", "Add the health route",
            "Move the store behind a module", "Fix flaky pagination test", "Bump dependencies",
        ]
        return GitCommit(
            hash: hash, short: String(hash.prefix(7)), author: n % 3 == 0 ? "Claude" : "Sam Rivera",
            date: now.addingTimeInterval(-Double(n) * 5_400), subject: subjects[n % subjects.count]
        )
    }

    static let branches = GitBranches(
        current: "main",
        branches: [
            .init(name: "main", upstream: "origin/main", current: true),
            .init(name: "fix/ids", upstream: nil, current: false),
            .init(name: "release/1.4", upstream: "origin/release/1.4", current: false),
        ]
    )

    static let tree: [String: [DirEntry]] = [
        "": [
            DirEntry(name: "assets", dir: true), DirEntry(name: "generated", dir: true),
            DirEntry(name: "newpkg", dir: true), DirEntry(name: "src", dir: true),
            DirEntry(name: "README.md", dir: false), DirEntry(name: "package.json", dir: false),
            DirEntry(name: "notes.txt", dir: false),
        ],
        "src": [
            DirEntry(name: "handlers", dir: true), DirEntry(name: "router.ts", dir: false),
            DirEntry(name: "store.ts", dir: false),
        ],
        "src/handlers": [DirEntry(name: "health.ts", dir: false), DirEntry(name: "items.ts", dir: false)],
        "newpkg": [DirEntry(name: "main.go", dir: false), DirEntry(name: "util.go", dir: false)],
    ]

    static let files: [String: String] = [
        "src/router.ts": """
        import { App } from "./app"
        import { create, find, list, remove } from "./handlers/items"
        import { health } from "./handlers/health"

        // Every route the API serves.
        export function routes(app: App) {
          app.get("/items", list)
          app.get("/items/:id", validateId, find)
          app.get("/health", health)
          app.post("/items", create)
          app.delete("/items/:id", remove)
        }

        """,
        "README.md": "# api\n\nA small items API.\n\n## Run\n\n```sh\nnpm start\n```\n",
        "notes.txt": "check the 404 path\nthen the pagination test\n",
    ]

    static let ports = [
        ListeningPort(port: 3000, address: "127.0.0.1", pid: 4121, command: "node"),
        ListeningPort(port: 5173, address: "0.0.0.0", pid: 4188, command: "vite"),
        ListeningPort(port: 8080, address: "::1", pid: 4302, command: "python3"),
    ]
}
#endif
