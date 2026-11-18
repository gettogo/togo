import TogoKit

/// The helper can briefly list both an approval and the question that owns it.
/// Keep the original feed; only the presented Mission snapshot removes that duplicate.
enum MissionNeedsPresentation {
    static func state(_ mission: HarnessMission?) -> MissionState {
        var state = MissionState()
        if let mission { state.load(mission) }
        return state
    }

    static func snapshot(_ mission: HarnessMission?, in timeline: HarnessTimeline) -> HarnessMission? {
        guard let mission, let index = mission.sections.firstIndex(where: { $0.kind == .needs }) else { return mission }
        let rows = mission.sections[index].rows
        let questions = Set(rows.compactMap { row -> String? in
            guard let item = MissionLayout.needsItem(for: row, in: timeline), case .question = item.body else { return nil }
            return item.id
        })
        guard !questions.isEmpty else { return mission }
        let kept = rows.filter { row in
            guard let item = MissionLayout.needsItem(for: row, in: timeline),
                  case .approval(let approval) = item.body, let owner = approval.owner else { return true }
            return !questions.contains(owner)
        }
        let removed = rows.count - kept.count
        guard removed > 0 else { return mission }
        var presented = mission
        presented.sections[index].rows = kept
        presented.summary.needsYou = max(kept.count, mission.summary.needsYou - removed)
        return presented
    }
}
