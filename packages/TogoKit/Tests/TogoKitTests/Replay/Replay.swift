import Foundation
@testable import TogoKit

/// Feeds a fixture through `ReplayModel` and keeps the state after every record, so a test can
/// ask what the app showed at any point ("after the approval.answer request, nothing is
/// pending") as well as at the end.
///
/// A perturbation changes how the stream arrives without changing what happened: every frame
/// twice, two frames swapped, or the connection lost for a stretch and then reopened and
/// resynced the way the store and monitor do. While perturbed, a reopen or resync is answered
/// from the unperturbed replay at the furthest point delivered, which is what the helper
/// would answer then.
struct Replay {
    enum Perturbation: Equatable, CustomStringConvertible {
        case none
        /// Every event and harness notification arrives twice.
        case duplicate
        /// Record `at` and the next one swap places.
        case swap(at: Int)
        /// The connection drops after record `at`; the next `missing` events and harness
        /// notifications never arrive; then the app reopens and resyncs.
        case reconnect(at: Int, missing: Int)

        var description: String {
            switch self {
            case .none: "straight"
            case .duplicate: "duplicated"
            case .swap(let at): "swapped at \(at)"
            case .reconnect(let at, let missing): "reconnected after \(at), \(missing) lost"
            }
        }
    }

    let fixture: ReplayFixture
    let perturbation: Perturbation
    /// The state after each record, aligned with `fixture.records`.
    private(set) var states: [ReplayModel] = []

    static func run(_ name: String, _ perturbation: Perturbation = .none) throws -> Replay {
        try Replay(fixture: ReplayFixture.load(name), perturbation)
    }

    init(fixture: ReplayFixture, _ perturbation: Perturbation = .none) {
        self.fixture = fixture
        self.perturbation = perturbation
        let session = fixture.header.session ?? Self.firstSession(fixture)
        let reference = perturbation == .none ? nil : Replay(fixture: fixture)
        var model = ReplayModel(sessionID: session)
        var order = Array(fixture.records.indices)
        if case .swap(let at) = perturbation, at + 1 < order.count { order.swapAt(at, at + 1) }
        var furthest = -1
        var dropped = 0
        for index in order {
            let record = fixture.records[index]
            furthest = max(furthest, index)
            let live = Self.isLive(record.step)
            if case .reconnect(let at, let missing) = perturbation, index > at, live, dropped < missing {
                dropped += 1
                if dropped == missing, let reference { Self.heal(&model, from: reference.states[furthest], t: record.t, force: true) }
                states.append(model)
                continue
            }
            model.apply(record.step, t: record.t)
            if perturbation == .duplicate, live { model.apply(record.step, t: record.t) }
            if let reference { Self.heal(&model, from: reference.states[furthest], t: record.t) }
            states.append(model)
        }
        if case .reconnect = perturbation, dropped > 0, let reference, let last = reference.states.last {
            // The stream ended inside the outage: the app's next open catches up.
            Self.heal(&model, from: last, t: fixture.records.last?.t ?? 0, force: true)
            states[states.count - 1] = model
        }
    }

    /// Reopens and resyncs `model` from `reference` when it needs to, or always with `force`
    /// (a reconnect reopens whatever the feed said).
    private static func heal(_ model: inout ReplayModel, from reference: ReplayModel, t: Int, force: Bool = false) {
        if (model.needsReopen || force), model.feed.isLoaded, let result = reference.reopen(after: model.feed.openAfter) {
            model.open(result)
        }
        if model.needsResync || force { model.resync(reference.snapshot(), t: t) }
    }

    private static func isLive(_ step: ReplayFixture.Step) -> Bool {
        switch step {
        case .event, .harness: true
        default: false
        }
    }

    private static func firstSession(_ fixture: ReplayFixture) -> String? {
        for record in fixture.records {
            if case .event(let e) = record.step, let id = e.sessionId { return id }
            if case .harness(let n) = record.step { return n.sessionId }
        }
        return nil
    }

    // MARK: Reading

    var final: ReplayModel { states.last ?? ReplayModel(sessionID: fixture.header.session) }

    func t(_ index: Int) -> Int { fixture.records[index].t }

    /// The state right after record `index`.
    func after(_ index: Int) -> ReplayModel { states[index] }

    /// The index of the `nth` request of `method` the app made, if any.
    func request(_ method: String, nth: Int = 0) -> Int? {
        fixture.records.indices.filter { index in
            if case .request(let m, _) = fixture.records[index].step { return m == method }
            return false
        }.dropFirst(nth).first
    }

    /// The first index at or after `start` whose state satisfies `predicate`.
    func first(from start: Int = 0, where predicate: (ReplayModel) -> Bool) -> Int? {
        states.indices.first { $0 >= start && predicate(states[$0]) }
    }

    /// Whether some state ever satisfied `predicate`.
    func ever(_ predicate: (ReplayModel) -> Bool) -> Bool { states.contains(where: predicate) }

    /// Indices of events and harness notifications, where perturbations are interesting.
    var liveIndices: [Int] { fixture.records.indices.filter { Self.isLive(fixture.records[$0].step) } }
}

/// What two replays must agree on at the end, whatever the delivery.
struct ReplaySignature: Equatable, CustomStringConvertible {
    var items: [String: HarnessItem]
    var rowIDs: [String]
    var pending: [String]
    var summary: MissionSummary?
    var status: HarnessStatus?
    var sessionStatus: String?

    init(_ model: ReplayModel) {
        items = Dictionary(model.items.map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
        rowIDs = model.rows.map(\.id)
        pending = model.pending.map(\.requestId).sorted()
        summary = model.mission?.summary
        status = model.status
        sessionStatus = model.session?.status.rawValue
    }

    var description: String {
        "rows \(rowIDs), pending \(pending), working \(summary?.working ?? false), session \(sessionStatus ?? "-")"
    }

    /// The fields that differ, by name.
    func differences(from other: ReplaySignature) -> [String] {
        var out: [String] = []
        if items.keys.sorted() != other.items.keys.sorted() { out.append("item ids") }
        else if items != other.items { out.append("item contents") }
        if rowIDs != other.rowIDs {
            out.append(Set(rowIDs) == Set(other.rowIDs) ? "row order" : "row ids")
        }
        if pending != other.pending { out.append("pending requests") }
        if summary != other.summary { out.append("mission summary") }
        if status != other.status { out.append("status") }
        if sessionStatus != other.sessionStatus { out.append("session status") }
        return out
    }
}
