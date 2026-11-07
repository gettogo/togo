import Foundation
import TogoKit

/// Turns what a machine's monitor reports into what the control center shows.
///
/// The helper reports a session's status but not when it last changed, so the mapper keeps
/// that itself: a row's `statusSince` moves only when its status does.
struct SnapshotMapper {
    /// Display names by agent id for each machine, from its `agents.list`.
    var agentNames: [HostID: [String: String]] = [:]
    private var seen: [TabID: (status: SessionStatus, since: Date)] = [:]

    mutating func forget(_ hostID: HostID) {
        agentNames[hostID] = nil
        seen = seen.filter { $0.key.hostID != hostID }
    }

    mutating func map(_ snapshot: HostSnapshot, noHelper: Bool, wake: WakeTarget? = nil, now: Date) -> HostViewSnapshot {
        let hostID = snapshot.hostID
        let names = agentNames[hostID] ?? [:]
        let rows = snapshot.sessions.map { session in
            row(for: session, hostID: hostID, agentName: Self.agentName(session.agent, names: names), now: now)
        }
        if snapshot.reach == .online {
            let listed = Set(rows.map { TabID(hostID: hostID, sessionID: $0.id) })
            seen = seen.filter { $0.key.hostID != hostID || listed.contains($0.key) }
        }
        let bibs = Dictionary(rows.map { ($0.id, $0.bib) }, uniquingKeysWith: { first, _ in first })
        let cards = snapshot.approvals.map { approval in
            RequestCard(
                id: approval.requestId, hostID: hostID, sessionID: approval.sessionId,
                bib: approval.bib ?? approval.sessionId.flatMap { bibs[$0] ?? nil },
                agentName: Self.agentName(approval.agent, names: names), title: approval.title,
                detail: approval.detail, options: approval.options, allowText: approval.allowText, phase: .open,
                signIn: approval.signIn
            )
        }
        return HostViewSnapshot(
            hostID: hostID, reachability: Self.reachability(snapshot, now: now),
            helper: Self.helper(snapshot, noHelper: noHelper), sessions: rows, requests: cards, at: now,
            capabilities: snapshot.capabilities, awake: snapshot.awake, wake: wake,
            closed: Dictionary(
                snapshot.closed.map { ($0.requestId, Self.outcome($0.resolution)) }, uniquingKeysWith: { _, last in last }
            )
        )
    }

    private mutating func row(for session: Session, hostID: HostID, agentName: String, now: Date) -> SessionRow {
        let key = TabID(hostID: hostID, sessionID: session.id)
        let since: Date
        if let previous = seen[key], previous.status == session.status {
            since = previous.since
        } else if seen[key] == nil, session.status.isFinished {
            // A session already finished when first seen has been finished for an unknown time;
            // its creation time is the earliest it could have been, so an old one archives at once.
            since = session.createdAt
        } else {
            since = now
        }
        seen[key] = (session.status, since)
        return SessionRow(
            id: session.id, hostID: hostID, bib: session.bib, name: session.name, agent: session.agent,
            agentName: agentName, status: session.status, activity: session.activity, cwd: session.cwd,
            muted: session.muted, managed: session.managed, meta: session.meta, statusSince: since,
            exit: session.status.isFinished ? session.exit : nil, activityHistory: session.activityHistory,
            mission: session.mission
        )
    }

    static func agentName(_ id: String, names: [String: String]) -> String {
        if let name = names[id] { return name }
        if id.isEmpty { return "Shell" }
        if let known = AgentCatalog.name(id) { return known }
        return id.prefix(1).uppercased() + id.dropFirst()
    }

    static func reachability(_ snapshot: HostSnapshot, now: Date) -> Reachability {
        switch snapshot.reach {
        case .connecting: .connecting
        case .waking: .waking
        case .online: .online
        case .offline(let reason): .offline(since: snapshot.lastSeen ?? now, reason: reason)
        case .authNeeded: .authNeeded
        case .failed(let reason): .failed(reason)
        case .keyMissing: .keyMissing
        }
    }

    /// A request answered on this phone already shows its own outcome; one closed from a
    /// phone here is another phone, which reads the same as the desk.
    static func outcome(_ resolution: CloseResolution) -> RequestCard.Outcome {
        switch resolution {
        case .expired, .gone: .expired
        case .desk, .phone, .unknown: .answeredElsewhere
        }
    }

    /// A machine added without the helper reads as missing whatever tmux reports, which is
    /// what tells the screens to offer plain tmux actions only.
    static func helper(_ snapshot: HostSnapshot, noHelper: Bool) -> HelperState {
        if noHelper { return .missing }
        switch snapshot.helper {
        case .unknown: return .checking
        case .present(let version): return .present(version: version, live: snapshot.capabilities.contains(Capability.events))
        case .missing: return .missing
        case .outdated(let found): return .outdated(found: found)
        }
    }
}

extension LiveServices {
    /// What the app shows for a stored machine.
    static func view(of record: HostRecord) -> HostRecordView {
        HostRecordView(
            id: record.id, name: record.profile.name, symbol: HostSymbol(rawValue: record.symbol) ?? .laptop,
            address: record.profile.endpoint.host, etAvailable: record.profile.etAvailable
        )
    }
}
