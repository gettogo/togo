import SwiftUI
import TogoPush

/// Every session the phone last saw, by machine.
struct SessionsView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let store: WatchStore
    /// The sessions on screen. They follow the store's: a session arriving or leaving, or an
    /// activity line appearing, moves the list in one `resize` transaction; new words for the
    /// same rows land in place.
    @State private var shown: [WatchSession]?

    private var sessions: [WatchSession] { shown ?? store.sessions }

    private var machines: [(name: String, sessions: [WatchSession])] {
        var order: [String] = []
        var grouped: [String: [WatchSession]] = [:]
        for session in sessions {
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
                        ForEach(machine.sessions, id: \.key) { session in
                            SessionRow(session: session)
                            if session.acceptsReply, let machine = session.machineId, let id = session.sessionId {
                                ReplyLink(
                                    target: .prompt(machineId: machine, sessionId: id),
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
                                .fixedSize(horizontal: false, vertical: true)
                        }
                    }
                }
            }
            .overlay {
                if sessions.isEmpty {
                    // motion: state change — the last session left; the empty state takes its place
                    ContentUnavailableView(WatchCopy.noSessions, systemImage: "iphone")
                        .transition(Motion.swap(reduce: reduceMotion))
                }
            }
        }
        .navigationTitle(WatchCopy.sessions)
        .onChange(of: store.sessions, initial: true) { _, new in
            guard let old = shown, old.map(\.shape) != new.map(\.shape) else {
                shown = new
                return
            }
            // motion: preventing a jump — sessions arriving or leaving open and close space
            withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { shown = new }
        }
    }
}

private extension WatchSession {
    var key: String { WatchHome.key(self) }

    /// What changes the list's layout: which sessions there are and whether each has an
    /// activity line.
    var shape: String { key + (activity?.isEmpty == false ? "+" : "") }
}

private struct SessionRow: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let session: WatchSession

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            RosterLine(line: WatchHome.lines([session])[0])
            if let activity = session.activity, !activity.isEmpty {
                Text(PromptText.clean(activity, maxLines: 1))
                    .font(.system(.footnote, design: .monospaced))
                    .foregroundStyle(.secondary)
                    // trunc-ok: the agent's live last action, one line on the wrist; the phone shows it in full
                    .lineLimit(1)
                    // The last action can quote a command: hidden while the wrist is down.
                    .privacySensitive()
                    // motion: preventing a jump — the row grows for the agent's last action, by opacity
                    .transition(Motion.arrive(reduce: reduceMotion))
            }
        }
        .padding(.vertical, 2)
    }
}
