#if DEBUG
import SwiftUI
import TogoKit

struct DebugSessionsView: View {
    let profile: HostProfile

    @State private var sessions: [Session] = []
    @State private var loading = false
    @State private var error: String?

    var body: some View {
        List {
            if let error {
                Section { Text(error).foregroundStyle(.red) }
            }
            Section {
                Button("New Claude session") { Task { await create() } }.disabled(loading)
            }
            Section("Sessions") {
                if sessions.isEmpty, !loading {
                    Text("No sessions on this host").foregroundStyle(.secondary)
                }
                ForEach(sessions) { session in
                    NavigationLink {
                        DebugTerminalScreen(profile: profile, session: session)
                    } label: {
                        VStack(alignment: .leading) {
                            Text(session.name)
                            Text("\(session.agent) · \(session.cwd)").font(.caption).foregroundStyle(.secondary)
                        }
                    }
                }
            }
        }
        .overlay { if loading { ProgressView() } }
        .navigationTitle(profile.name)
        .refreshable { await reload() }
        .task { await reload() }
    }

    private func reload() async {
        await run { helper in try await helper.listSessions() }
    }

    private func create() async {
        await run { helper in
            _ = try await helper.createSession(CreateSessionParams(agent: "claude"))
            return try await helper.listSessions()
        }
    }

    private func run(_ body: @escaping @Sendable (HelperClient) async throws -> [Session]) async {
        loading = true
        defer { loading = false }
        do {
            sessions = try await DebugComposition.shared.withHelper(profile, body)
            error = nil
        } catch {
            self.error = error.localizedDescription
        }
    }
}
#endif
