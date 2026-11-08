import Foundation
import TogoKit

/// One subagent's own conversation, read out of the session's timeline: the agent item that
/// announced it, the items whose `parent` is that item, folded into chat rows the way the main
/// conversation is, and the way back up through any agent that started it.
struct SubagentThread: Equatable {
    /// The agent item's id; items with this `parent` belong here.
    let id: String
    /// The agent item, once the timeline holds it.
    let agent: HarnessItem.Agent?
    /// The agent that started this one, nil when the main conversation did.
    let parent: String?
    let rows: [ChatRow]

    init(id: String, timeline: HarnessTimeline) {
        self.id = id
        let item = timeline.item(id)
        if case .agent(let agent)? = item?.body {
            self.agent = agent
        } else {
            agent = nil
        }
        parent = item?.parent
        rows = Self.rows(timeline.conversation(parent: id))
    }

    /// The agent's name for titles: its own name, or its role, or a plain word.
    var title: String {
        if let name = agent?.name, !name.isEmpty { return name }
        if let role = agent?.role, !role.isEmpty { return role }
        return TerminalSyncCopy.subagent
    }

    var state: HarnessState { agent?.state ?? .running }

    /// Folds a subagent's items as the main chat folds its own. TurnFold leaves out items that
    /// have a parent, so here they are lifted to the top level first.
    static func rows(_ items: [HarnessItem]) -> [ChatRow] {
        ChatRowDedupe.apply(TurnFold.rows(items.map { item in
            var top = item
            top.parent = nil
            return top
        }))
    }

    /// The agents from the main conversation down to `id`, outermost first, `id` last. Stops at
    /// a loop or a missing link rather than walking forever.
    static func lineage(of id: String, in timeline: HarnessTimeline) -> [String] {
        var chain = [id]
        var seen: Set<String> = [id]
        var current = timeline.item(id)?.parent
        while let next = current, !seen.contains(next) {
            chain.insert(next, at: 0)
            seen.insert(next)
            current = timeline.item(next)?.parent
        }
        return chain
    }

    /// Approvals and questions from any subagent that still wait for the user. They show in the
    /// subagent's own conversation, and bubble up to the main chat so the user never has to
    /// drill down to find what waits for them.
    static func waitingNeeds(in timeline: HarnessTimeline) -> [HarnessItem] {
        timeline.items.filter { item in
            guard item.parent != nil else { return false }
            switch item.body {
            case .approval(let approval): return approval.resolved == nil
            case .question(let question): return question.answered == nil
            default: return false
            }
        }
    }

    /// The main chat's rows with the subagents' waiting needs added where they arrived, so an
    /// approval a subagent asks for is answerable from the main chat.
    static func mainRows(
        _ timeline: HarnessTimeline, conversation: [HarnessItem]? = nil, resolvedAt: [String: Date] = [:]
    ) -> [ChatRow] {
        let main = ChatRowDedupe.apply(TurnFold.rows(conversation ?? timeline.conversation(), resolvedAt: resolvedAt))
        let waiting = waitingNeeds(in: timeline)
        guard !waiting.isEmpty else { return main }
        var out: [ChatRow] = []
        var pending = waiting.sorted { $0.seq < $1.seq }[...]
        for row in main {
            if let first = Self.firstSeq(row) {
                while let next = pending.first, next.seq < first {
                    out.append(.needs(next))
                    pending = pending.dropFirst()
                }
            }
            out.append(row)
        }
        out.append(contentsOf: pending.map(ChatRow.needs))
        return out
    }

    private static func firstSeq(_ row: ChatRow) -> Int? {
        switch row {
        case .prompt(let item), .reply(let item), .needs(let item), .notice(let item), .shell(let item):
            item.seq
        case .work(_, _, let steps):
            steps.map(\.seq).min()
        }
    }
}
