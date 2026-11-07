import Foundation

/// One row of the chat as drawn: the folded form of a turn's items.
public enum ChatRow: Hashable, Sendable, Identifiable {
    /// The user's prompt, sent or queued.
    case prompt(HarnessItem)
    /// The turn's work folded into one quiet line: "Worked 1m 12s, 6 steps, 2 files changed".
    /// `steps` are the tool, thinking, agent, background and plan items it hides.
    case work(id: String, summary: WorkSummary, steps: [HarnessItem])
    /// Assistant text, shown in full.
    case reply(HarnessItem)
    /// Something that needs the user: an approval or a question. Never folded.
    case needs(HarnessItem)
    /// A quiet system line.
    case notice(HarnessItem)
    /// A shell command block.
    case shell(HarnessItem)

    public var id: String {
        switch self {
        case .prompt(let item), .reply(let item), .needs(let item), .notice(let item), .shell(let item): item.id
        case .work(let id, _, _): id
        }
    }
}

/// The numbers on a folded work line.
public struct WorkSummary: Hashable, Sendable {
    public var running: Bool
    public var startedAt: Date?
    public var endedAt: Date?
    public var steps: Int
    public var filesChanged: Int
    public var failed: Bool

    public init(running: Bool, startedAt: Date?, endedAt: Date?, steps: Int, filesChanged: Int, failed: Bool) {
        self.running = running
        self.startedAt = startedAt
        self.endedAt = endedAt
        self.steps = steps
        self.filesChanged = filesChanged
        self.failed = failed
    }

    /// How long the work took, or has taken so far at `now` while it runs.
    public func elapsed(now: Date) -> TimeInterval? {
        guard let startedAt else { return nil }
        let end = running ? now : (endedAt ?? now)
        return max(0, end.timeIntervalSince(startedAt))
    }
}

/// Folds a conversation into chat rows. Per turn: the prompt, then one work row for every tool,
/// thinking, agent, background and plan item (numbers from the turn item when present), then
/// the replies; approvals and questions stay as their own rows where they occurred, so the
/// user never has to expand anything to act. Items without a turn keep their own rows, and
/// steps without a turn next to each other share one work row. Items of a subagent's own
/// conversation (with a `parent`) never appear.
public enum TurnFold {
    public static func rows(_ items: [HarnessItem]) -> [ChatRow] {
        var rows: [ChatRow] = []
        var turns: [String: Turn] = [:]
        var order: [Slot] = []
        var loose: [HarnessItem] = []

        func flushLoose() {
            guard let first = loose.first else { return }
            order.append(.row(.work(id: "work.\(first.id)", summary: computed(loose), steps: loose)))
            loose = []
        }

        for item in items where item.parent == nil {
            if case .unknown = item.body { continue }
            guard let turnID = item.turnId else {
                if isStep(item) {
                    loose.append(item)
                } else {
                    flushLoose()
                    if let row = ownRow(item) { order.append(.row(row)) }
                }
                continue
            }
            flushLoose()
            if turns[turnID] == nil {
                turns[turnID] = Turn()
                order.append(.turn(turnID))
            }
            turns[turnID]?.add(item)
        }
        flushLoose()

        for slot in order {
            switch slot {
            case .row(let row): rows.append(row)
            case .turn(let id): rows.append(contentsOf: turns[id]?.rows(id: id) ?? [])
            }
        }
        return rows
    }

    /// True for the kinds a work line folds away.
    public static func isStep(_ item: HarnessItem) -> Bool {
        switch item.body {
        case .tool, .thinking, .agent, .background, .plan: true
        default: false
        }
    }

    private enum Slot {
        case row(ChatRow)
        case turn(String)
    }

    private struct Turn {
        var prompts: [HarnessItem] = []
        var steps: [HarnessItem] = []
        var rest: [ChatRow] = []
        var turn: HarnessItem.Turn?

        mutating func add(_ item: HarnessItem) {
            switch item.body {
            case .user: prompts.append(item)
            case .turn(let value): turn = value
            default:
                if TurnFold.isStep(item) {
                    steps.append(item)
                } else if let row = TurnFold.ownRow(item) {
                    rest.append(row)
                }
            }
        }

        func rows(id: String) -> [ChatRow] {
            var rows = prompts.map(ChatRow.prompt)
            let summary = turn.map(TurnFold.summary) ?? TurnFold.computed(steps)
            if !steps.isEmpty || summary.running || summary.steps > 0 {
                rows.append(.work(id: "work.\(id)", summary: summary, steps: steps))
            }
            return rows + rest
        }
    }

    private static func ownRow(_ item: HarnessItem) -> ChatRow? {
        switch item.body {
        case .user: .prompt(item)
        case .assistant: .reply(item)
        case .approval, .question: .needs(item)
        case .notice: .notice(item)
        case .shell: .shell(item)
        default: nil
        }
    }

    private static func summary(_ turn: HarnessItem.Turn) -> WorkSummary {
        WorkSummary(
            running: turn.state.isLive, startedAt: turn.startedAt.date, endedAt: turn.endedAt?.date,
            steps: turn.steps, filesChanged: turn.filesChanged,
            failed: turn.state == .failed || turn.state == .interrupted
        )
    }

    /// The numbers for steps without a turn item: the first start, the last end once nothing
    /// runs, and every distinct path a tool changed.
    static func computed(_ steps: [HarnessItem]) -> WorkSummary {
        var running = false
        var started: Date?
        var ended: Date?
        var paths = Set<String>()
        for step in steps {
            let span = span(of: step)
            if span.live { running = true }
            started = min(started ?? span.start, span.start)
            if let end = span.end { ended = max(ended ?? end, end) }
            if case .tool(let tool) = step.body {
                for file in tool.files ?? [] { paths.insert(file.path) }
            }
        }
        return WorkSummary(
            running: running, startedAt: started, endedAt: running ? nil : ended,
            steps: steps.count, filesChanged: paths.count, failed: false
        )
    }

    private static func span(of item: HarnessItem) -> (start: Date, end: Date?, live: Bool) {
        switch item.body {
        case .tool(let tool): (tool.startedAt.date, tool.endedAt?.date, tool.state.isLive)
        case .agent(let agent): (agent.startedAt.date, agent.endedAt?.date, agent.state.isLive)
        case .background(let job): (job.startedAt.date, job.endedAt?.date, false)
        case .thinking(let text): (item.at.date, text.partial ? nil : item.at.date, text.partial)
        default: (item.at.date, item.at.date, false)
        }
    }
}
