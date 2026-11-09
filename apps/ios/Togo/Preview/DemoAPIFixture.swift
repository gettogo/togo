#if DEBUG
import Foundation
import TogoKit

/// `-ui-demo-api`: what the website's screen captures show. Session 01 reviews the demo API
/// (`togo-demo-api`) right after the recorded Claude Code session
/// protocol/v1/replays/claude.page.jsonl ("Add a small HTML page at / that shows the service
/// name, the version from package.json and the uptime…"): `src/app.ts` modified, and
/// `src/routes/home.ts` and `test/home.test.ts` new, as `git status` and `git diff` report them
/// (kept on togo-demo-api's branch demo/home-page). Its server listens on its default port and
/// answers `/` with the page that session wrote. The new session sheet lists the agents the
/// registry knows, installed as on the owner's Mac.
enum DemoAPIFixture {
    static var enabled: Bool { UITestLaunch.current.demoAPI }

    static let session = "s-01"
    static let root = "/Users/sam/Dev/togo-demo-api"

    static var status: GitStatus {
        GitStatus(
            repo: true, root: root, branch: "main", ahead: 0, behind: 0, truncated: false,
            files: [
                ReviewFixtures.file("src/app.ts", ".", "M"),
                ReviewFixtures.file("src/routes/home.ts", "?", "?"),
                ReviewFixtures.file("test/home.test.ts", "?", "?"),
            ]
        )
    }

    static let diffs: [String: String] = [
        "src/app.ts": """
            diff --git a/src/app.ts b/src/app.ts
            index e81e3e5..584e522 100644
            --- a/src/app.ts
            +++ b/src/app.ts
            @@ -1,10 +1,12 @@
             import express from "express";
            +import { home } from "./routes/home.js";
             import { health } from "./routes/health.js";
             import { login } from "./routes/login.js";
            \u{20}
             export function createApp() {
               const app = express();
               app.use(express.json());
            +  app.get("/", home);
               app.get("/health", health);
               app.post("/login", login);
               return app;

            """,
    ]

    /// The compiled server run with node: Express on every address, as lsof reports it (`*:3000`).
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

    /// Claude Code is the starred agent, so it leads the list and is chosen.
    static let favorites = ["claude"]
}
#endif
