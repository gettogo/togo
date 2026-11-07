import Foundation
import TogoKit

/// What the live working row at the foot of the chat says: how long the agent has worked (the
/// turn's stopwatch, which pauses while it waits on the user) and what it is doing now. nil
/// while the agent is idle.
struct WorkingState: Equatable {
    var clock: TurnClock
    /// "Reading src/auth/sign-in.ts", from the newest step still running; nil when the step is
    /// not known or the reply is already streaming in above.
    var step: String?
    /// The agent waits on an approval or a question: the row says so instead of "Working".
    var waiting = false
}

/// Pure rules behind the working row, so they are tested without a view.
///
/// The row shows while the helper says a turn runs, and from the moment a prompt is sent until
/// the helper catches up, so it appears at once. It clears when the turn ends: the helper
/// stops saying "working" after it had, a turn item ends after the send, or the helper never
/// said so within `sendGrace`.
enum WorkingLogic {
    /// How long a sent prompt alone keeps the row up while the helper does not report work.
    static let sendGrace: TimeInterval = 20

    static func state(
        items: [HarnessItem], working: Bool, awaitingSince: Date?, localSince: Date?, now: Date,
        needsYou: Int = 0, resolvedAt: [String: Date] = [:]
    ) -> WorkingState? {
        guard working || awaitingSince != nil else { return nil }
        let main = items.filter { $0.parent == nil }
        let turn = runningTurn(main)
        let start = min(turn?.start ?? awaitingSince ?? localSince ?? now, now)
        let clock = TurnClock(start: start, waits: TurnClock.waits(
            in: scope(main, turnID: turn?.id), resolvedAt: resolvedAt, turnEnd: nil
        ))
        let waiting = needsYou > 0 || hasOpenNeeds(items)
        return WorkingState(clock: clock, step: waiting ? nil : step(main), waiting: waiting)
    }

    /// The items whose requests pause the clock: the running turn's own, as the fold line
    /// counts them once it ends; without turn items, everything since the newest sent prompt.
    static func scope(_ main: [HarnessItem], turnID: String?) -> [HarnessItem] {
        if let turnID {
            return main.filter { item in
                guard item.turnId == turnID else { return false }
                if case .turn = item.body { return false }
                return true
            }
        }
        let lastPrompt = main.lastIndex { item in
            guard case .user(let user) = item.body else { return false }
            return !user.queued
        } ?? -1
        return Array(main[(lastPrompt + 1)...])
    }

    /// An approval or a question, the main agent's or a subagent's, still waits for an answer.
    /// Only requests since the newest sent prompt count, so an old one the helper never closed
    /// does not hold the row.
    static func hasOpenNeeds(_ items: [HarnessItem]) -> Bool {
        let lastPrompt = items.lastIndex { item in
            guard item.parent == nil, case .user(let user) = item.body else { return false }
            return !user.queued
        } ?? -1
        return items.indices.contains { index in
            guard index > lastPrompt else { return false }
            switch items[index].body {
            case .approval(let approval): return approval.resolved == nil
            case .question(let question): return (question.answered ?? [:]).isEmpty
            default: return false
            }
        }
    }

    /// The same for a conversation already folded into rows, such as a subagent's thread.
    static func hasOpenNeeds(rows: [ChatRow]) -> Bool {
        hasOpenNeeds(rows.compactMap { row in if case .needs(let item) = row { item } else { nil } })
    }

    /// True when a sent prompt no longer holds the row up by itself.
    static func awaitingEnded(
        awaitingSince: Date, wasWorking: Bool, working: Bool, items: [HarnessItem], now: Date
    ) -> Bool {
        if wasWorking, !working { return true }
        if working { return false }
        if now.timeIntervalSince(awaitingSince) > sendGrace { return true }
        // A turn that started after the send has already ended.
        return items.contains { item in
            guard item.parent == nil, case .turn(let turn) = item.body, let ended = turn.endedAt else { return false }
            return !turn.state.isLive && ended.date >= awaitingSince && turn.startedAt.date >= awaitingSince.addingTimeInterval(-2)
        }
    }

    /// The start of the turn that is still running, newest first.
    static func runningTurnStart(_ items: [HarnessItem]) -> Date? {
        runningTurn(items)?.start
    }

