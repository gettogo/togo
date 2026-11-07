import SwiftUI
import TogoKit

/// Which sections mission control shows and in what order, apart from the views.
///
/// Rules: sections come in `MissionSectionKind.order` whatever order the snapshot sent; a kind
/// the app does not know is dropped; with a descriptor, a section shows only when the harness
/// declares its capability (needs: approvals or questions). Without a descriptor, as from the
/// roster before the session is described, the snapshot's own sections stand.
enum MissionLayout {
    static func sections(_ mission: HarnessMission?, capabilities: Set<String>?) -> [HarnessSection] {
        guard let mission else { return [] }
        return MissionSectionKind.order.compactMap { kind in
            guard let section = mission.sections.first(where: { $0.kind == kind }) else { return nil }
            guard let capabilities else { return section }
            return supports(kind, capabilities) ? section : nil
        }
    }

    static func supports(_ kind: MissionSectionKind, _ capabilities: Set<String>) -> Bool {
        switch kind {
        case .needs:
            capabilities.contains(HarnessCapability.needsApprovals) || capabilities.contains(HarnessCapability.needsQuestions)
        case .agents: capabilities.contains(HarnessCapability.agents)
        case .background: capabilities.contains(HarnessCapability.background)
        case .plan: capabilities.contains(HarnessCapability.plan)
        case .changes: capabilities.contains(HarnessCapability.changes)
        case .ports: capabilities.contains(HarnessCapability.ports)
        case .status: capabilities.contains(HarnessCapability.status)
        default: false
        }
    }

    /// The adapter's extra screens placed in the status section, such as Usage or MCP servers.
    static func statusScreens(_ descriptor: HarnessDescriptor?) -> [HarnessDescriptor.ScreenRef] {
        descriptor?.screens.filter { $0.section == MissionSectionKind.status.rawValue } ?? []
    }

    /// The plan section as a checklist: rows are items, children nest, and the progress comes
    /// from the summary when it has one, else from the rows.
    static func checklist(_ rows: [HarnessRow], summary: MissionSummary.Progress?) -> HarnessScreen.Checklist {
        func item(_ row: HarnessRow) -> HarnessScreen.Checklist.Item {
            HarnessScreen.Checklist.Item(id: row.id, text: row.title, state: row.state, children: row.children?.map(item))
        }
        let items = rows.map(item)
        if let summary, summary.total > 0 {
            return HarnessScreen.Checklist(items: items, done: summary.done, total: summary.total)
        }
        let flat = ChecklistLogic.flatten(items)
        let done = flat.filter { ChecklistLogic.isFinished($0.item.state) }.count
        return HarnessScreen.Checklist(items: items, done: done, total: flat.count)
    }

    /// The timeline item a needs row stands for, so it can be answered in place: the approval
    /// whose request id the row opens, or the item with the row's own id.
    static func needsItem(for row: HarnessRow, in timeline: HarnessTimeline) -> HarnessItem? {
        if let open = row.open, open.kind == .approval {
            let byRequest = timeline.items.last { item in
                if case .approval(let approval) = item.body { approval.requestId == open.ref } else { false }
            }
            if let byRequest { return byRequest }
        }
        return timeline.item(row.id)
    }

    /// What moves the layout of mission control: which sections show, which rows are in each
    /// (by id), and the parts of a row that add a line (a running clock, a subtitle, a progress
    /// bar). A change here glides on `resize`; any other change (a title, a count, a tail line)
    /// lands in place (motion spec §2.5).
    static func shape(_ sections: [HarnessSection]) -> [String] {
        sections.map { section in
            let rows = section.rows.map { row in
                var flags = ""
                if row.state.isLive { flags += "l" }
                if !(row.subtitle ?? "").isEmpty { flags += "s" }
                if row.progress != nil { flags += "p" }
                return row.id + "/" + flags
            }
            return section.kind.rawValue + (section.status == nil ? "" : "+status") + ":" + rows.joined(separator: ",")
        }
    }

    /// Where a row goes when tapped: its own target, or what its section implies.
    static func target(for row: HarnessRow, in kind: MissionSectionKind) -> HarnessOpen? {
        if let open = row.open { return open }
        switch kind {
        case .agents: return HarnessOpen(kind: .chat, ref: row.id)
        case .background: return HarnessOpen(kind: .terminal, ref: row.id)
        case .changes: return HarnessOpen(kind: .diff, ref: row.id)
        case .ports: return HarnessOpen(kind: .url, ref: row.id)
        default: return nil
        }
    }
}

/// How mission control is presented: a sheet over the chat where the window is narrow, a
/// side inspector beside it where there is room (iPad, an unfolded Duo).
enum MissionPresentation: Equatable {
    case sheet
    case inspector

    static func choose(_ horizontalSizeClass: UserInterfaceSizeClass?) -> MissionPresentation {
        horizontalSizeClass == .regular ? .inspector : .sheet
    }
}
