import SwiftUI
import TogoPush

/// Requests waiting on the user, oldest first.
struct RequestsView: View {
    let store: WatchStore

    var body: some View {
        List {
            ForEach(store.requests) { request in
                NavigationLink(value: request.requestId) {
                    RequestRow(request: request)
                }
            }
        }
        .overlay {
            if store.requests.isEmpty {
                ContentUnavailableView(WatchCopy.nothingWaiting, systemImage: "checkmark.circle")
            }
        }
        .animation(.default, value: store.requests.map(\.requestId))
        .navigationTitle(WatchCopy.waiting)
        .navigationDestination(for: String.self) { id in
            RequestDetailView(store: store, requestId: id)
        }
    }
}

struct RequestRow: View {
    let request: PendingRequest

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            RequestHeader(request: request)
            Text(request.title)
                .font(.body)
                .lineLimit(2)
        }
        .padding(.vertical, 2)
    }
}

/// "04 Claude Code": the bib in mono, then the agent.
struct RequestHeader: View {
    let request: PendingRequest

    var body: some View {
        HStack(spacing: 6) {
            if let bib = request.bib, !bib.isEmpty {
                Text(PromptText.clean(bib, maxLines: 1))
                    .font(.system(.footnote, design: .monospaced).weight(.semibold))
            }
            if let agent = request.agent, !agent.isEmpty {
                Text(AgentName.display(agent))
                    .font(.footnote)
                    .lineLimit(1)
            }
        }
        .foregroundStyle(.secondary)
    }
}
