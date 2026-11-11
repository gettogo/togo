#if DEBUG
import SwiftUI
import TogoPush

/// Fixed screens for screenshots: launch with `-fixture <name>`. Debug builds only; the data
/// lives in a temporary directory, never the app group.
enum WatchFixtures {
    static var name: String? { UserDefaults.standard.string(forKey: "fixture") }

    private struct Sender: AnswerSending {
        var fails: Bool
        func send(requestId: String, decision: String, pairing: Pairing) async throws {
            if fails { throw AnswerError.offline }
        }
    }

    @MainActor static func root(_ name: String) -> some View {
        let now = Date()
        let directory = FileManager.default.temporaryDirectory.appending(path: "fixtures-\(UUID().uuidString)")
        let pending = PendingStore(directory: directory, clock: { Date() })
        let pairings = PairingStore(directory: directory)
        pairings.put(Pairing(
            hostId: "h_studio", hostKey: "", relayURL: URL(string: "https://relay.example")!, machineName: "Studio",
            appHostID: nil, pairedAt: now, tokenHash: "", liveActivityCommand: false
        ))
        let store = WatchStore(dependencies: .init(
            pending: pending, pairings: pairings, sender: Sender(fails: name == "detail-failed"),
            reloadComplication: {}
        ))
        let contextAge: TimeInterval = name == "sessions-asof" ? 720 : 20
        if !name.hasSuffix("empty") {
            for request in requests { store.ingest(request, hostId: "h_studio") }
            store.apply(context: WatchContext(
                sessions: sessions, closedRequestIds: [], at: now.addingTimeInterval(-contextAge)
            ))
        }
        return FixtureRoot(name: name, store: store)
    }

    private static var requests: [PushPayload] {
        let ts = Int64(Date().timeIntervalSince1970 * 1000)
        return [
            // The recorded OpenCode session on the status page, asking to run its checks: a
            // permission as the helper offers it, Allow once and Deny.
            PushPayload(
                kind: .needsYou, sessionId: "s_1", bib: "01", agent: "opencode", host: "studio",
                title: "Allow shell: npm run typecheck, npm test", body: "", requestId: "r_1",
                options: [
                    PushOption(n: 1, label: "Allow once", role: "approve"),
                    PushOption(n: 2, label: "Deny", role: "deny"),
                ],
                ts: ts - 90_000, sessionName: "status"
            ),
            PushPayload(
                kind: .needsYou, sessionId: "s_2", bib: "17", agent: "codex", host: "studio",
                title: "Apply the change to src/server/routes.ts?", body: "", requestId: "r_2",
                options: [PushOption(n: 1, label: "No, tell Codex what to do", role: "deny")],
                subject: PushSubject(kind: "file", text: "src/server/routes.ts"), ts: ts - 30_000
            ),
        ]
    }

    private static let sessions = [
        WatchSession(hostName: "Studio", bib: "01", agentName: "OpenCode", status: "needs_you", activity: nil),
        WatchSession(hostName: "Studio", bib: "17", agentName: "Codex", status: "needs_you", activity: nil),
        WatchSession(hostName: "Studio", bib: "07", agentName: "omp", status: "working", activity: "Running go test ./..."),
        WatchSession(hostName: "Build box", bib: "03", agentName: "Pi", status: "done", activity: nil),
        WatchSession(hostName: "Build box", bib: "08", agentName: "Codex", status: "idle", activity: nil),
    ]
}

private struct FixtureRoot: View {
    let name: String
    let store: WatchStore

    var body: some View {
        NavigationStack {
            Group {
                switch name {
                case "requests", "requests-empty":
                    RequestsView(store: store)
                case "detail", "detail-approved", "detail-failed":
                    RequestDetailView(store: store, requestId: "r_1")
                        .task {
                            guard name != "detail" else { return }
                            await store.answer("r_1", name == "detail-approved" ? "approve" : "deny")
                        }
                case "detail-deny-only":
                    RequestDetailView(store: store, requestId: "r_2")
                default:
                    SessionsView(store: store)
                }
            }
            .watchRoutes(store: store)
        }
    }
}
#endif
