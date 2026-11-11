#if DEBUG
import Foundation
import TogoKit

/// `-ui-demo-api`: what the website's screen captures show. Session 01 is the recorded OpenCode
/// session protocol/v1/replays/opencode.status.jsonl on togo-demo-status, a small service status
/// page ("Add an incident history timeline under the services…"), reviewed right after it:
/// four files modified and three new, as `git status` and `git diff` report them (the tree is
/// kept on togo-demo-status's branch demo/incidents; the demo machine's `DemoProject`, so the
/// captures and the in-app demo share one copy). The first machine reads
/// as the owner's laptop, and the new session sheet lists the agents the registry knows,
/// installed as on the owner's Mac, with ~/status first among recent folders.
enum DemoAPIFixture {
    static var enabled: Bool { UITestLaunch.current.demoAPI }

    static let session = "s-01"
    /// The session's line: the helper names a session after its folder.
    static let name = "status"
    static let agentName = "OpenCode"
    static let activity: String? = nil
    static let root = "/Users/sam/status"
    static let machine = "MacBook Pro"

    /// The status project and the session's changes: the same copy the demo machine plays.
    private static let project = DemoProject.load()

    static var status: GitStatus {
        let files = project?.status ?? []
        return GitStatus(
            repo: true, root: root, branch: "main", ahead: 0, behind: 0, truncated: false,
            files: files.filter { $0.index != "?" }.map(\.path).sorted().map { ReviewFixtures.file($0, ".", "M") }
                + files.filter { $0.index == "?" }.map(\.path).sorted().map { ReviewFixtures.file($0, "?", "?") }
        )
    }

    static var diffs: [String: String] { project?.diffs ?? [:] }

    /// The untracked files, as the review reads them whole.
    static var reads: [String: FsReadResult] {
        let project = project
        return (project?.status ?? []).filter { $0.index == "?" }.reduce(into: [:]) { result, file in
            guard let text = project?.work[file.path] else { return }
            result[file.path] = FsReadResult(path: file.path, size: text.utf8.count, encoding: .utf8, content: text, truncated: false)
        }
    }

    /// Ports and the preview still show the earlier recorded Claude Code session on the demo API
    /// (protocol/v1/replays/claude.page.jsonl, togo-demo-api's branch demo/home-page): the server
    /// it ran and the page it wrote at `/`. The website's fidelity baselines for them are that
    /// session's.
    /// Its compiled server run with node: Express on every address, as lsof reports it (`*:3000`).
    static let ports = [ListeningPort(port: 3000, address: "*", pid: 2944, command: "node")]

    /// What the API answers at `/`, byte for byte: the page the session wrote, 30 seconds after the
    /// server started.
    static let rootPage = """
        <!doctype html>
        <html lang="en">
        <head>
        <meta charset="utf-8">
        <meta name="viewport" content="width=device-width, initial-scale=1">
        <title>api</title>
        <style>
          body { font: 16px/1.5 system-ui, sans-serif; background: #f6f7f9; color: #1c1e21; display: grid; place-items: center; min-height: 100vh; margin: 0; }
          main { background: #fff; padding: 2rem 2.5rem; border-radius: 12px; box-shadow: 0 1px 4px rgb(0 0 0 / 12%); }
          h1 { margin: 0 0 1rem; font-size: 1.5rem; }
          dl { display: grid; grid-template-columns: auto 1fr; gap: .25rem 1.5rem; margin: 0; }
          dt { color: #6b7280; }
          dd { margin: 0; font-variant-numeric: tabular-nums; }
        </style>
        </head>
        <body>
        <main>
          <h1>api</h1>
          <dl>
            <dt>Version</dt><dd>0.1.0</dd>
            <dt>Uptime</dt><dd>30s</dd>
          </dl>
        </main>
        </body>
        </html>
        """

    static var rootResponse: String {
        "HTTP/1.1 200 OK\r\nX-Powered-By: Express\r\nContent-Type: text/html; charset=utf-8\r\n"
            + "Content-Length: \(rootPage.utf8.count)\r\nConnection: close\r\n\r\n" + rootPage
    }

    /// The registry's agents (helper/internal/agents), each installed as on the owner's Mac;
    /// Kiro CLI is not.
    static let agents: [AgentInfo] = [
        ("aider", "Aider"), ("amp", "Amp"), ("auggie", "Auggie"), ("claude", "Claude Code"), ("cline", "Cline"),
        ("codex", "Codex"), ("copilot", "Copilot CLI"), ("crush", "Crush"), ("cursor", "Cursor CLI"),
        ("droid", "Droid"), ("goose", "Goose"), ("kilo", "Kilo Code"), ("kimi", "Kimi Code"), ("kiro", "Kiro CLI"),
        ("vibe", "Mistral Vibe"), ("omp", "omp"), ("opencode", "OpenCode"), ("openhands", "OpenHands"), ("pi", "Pi"),
    ].map { AgentInfo(id: $0.0, name: $0.1, installed: $0.0 != "kiro") } + [AgentInfo(id: "shell", name: "Shell", installed: true)]

    static let recentDirs = [root, "/Users/sam/src/app", "/Users/sam/src/site", "/Users/sam/notes"]

    /// omp is the starred agent, so it leads the list and is chosen.
    static let favorites = ["omp"]
}
#endif
