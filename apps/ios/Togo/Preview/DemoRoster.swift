#if DEBUG
import Foundation
import TogoKit

/// A busy, credible roster for the website's reference captures (`-ui-demo-roster`): a Mac and
/// a Linux box, nine sessions across eight agents in different projects, two that need you,
/// three working, two done and two idle.
extension Samples {
    static let demoStudio = HostID(uuidString: "6F1C2D3E-0000-4000-8000-0000000000D1")!
    static let demoBuildBox = HostID(uuidString: "6F1C2D3E-0000-4000-8000-0000000000D2")!

    static let demoHosts = [
        HostRecordView(id: demoStudio, name: "Studio", symbol: .desktop, address: "studio.local"),
        HostRecordView(id: demoBuildBox, name: "build-box", symbol: .server, address: "build-box.tail1234.ts.net"),
    ]

    /// One session with a catalog agent id, so the line shows the agent's own mark.
    static func demoRow(
        _ hostID: HostID, _ bib: String, _ name: String, agent: String, _ status: SessionStatus,
        _ activity: String?, cwd: String, history: [Double]
    ) -> SessionRow {
        SessionRow(
            id: "s-\(bib)", hostID: hostID, bib: bib, name: name, agent: agent,
            agentName: AgentCatalog.name(agent) ?? agent, status: status, activity: activity, cwd: cwd,
            muted: false, managed: true, meta: SessionMeta(), statusSince: now, activityHistory: history
        )
    }

    static var demoControlCenter: ControlCenterState {
        let busy: [Double] = [0.1, 0.2, 0.15, 0.3, 0.5, 0.4, 0.6, 0.8, 0.7, 0.9, 0.6, 0.75, 0.85, 0.7, 0.9, 1]
        let quiet: [Double] = [0.4, 0.6, 0.5, 0.3, 0.2, 0.1, 0.05, 0, 0, 0, 0, 0, 0, 0, 0, 0]
        let calm: [Double] = [0.3, 0.5, 0.6, 0.4, 0.5, 0.7, 0.6, 0.4, 0.3, 0.2, 0.2, 0.1, 0.1, 0.05, 0.05, 0.1]
        return ControlCenterState(
            machines: [
                MachineSection(
                    id: demoStudio, name: "Studio", symbol: .desktop, reachability: .online,
                    helper: .present(version: "0.3.0", live: true),
                    sessions: [
                        demoRow(demoStudio, "01", "api", agent: "opencode", .needsYou, "Wants to run npm test",
                                cwd: "/Users/sam/code/api", history: calm),
                        demoRow(demoStudio, "02", "web", agent: "codex", .working, "Editing src/checkout/cart.tsx",
                                cwd: "/Users/sam/code/web", history: busy),
                        demoRow(demoStudio, "03", "ios", agent: "claude", .working, "Running xcodebuild test",
                                cwd: "/Users/sam/code/ios", history: busy.reversed()),
                        demoRow(demoStudio, "04", "docs", agent: "goose", .done, "Rewrote the setup guide",
                                cwd: "/Users/sam/code/docs", history: quiet),
                        demoRow(demoStudio, "05", "site", agent: "kimi", .idle, nil,
                                cwd: "/Users/sam/code/site", history: []),
                    ],
                    lastSeen: now
                ),
                MachineSection(
                    id: demoBuildBox, name: "build-box", symbol: .server, reachability: .online,
                    helper: .present(version: "0.3.0", live: true),
                    sessions: [
                        demoRow(demoBuildBox, "06", "infra", agent: "cline", .needsYou, "Wants to run terraform apply",
                                cwd: "/home/sam/infra", history: calm.reversed()),
                        demoRow(demoBuildBox, "07", "worker", agent: "omp", .working, "Running go test ./...",
                                cwd: "/home/sam/worker", history: busy),
                        demoRow(demoBuildBox, "08", "db", agent: "pi", .done, "Applied 3 migrations",
                                cwd: "/home/sam/db", history: quiet),
                        demoRow(demoBuildBox, "09", "ci", agent: "claude", .idle, nil,
                                cwd: "/home/sam/ci", history: []),
                    ],
                    lastSeen: now
                ),
            ],
            loaded: true
        )
    }
}
#endif
