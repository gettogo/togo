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
            PushPayload(
                kind: .needsYou, sessionId: "s_1", bib: "04", agent: "claude", host: "studio",
                title: "Allow this command to run?", body: "", requestId: "r_1",
                options: [
                    PushOption(n: 1, label: "Yes", role: "approve"),
                    PushOption(n: 2, label: "Yes, and don't ask again", role: "approve_always"),
                    PushOption(n: 3, label: "No", role: "deny"),
                ],
                subject: PushSubject(kind: "command", text: "npm run test -- --filter api/sessions"), ts: ts - 90_000
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
        WatchSession(hostName: "Studio", bib: "04", agentName: "Claude Code", status: "needs_you", activity: nil),
        WatchSession(hostName: "Studio", bib: "17", agentName: "Codex", status: "needs_you", activity: nil),
        WatchSession(
            hostName: "Studio", bib: "21", agentName: "Claude Code", status: "working", activity: "Running the test suite"
        ),
        WatchSession(hostName: "Build box", bib: "03", agentName: "OMP", status: "done", activity: nil),
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
