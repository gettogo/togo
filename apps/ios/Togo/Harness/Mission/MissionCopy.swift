import Foundation
import TogoKit

/// Every word mission control says, keyed under `mission.` in the string catalog. Machine words
/// (counts, plan progress) are joined by two spaces, never by dots.
enum MissionCopy {
    static let title = String(localized: "mission.title", defaultValue: "Mission control")
    static let openHint = String(localized: "mission.open.hint", defaultValue: "Opens the session's agents, jobs, plan and status")
    static let toggleHint = String(localized: "mission.toggle.hint", defaultValue: "Shows or hides mission control beside the chat")
    static let done = String(localized: "mission.done", defaultValue: "Done")
    static let allClear = String(localized: "mission.allClear", defaultValue: "All clear")
    static let working = String(localized: "mission.working", defaultValue: "Working")
    static let reading = String(localized: "mission.reading", defaultValue: "Reading the session")
    static let nothingYet = String(localized: "mission.nothingYet", defaultValue: "Nothing here yet")
    static let unavailable = String(localized: "mission.unavailable", defaultValue: "This session has no mission control")
    static let unavailableDetail = String(
        localized: "mission.unavailable.detail", defaultValue: "Terminal only"
    )
    static let answerInChat = String(localized: "mission.answerInChat", defaultValue: "Answer in the chat")
    static let openTerminal = String(localized: "mission.openTerminal", defaultValue: "Open terminal")
    static let openChat = String(localized: "mission.openChat", defaultValue: "Open chat")
    static let openDiff = String(localized: "mission.openDiff", defaultValue: "Review changes")
    static let openPreview = String(localized: "mission.openPreview", defaultValue: "Open preview")
    static let more = String(localized: "mission.more", defaultValue: "More")
    static let noOutputYet = String(localized: "mission.background.noOutput", defaultValue: "No output yet")

    // MARK: Bar item and session menu

    static let goToRequest = String(localized: "mission.menu.goToRequest", defaultValue: "Go to request")
    static let stop = String(localized: "mission.menu.stop", defaultValue: "Stop")
    static let compactMenu = String(localized: "mission.menu.compact", defaultValue: "Compact…")
    static let compactConfirm = String(localized: "mission.menu.compactConfirm", defaultValue: "Compact the conversation now?")
    static let stopFailed = String(localized: "mission.menu.stopFailed", defaultValue: "Couldn't stop. The terminal can.")
    static let sessionSection = String(localized: "mission.session.section", defaultValue: "Session")

    /// A session menu item that opens one of the adapter's screens: "Resume…".
    static func opensScreen(_ title: String) -> String {
        String(localized: "mission.session.screen", defaultValue: "\(title)…")
    }

    /// The Status section before the first status arrives: when it will.
    static func statusPending(_ agent: String?) -> String {
        guard let agent, !agent.isEmpty else {
            return String(localized: "mission.status.pending", defaultValue: "Usage shows after the next reply")
        }
        return String(localized: "mission.status.pending.agent", defaultValue: "Usage shows after \(agent)'s next reply")
    }

    /// A background loop named by what it runs: "Loop: echo, sleep".
    static func loop(_ names: String) -> String {
        String(localized: "mission.background.loop", defaultValue: "Loop: \(names)")
    }

    // MARK: Sections

    static func header(_ kind: MissionSectionKind) -> String {
        switch kind {
        case .needs: String(localized: "mission.section.needs", defaultValue: "Needs you")
        case .agents: String(localized: "mission.section.agents", defaultValue: "Agents")
        case .background: String(localized: "mission.section.background", defaultValue: "Background")
        case .plan: String(localized: "mission.section.plan", defaultValue: "Plan")
        case .changes: String(localized: "mission.section.changes", defaultValue: "Changes")
        case .ports: String(localized: "mission.section.ports", defaultValue: "Ports")
        case .status: String(localized: "mission.section.status", defaultValue: "Status")
        default: kind.rawValue.capitalized
        }
    }

    static func symbol(_ kind: MissionSectionKind) -> String {
        switch kind {
        case .needs: "hand.raised"
        case .agents: "person.2"
        case .background: "terminal"
        case .plan: "checklist"
        case .changes: "plusminus"
        case .ports: "network"
        case .status: "gauge.with.dots.needle.33percent"
        default: "square.grid.2x2"
        }
    }