    /// The turn that is still running, newest first: its id and start.
    static func runningTurn(_ items: [HarnessItem]) -> (id: String?, start: Date)? {
        for item in items.reversed() {
            if case .turn(let turn) = item.body, turn.state.isLive { return (item.turnId, turn.startedAt.date) }
        }
        return nil
    }

    /// What the agent is doing now: the newest live step of the current turn, after the last
    /// prompt. A reply already streaming in says enough itself.
    static func step(_ items: [HarnessItem]) -> String? {
        for item in items.reversed() {
            switch item.body {
            case .user(let user) where !user.queued:
                return nil
            case .assistant(let text) where text.partial:
                return nil
            case .tool(let tool) where tool.state.isLive:
                return phrase(tool)
            case .agent(let agent) where agent.state.isLive:
                return WorkingCopy.agent(agent.name)
            case .thinking(let text) where text.partial:
                return WorkingCopy.thinking
            default:
                continue
            }
        }
        return nil
    }

    /// "Reading src/a.ts", "Running npm test"; the tool's own title when there is no subject.
    static func phrase(_ tool: HarnessItem.Tool) -> String {
        let subject = tool.subject.map { firstLine($0.text) }.flatMap { $0.isEmpty ? nil : $0 }
        guard let subject else { return firstLine(tool.title) }
        switch tool.category {
        case .read: return WorkingCopy.reading(subject)
        case .edit: return WorkingCopy.editing(subject)
        case .write: return WorkingCopy.writing(subject)
        case .search: return WorkingCopy.searching(subject)
        case .shell: return WorkingCopy.running(subject)
        case .web: return WorkingCopy.fetching(subject)
        default: return firstLine(tool.title)
        }
    }

    private static func firstLine(_ text: String) -> String {
        let line = text.split(separator: "\n", maxSplits: 1).first.map(String.init) ?? ""
        let trimmed = line.trimmingCharacters(in: .whitespaces)
        return trimmed.count > 120 ? String(trimmed.prefix(119)) + "…" : trimmed
    }
}

enum WorkingCopy {
    static let working = String(localized: "chat.live.working", defaultValue: "Working")
    static let waiting = String(localized: "chat.live.waiting", defaultValue: "Waiting for you")
    static let thinking = String(localized: "chat.live.thinking", defaultValue: "Thinking")
    static let stop = String(localized: "chat.live.stop", defaultValue: "Stop")
    static let started = String(localized: "chat.live.started", defaultValue: "Agent is working")
    static let finished = String(localized: "chat.live.finished", defaultValue: "Agent finished")

    static func reading(_ s: String) -> String { String(localized: "chat.live.reading", defaultValue: "Reading \(s)") }
    static func editing(_ s: String) -> String { String(localized: "chat.live.editing", defaultValue: "Editing \(s)") }
    static func writing(_ s: String) -> String { String(localized: "chat.live.writing", defaultValue: "Writing \(s)") }
    static func searching(_ s: String) -> String { String(localized: "chat.live.searching", defaultValue: "Searching \(s)") }
    static func running(_ s: String) -> String { String(localized: "chat.live.running", defaultValue: "Running \(s)") }
    static func fetching(_ s: String) -> String { String(localized: "chat.live.fetching", defaultValue: "Fetching \(s)") }
    static func agent(_ name: String) -> String { String(localized: "chat.live.agent", defaultValue: "Working with \(name)") }

    /// The words before the clock: "Working", or "Waiting for you" while a request is open.
    static func word(_ state: WorkingState) -> String {
        state.waiting ? waiting : working
    }

    /// The seconds the line shows: the current wait while waiting, the working time otherwise.
    /// nil while waiting on a request the clock cannot place.
    static func seconds(_ state: WorkingState, now: Date) -> TimeInterval? {
        guard state.waiting else { return state.clock.worked(at: now) }
        return state.clock.currentWait(at: now)
    }

    /// "Working · 12s", "Waiting for you · 12s".
    static func line(_ state: WorkingState, now: Date) -> String {
        guard let seconds = seconds(state, now: now) else { return word(state) }
        return "\(word(state)) · \(ChatCopy.clock(seconds))"
    }

    /// "Working, 12 seconds, Reading src/a.ts"; "Waiting for you, 12 seconds" while a request
    /// is open.
    static func spoken(_ state: WorkingState, now: Date) -> String {
        [word(state), seconds(state, now: now).map(ChatCopy.spokenClock), state.step]
            .compactMap { $0 }.joined(separator: ", ")
    }
}
