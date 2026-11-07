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
}

/// Folds a conversation into chat rows. Per turn: the prompt, then one work row for every tool,
/// thinking, agent, background and plan item (numbers from the turn item when present), then
/// the replies; approvals and questions stay as their own rows where they occurred, so the
/// user never has to expand anything to act. Items without a turn keep their own rows.
public enum TurnFold {
    public static func rows(_ items: [HarnessItem]) -> [ChatRow] {
        items.compactMap { item in
            switch item.body {
            case .user: .prompt(item)
            case .assistant: .reply(item)
            case .approval, .question: .needs(item)
            case .notice: .notice(item)
            case .shell: .shell(item)
            default: nil
            }
        }
    }
}
