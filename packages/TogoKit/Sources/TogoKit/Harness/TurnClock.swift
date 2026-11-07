import Foundation

/// The turn's one stopwatch. Working time runs from the turn's start and pauses while the turn
/// waits on the user (an approval or a question); the current wait counts on its own. The live
/// line, the fold line, the pill and the Live Activity all read this, so they never disagree
/// and never jump when the user answers.
public struct TurnClock: Hashable, Sendable {
    /// A stretch the turn spent waiting on the user. `end` is nil while it still waits.
    public struct Wait: Hashable, Sendable {
        public var start: Date
        public var end: Date?

        public init(start: Date, end: Date?) {
            self.start = start
            self.end = end
        }
    }

    public var start: Date
    /// nil while the turn runs.
    public var end: Date?
    public var waits: [Wait]

    public init(start: Date, end: Date? = nil, waits: [Wait] = []) {
        self.start = start
        self.end = end
        self.waits = waits
    }

    /// Working time at `now`: the turn's length so far with every wait taken out.
    public func worked(at now: Date) -> TimeInterval {
        let stop = max(start, end ?? now)
        return max(0, stop.timeIntervalSince(start) - waited(until: stop))
    }

    /// The wait still open at `now`, in seconds, or nil when the turn is not waiting.
    public func currentWait(at now: Date) -> TimeInterval? {
        guard end == nil, let open = waits.last(where: { $0.end == nil }) else { return nil }
        return max(0, now.timeIntervalSince(max(open.start, start)))
    }

    /// The turn is waiting on the user now.
    public var isWaiting: Bool { end == nil && waits.contains { $0.end == nil } }

    /// Where a system timer (`Text(timerInterval:)`, a Live Activity) must start so it shows
    /// the working time at `now` and keeps counting from there rather than from zero.
    public func workingOrigin(at now: Date) -> Date {
        now.addingTimeInterval(-worked(at: now))
    }

    /// The origin of the number on screen, for a one-second schedule
    /// (`TimelineView(.periodic(from: displayOrigin, by: 1))`) whose ticks land exactly when the
    /// shown second changes. While `waiting` with a wait open: that wait's start (the number is
    /// the wait). Otherwise the turn's start plus the time spent waiting so far, which stays
    /// fixed while the turn works, so `now - origin` is the working time.
    public func displayOrigin(waiting: Bool, at now: Date = .now) -> Date {
        if waiting, end == nil, let open = waits.last(where: { $0.end == nil }) {
            return max(open.start, start)
        }
        let stop = max(start, end ?? now)
        return start.addingTimeInterval(waited(until: stop))
    }

    /// Seconds spent waiting between the start and `stop`, overlapping waits counted once.
    private func waited(until stop: Date) -> TimeInterval {
        let spans = waits.compactMap { wait -> (Date, Date)? in
            let from = max(wait.start, start)
            let to = min(wait.end ?? stop, stop)
            return to > from ? (from, to) : nil
        }.sorted { $0.0 < $1.0 }
        var total: TimeInterval = 0
        var current: (Date, Date)?
        for span in spans {
            if let open = current, span.0 <= open.1 {
                current = (open.0, max(open.1, span.1))
            } else {
                if let open = current { total += open.1.timeIntervalSince(open.0) }
                current = span
            }
        }
        if let open = current { total += open.1.timeIntervalSince(open.0) }
        return total
    }
}

extension TurnClock {
    /// The waits among a turn's items, in order. A wait starts at the approval's or question's
    /// `at`. It ends when the helper or the phone saw it resolved (`resolvedAt`); for history
    /// with no such time, at the next item of the turn after it, else at the turn's end. An
    /// open request in an ended turn ends with the turn; in a running one it is still open.
    public static func waits(in items: [HarnessItem], resolvedAt: [String: Date], turnEnd: Date?) -> [Wait] {
        var out: [Wait] = []
        for (index, item) in items.enumerated() {
            guard let open = Self.isOpen(item) else { continue }
            let start = item.at.date
            if open {
                out.append(Wait(start: start, end: turnEnd))
                continue
            }
            let next = items[(index + 1)...].first { $0.at.date >= start && !Self.isNeeds($0) }?.at.date
            out.append(Wait(start: start, end: resolvedAt[item.id] ?? next ?? turnEnd ?? start))
        }
        return out
    }

    /// True for a request still waiting, false for one answered, nil for anything else.
    public static func isOpen(_ item: HarnessItem) -> Bool? {
        switch item.body {
        case .approval(let approval): approval.resolved == nil
        case .question(let question): (question.answered ?? [:]).isEmpty
        default: nil
        }
    }

    private static func isNeeds(_ item: HarnessItem) -> Bool { isOpen(item) != nil }
}

/// When each request was answered, as far as this phone knows: the moment the feed first
/// showed resolved a request it had seen open. Requests already answered when first seen get
/// no entry; `TurnClock.waits` falls back to the next item for those.
public struct ResolutionLedger: Equatable, Sendable {
    public private(set) var resolvedAt: [String: Date] = [:]
    private var open: Set<String> = []

    public init() {}

    /// Notes requests seen open and the ones that resolved since. Returns whether anything
    /// was newly resolved.
    @discardableResult
    public mutating func observe(_ items: [HarnessItem], now: Date) -> Bool {
        var changed = false
        for item in items {
            guard let isOpen = TurnClock.isOpen(item) else { continue }
            if isOpen {
                open.insert(item.id)
            } else if open.remove(item.id) != nil, resolvedAt[item.id] == nil {
                resolvedAt[item.id] = now
                changed = true
            }
        }
        return changed
    }
}