    /// What an empty section says, so a supported section never reads as broken.
    static func empty(_ kind: MissionSectionKind) -> String {
        switch kind {
        case .needs: String(localized: "mission.empty.needs", defaultValue: "Nothing needs you")
        case .agents: String(localized: "mission.empty.agents", defaultValue: "No agents running")
        case .background: String(localized: "mission.empty.background", defaultValue: "No background jobs")
        case .plan: String(localized: "mission.empty.plan", defaultValue: "No plan yet")
        case .changes: String(localized: "mission.empty.changes", defaultValue: "No changes")
        case .ports: String(localized: "mission.empty.ports", defaultValue: "No open ports")
        default: nothingYet
        }
    }

    // MARK: Counts

    static func needs(_ count: Int) -> String {
        count == 1
            ? String(localized: "mission.count.needs.one", defaultValue: "1 needs you")
            : String(localized: "mission.count.needs.many", defaultValue: "\(count) need you")
    }

    static func agents(_ count: Int) -> String {
        count == 1
            ? String(localized: "mission.count.agents.one", defaultValue: "1 agent")
            : String(localized: "mission.count.agents.many", defaultValue: "\(count) agents")
    }

    static func shells(_ count: Int) -> String {
        count == 1
            ? String(localized: "mission.count.shells.one", defaultValue: "1 shell")
            : String(localized: "mission.count.shells.many", defaultValue: "\(count) shells")
    }

    static func plan(_ progress: MissionSummary.Progress) -> String {
        String(localized: "mission.count.plan", defaultValue: "plan \(progress.done)/\(progress.total)")
    }

    static func context(_ percent: Double) -> String {
        let whole = Int(min(max(percent, 0), 100).rounded())
        return String(localized: "mission.count.context", defaultValue: "ctx \(whole)%")
    }

    static func spokenPlan(_ progress: MissionSummary.Progress) -> String {
        String(localized: "mission.spoken.plan", defaultValue: "plan \(progress.done) of \(progress.total) done")
    }

    static func spokenContext(_ percent: Double) -> String {
        let whole = Int(min(max(percent, 0), 100).rounded())
        return String(localized: "mission.spoken.context", defaultValue: "context \(whole) percent full")
    }
}

/// The words of the activity pill and the roster's mission line, worked out from a summary.
enum MissionText {
    /// The parts in reading order, zero counts left out: "1 needs you", "2 agents", "1 shell",
    /// "plan 3/7". A plan with no steps says nothing.
    static func parts(_ summary: MissionSummary) -> [String] {
        var parts: [String] = []
        if summary.needsYou > 0 { parts.append(MissionCopy.needs(summary.needsYou)) }
        if summary.agents > 0 { parts.append(MissionCopy.agents(summary.agents)) }
        if summary.background > 0 { parts.append(MissionCopy.shells(summary.background)) }
        if let plan = summary.plan, plan.total > 0 { parts.append(MissionCopy.plan(plan)) }
        return parts
    }

    /// "2 agents  1 shell  plan 3/7", or with nothing counted "Working" while a turn runs and
    /// "All clear" when it does not.
    static func pill(_ summary: MissionSummary) -> String {
        let parts = parts(summary)
        guard parts.isEmpty else { return parts.joined(separator: "  ") }
        return summary.working ? MissionCopy.working : MissionCopy.allClear
    }

    /// The same, for VoiceOver: commas, and the plan read out.
    static func spoken(_ summary: MissionSummary) -> String {
        var parts = parts(summary)
        if let plan = summary.plan, plan.total > 0, let at = parts.firstIndex(of: MissionCopy.plan(plan)) {
            parts[at] = MissionCopy.spokenPlan(plan)
        }
        guard parts.isEmpty else { return parts.joined(separator: ", ") }
        return summary.working ? MissionCopy.working : MissionCopy.allClear
    }

    /// Done over total in 0...1, nil without a plan.
    static func planFraction(_ summary: MissionSummary?) -> Double? {
        guard let plan = summary?.plan, plan.total > 0 else { return nil }
        return min(1, max(0, Double(plan.done) / Double(plan.total)))
    }
}
