import Foundation
@testable import TogoKit

/// The app's state for one session, fed the way the app feeds it: events through the
/// monitor's rules (`HostSnapshot.apply`, seq gaps force a resync), the harness feed through
/// `HarnessFeedState` (as `HarnessSessionStore` drives it), the chat folded by `TurnFold` with
/// a `ResolutionLedger`. No UI and no network: the clock is the fixture's own.
struct ReplayModel {
    let sessionID: String?
    private(set) var host = HostSnapshot(hostID: UUID(uuidString: "00000000-0000-0000-0000-000000000001")!, reach: .online)
    private(set) var feed = HarnessFeedState()
    private(set) var ledger = ResolutionLedger()
    private(set) var descriptor: HarnessDescriptor?
    private(set) var lastSeq = 0
    /// A seq gap was seen: events wait in `held` until a snapshot arrives.
    private(set) var needsResync = false
    private(set) var held: [EventEnvelope] = []
    /// The feed answered `.reopen`: notifications are dropped until an open result arrives.
    private(set) var needsReopen = false
    /// Every request id the events closed, with the fixture time it closed.
    private(set) var closedAt: [String: Int] = [:]
    /// Every request id the events opened, with the fixture time it first opened.
    private(set) var openedAt: [String: Int] = [:]
    private(set) var reopens = 0
    private(set) var resyncs = 0

    init(sessionID: String?) {
        self.sessionID = sessionID
    }

    // MARK: Feeding

    mutating func apply(_ step: ReplayFixture.Step, t: Int) {
        switch step {
        case .subscribed(let snapshot): resync(snapshot)
        case .event(let envelope): receive(envelope, t: t)
        case .described(let d): descriptor = d
        case .opened(let result): open(result)
        case .harness(let note): notify(note)
        case .sent(let item): feed.insertSent(item)
        case .request, .answer: break
        }
        _ = ledger.observe(feed.timeline.items, now: Date(timeIntervalSince1970: Double(t) / 1000))
    }

    /// The monitor's ingest: an old seq is already in, a gap waits for a snapshot.
    mutating func receive(_ event: EventEnvelope, t: Int) {
        if needsResync {
            held.append(event)
            return
        }
        guard event.seq > lastSeq else { return }
        guard event.seq == lastSeq + 1 else {
            needsResync = true
            held.append(event)
            return
        }
        ingest(event, t: t)
    }

    private mutating func ingest(_ event: EventEnvelope, t: Int) {
        lastSeq = event.seq
        switch event.event {
        case .approvalOpen(let a) where openedAt[a.requestId] == nil: openedAt[a.requestId] = t
        case .approvalClosed(let rid, _) where closedAt[rid] == nil: closedAt[rid] = t
        default: break
        }
        host.apply(event)
    }

    /// The monitor's resync: the snapshot replaces sessions and approvals, then held events
    /// at or past it apply.
    mutating func resync(_ snapshot: EventSnapshot, t: Int = 0) {
        if lastSeq > 0 { resyncs += 1 }
        host.sessions = snapshot.sessions
        host.approvals = snapshot.approvals
        lastSeq = snapshot.seq
        needsResync = false
        let backlog = held
        held = []
        for event in backlog { receive(event, t: t) }
    }

    /// The store's open: load, then replay what came early.
    mutating func open(_ result: HarnessOpenResult) {
        if feed.isLoaded { reopens += 1 }
        needsReopen = feed.load(result) == .reopen
    }

    mutating func notify(_ note: HarnessNotification) {
        guard !needsReopen else { return }
        if feed.apply(note) == .reopen { needsReopen = true }
    }

    // MARK: Reading

    var session: Session? { host.sessions.first { $0.id == sessionID } }
    /// Requests the prompt bar and the pill count for this session.
    var pending: [Approval] { host.approvals.filter { $0.sessionId == sessionID } }
    var items: [HarnessItem] { feed.timeline.items }
    var main: [HarnessItem] { feed.timeline.conversation() }
    var rows: [ChatRow] { TurnFold.rows(feed.timeline.items, resolvedAt: ledger.resolvedAt) }
    var mission: HarnessMission? { feed.mission.mission }
    var status: HarnessStatus? { mission?.sections.first { $0.kind == .status }?.status }

    /// Requests the chat draws as waiting (approval or question rows still open).
    var openNeeds: [HarnessItem] {
        rows.compactMap { row in
            guard case .needs(let item) = row, TurnClock.isOpen(item) == true else { return nil }
            return item
        }
    }

    /// The approval request ids the chat still draws as open.
    var openApprovalRequests: [String] {
        openNeeds.compactMap { item in
            if case .approval(let a) = item.body { return a.requestId }
            if case .question(let q) = item.body { return q.requestId }
            return nil
        }
    }

    /// Turn items of the main conversation still running.
    var runningTurns: [HarnessItem] {
        main.filter { item in
            if case .turn(let turn) = item.body { return turn.state.isLive }
            return false
        }
    }

    /// Work lines the chat draws as running.
    var runningWorkRows: [String] {
        rows.compactMap { row in
            if case .work(let id, let summary, _) = row, summary.running { return id }
            return nil
        }
    }

    /// Every place that can say "working": the roster status, the pill's summary, a running
    /// turn item.
    var workingSignals: [String] {
        var out: [String] = []
        if session?.status == .working { out.append("session.status") }
        if feed.mission.summary.working { out.append("mission.summary") }
        if !runningTurns.isEmpty { out.append("turn") }
        return out
    }

    /// The same session opened fresh now: what the chat shows after a reconnect from zero.
    func freshOpen() -> HarnessOpenResult? {
        guard let epoch = feed.timeline.epoch, let mission else { return nil }
        return HarnessOpenResult(epoch: epoch, cursor: feed.timeline.cursor, items: items, more: false, mission: mission)
    }

    /// What `harness.open(after:)` answers now: the held items past `after`, newest versions.
    func reopen(after: Int) -> HarnessOpenResult? {
        guard let epoch = feed.timeline.epoch, let mission else { return nil }
        let newer = items.filter { $0.seq > after }
        return HarnessOpenResult(epoch: epoch, cursor: feed.timeline.cursor, items: newer, more: false, mission: mission)
    }

    /// What `events.subscribe` answers now.
    func snapshot() -> EventSnapshot {
        EventSnapshot(seq: lastSeq, sessions: host.sessions, approvals: host.approvals)
    }
}
