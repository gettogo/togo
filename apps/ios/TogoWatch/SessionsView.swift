import SwiftUI
import TogoPush

/// Every session the phone last saw, by machine.
struct SessionsView: View {
    let store: WatchStore

    private var machines: [(name: String, sessions: [WatchSession])] {
        var order: [String] = []
        var grouped: [String: [WatchSession]] = [:]
        for session in store.sessions {
            if grouped[session.hostName] == nil { order.append(session.hostName) }
            grouped[session.hostName, default: []].append(session)
        }
        return order.map { ($0, grouped[$0] ?? []) }
    }

    var body: some View {
        TimelineView(.everyMinute) { timeline in
            let asOf = store.asOf(at: timeline.date)
            List {
                ForEach(machines, id: \.name) { machine in
                    Section {
                        ForEach(Array(machine.sessions.enumerated()), id: \.offset) { _, session in
                            SessionRow(session: session)
                            if session.acceptsReply, let machine = session.machineId, let id = session.sessionId {
                                ReplyLink(
                                    store: store, target: .prompt(machineId: machine, sessionId: id),
                                    title: "\(session.bib) \(session.agentName)"
                                )
                            }
                        }
                    } header: {
                        VStack(alignment: .leading, spacing: 4) {
                            if let asOf, machine.name == machines.first?.name {
                                Text(WatchCopy.asOf(asOf))
                                    .font(.footnote)
                                    .foregroundStyle(.secondary)
                            }
                            Text(PromptText.clean(machine.name, maxLines: 1))
                                .lineLimit(1)
                                .truncationMode(.middle)
                        }
                    }
                }
            }
            .overlay {
                if store.sessions.isEmpty {
                    ContentUnavailableView(WatchCopy.noSessions, systemImage: "iphone")
                }
            }
        }
        .navigationTitle(WatchCopy.sessions)
    }
}

private struct SessionRow: View {
    let session: WatchSession

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 6) {
                Text(PromptText.clean(session.bib, maxLines: 1))
                    .font(.system(.footnote, design: .monospaced).weight(.semibold))
                    .lineLimit(1)
                    .truncationMode(.middle)
                Text(PromptText.clean(session.agentName, maxLines: 1))
                    .font(.footnote)
                    .lineLimit(1)
            }
            .foregroundStyle(.secondary)
            Text(WatchCopy.status(session))
                .font(.body)
            if let activity = session.activity, !activity.isEmpty {
                Text(PromptText.clean(activity, maxLines: 1))
                    .font(.footnote)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
        }
        .padding(.vertical, 2)
    }
}
