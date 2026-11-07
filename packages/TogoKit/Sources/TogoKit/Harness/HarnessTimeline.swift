import Foundation

/// What applying a notification did.
public enum HarnessApply: Equatable, Sendable {
    case applied
    /// Already reflected (seq at or below the cursor, rev at or below the held rev).
    case ignored
    /// Arrived before `load`; the caller keeps it and replays it after.
    case early
    /// A gap, a reset or another epoch: call `harnessOpen` again and `load` the result.
    case reopen
}

/// A session's conversation as the app holds it: the items in display order, built from one
/// `harnessOpen` result, later `harness` notifications and older `harnessHistory` pages.
///
/// Rules:
/// - An item with a known id replaces the held one in place (its position does not move); a
///   new id is appended. Display order is first-seen order, which is seq order on load.
/// - Notified items with seq at or below `cursor` are ignored; `cursor` becomes the highest
///   seq applied. A first new seq past `cursor + 1` is a gap and answers `.reopen`.
/// - A notification for another epoch, or of kind reset, answers `.reopen`.
/// - Loading a result of the epoch already held merges it the same way; another epoch
///   replaces everything.
/// - `prepend` adds an older page before the first item and sets `hasOlder`.
/// - At most `limit` items are held; the oldest go first and `hasOlder` turns true.
public struct HarnessTimeline: Sendable {
    /// The most items held for one session, matching the helper's own ring.
    public static let limit = 2000

    public private(set) var epoch: String?
    public private(set) var cursor: Int = 0
    public private(set) var items: [HarnessItem] = []
    public private(set) var hasOlder = false
    private var index: [String: Int] = [:]
    /// Sent items shown before the items between the cursor and them arrived, by seq. A later
    /// arrival with a lower seq goes before them, so a prompt never draws above output it
    /// followed.
    private var ahead: [String: Int] = [:]

    public init() {}

    public var isLoaded: Bool { epoch != nil }

    /// The lowest seq held, for paging older items with `harnessHistory(before:)`.
    public var oldestSeq: Int? { items.lazy.map(\.seq).min() }

    public mutating func load(_ result: HarnessOpenResult) {
        if epoch == result.epoch {
            for item in result.items.sorted(by: { $0.seq < $1.seq }) { upsert(item) }
            cursor = max(cursor, result.cursor)
            settleAhead()
            if items.isEmpty { hasOlder = result.more }
        } else {
            epoch = result.epoch
            cursor = result.cursor
            items = result.items.sorted { $0.seq < $1.seq }
            hasOlder = result.more
            ahead = [:]
            reindex()
        }
        trim()
    }

    /// Forgets everything held, so the next `load` starts over.
    public mutating func clear() {
        self = HarnessTimeline()
    }

    /// Applies the items of an items notification.
    public mutating func apply(_ note: HarnessNotification) -> HarnessApply {
        guard isLoaded else { return .early }
        guard note.epoch == epoch, note.kind != .reset else { return .reopen }
        guard note.kind == .items else { return .ignored }
        let fresh = (note.items ?? []).filter { $0.seq > cursor }.sorted { $0.seq < $1.seq }
        guard let first = fresh.first else { return .ignored }
        if first.seq > cursor + 1 { return .reopen }
        for item in fresh { upsert(item) }
        cursor = fresh[fresh.count - 1].seq
        settleAhead()
        trim()
        return .applied
    }

    /// The item a send returned, shown before its notification arrives. The cursor does not
    /// move, so the notification still applies (in place) when it comes.
    public mutating func insertSent(_ item: HarnessItem) {
        guard isLoaded else { return }
        if let at = index[item.id], items[at].seq > item.seq { return }
        let known = index[item.id] != nil
        upsert(item)
        if !known, item.seq > cursor { ahead[item.id] = item.seq }
        trim()
    }

    public mutating func prepend(_ page: HarnessPage) {
        let older = page.items.filter { index[$0.id] == nil }.sorted { $0.seq < $1.seq }
        items = older + items
        hasOlder = page.more
        reindex()
    }

    /// The main conversation (`parent == nil`) or a subagent's own.
    public func conversation(parent: String? = nil) -> [HarnessItem] {
        items.filter { $0.parent == parent }
    }

    public func item(_ id: String) -> HarnessItem? {
        index[id].map { items[$0] }
    }

    private mutating func upsert(_ item: HarnessItem) {
        if let at = index[item.id] {
            items[at] = item
        } else if let at = ahead.filter({ $0.value > item.seq }).compactMap({ index[$0.key] }).min() {
            items.insert(item, at: at)
            reindex()
        } else {
            index[item.id] = items.count
            items.append(item)
        }
    }

    /// Sent items the cursor has reached are in their place now.
    private mutating func settleAhead() {
        ahead = ahead.filter { $0.value > cursor }
    }

    private mutating func trim() {
        guard items.count > Self.limit else { return }
        items.removeFirst(items.count - Self.limit)
        hasOlder = true
        reindex()
    }

    private mutating func reindex() {
        index = Dictionary(items.enumerated().map { ($1.id, $0) }, uniquingKeysWith: { _, last in last })
    }
}

/// One open session's feed as the app applies it: the conversation, mission control, and
/// notifications that came before the open result.
///
/// Notifications that arrive before `load` are kept and replayed right after it. A `.reopen`
/// answer means the caller opens again with `openAfter`; after a reset or another epoch that
/// is 0, so the next result replaces what is held.
public struct HarnessFeedState: Sendable {
    public private(set) var timeline = HarnessTimeline()
    public private(set) var mission = MissionState()
    private var early: [HarnessNotification] = []
    private var restart = false

    public init() {}

    public var isLoaded: Bool { timeline.isLoaded }

    /// The cursor for the next `harnessOpen`.
    public var openAfter: Int { restart ? 0 : timeline.cursor }

    /// Applies an open result and replays the notifications held for it. Answers `.reopen`
    /// when a replayed one says so.
    @discardableResult
    public mutating func load(_ result: HarnessOpenResult) -> HarnessApply {
        if restart || timeline.epoch != result.epoch { timeline.clear() }
        restart = false
        timeline.load(result)
        mission.load(result.mission)
        let held = early
        early = []
        var outcome = HarnessApply.applied
        for note in held where note.epoch == result.epoch || note.kind == .reset {
            if apply(note) == .reopen { outcome = .reopen }
        }
        return outcome
    }

    public mutating func apply(_ note: HarnessNotification) -> HarnessApply {
        guard isLoaded else {
            early.append(note)
            return .early
        }
        if note.kind == .reset || note.epoch != timeline.epoch {
            restart = true
            return .reopen
        }
        if note.kind == .mission {
            guard let delta = note.mission else { return .ignored }
            return mission.apply(delta)
        }
        return timeline.apply(note)
    }

    public mutating func insertSent(_ item: HarnessItem) {
        timeline.insertSent(item)
    }

    public mutating func prepend(_ page: HarnessPage) {
        timeline.prepend(page)
    }

    /// Drops everything; the next open starts from the beginning.
    public mutating func reset() {
        self = HarnessFeedState()
    }
}
