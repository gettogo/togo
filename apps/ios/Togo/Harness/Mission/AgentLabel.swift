import Foundation
import TogoKit

/// Display names for incomplete data from an older helper. IDs and open
/// targets remain unchanged, so a fallback never changes which agent opens.
enum AgentLabel {
    private static func useful(_ text: String?) -> String? {
        guard let text, !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return nil }
        return text
    }

    private static func fallback(_ id: String) -> String {
        guard let id = useful(id) else { return String(localized: "agent.label", defaultValue: "Agent") }
        return String(localized: "agent.label.id", defaultValue: "Agent \(id)")
    }

    static func name(_ agent: HarnessItem.Agent) -> String {
        useful(agent.name) ?? useful(agent.role) ?? fallback(agent.agentId)
    }

    static func line(_ agent: HarnessItem.Agent) -> String {
        let title = name(agent)
        guard let role = useful(agent.role), role != title else { return title }
        return title + "  " + role
    }

    static func title(_ row: HarnessRow) -> String {
        useful(row.title) ?? useful(row.subtitle) ?? useful(row.id) ?? fallback("")
    }

    static func subtitle(_ row: HarnessRow) -> String? {
        guard let subtitle = useful(row.subtitle), subtitle != title(row) else { return nil }
        return subtitle
    }

    static func resolved(_ row: HarnessRow, in timeline: HarnessTimeline) -> HarnessRow {
        guard useful(row.title) == nil else { return row }
        let linked: HarnessItem? = if let open = row.open, open.kind == .chat,
            let item = timeline.item(open.ref), case .agent = item.body { item } else { nil }
        let item = linked ?? timeline.items.last {
            if case .agent(let agent) = $0.body { agent.agentId == row.id } else { false }
        }
        let agent: HarnessItem.Agent? = if let item, case .agent(let agent) = item.body { agent } else { nil }
        var display = row
        display.title = useful(agent?.name) ?? useful(row.subtitle) ?? useful(agent?.role) ?? title(row)
        display.subtitle = useful(row.subtitle) ?? useful(agent?.role)
        display.subtitle = subtitle(display)
        return display
    }
}
