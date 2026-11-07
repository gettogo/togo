import Foundation

/// A session's mission control as the app holds it.
///
/// Rules: a delta whose rev is `mission.rev + 1` applies its ops in order; rev at or below the
/// held rev is ignored; any other rev answers `.reopen`. Upsert replaces the row with the same
/// id or appends it; remove drops it; replace swaps every row of the section; status and
/// summary replace theirs. An op for a section the snapshot lacks is skipped.
public struct MissionState: Sendable {
    public private(set) var mission: HarnessMission?

    public init() {}

    public var summary: MissionSummary { mission?.summary ?? MissionSummary() }

    public mutating func load(_ mission: HarnessMission) {
        self.mission = mission
    }

    public mutating func apply(_ delta: HarnessDelta) -> HarnessApply {
        guard var held = mission else { return .early }
        if delta.rev <= held.rev { return .ignored }
        if delta.rev != held.rev + 1 { return .reopen }
        for op in delta.ops {
            Self.apply(op, to: &held)
        }
        held.rev = delta.rev
        mission = held
        return .applied
    }

    /// The section of `kind`, when the harness supports it.
    public func section(_ kind: MissionSectionKind) -> HarnessSection? {
        mission?.sections.first { $0.kind == kind }
    }

    private static func apply(_ op: HarnessDelta.Op, to mission: inout HarnessMission) {
        switch op.op {
        case .summary:
            if let summary = op.summary { mission.summary = summary }
        case .status:
            guard let status = op.status,
                  let at = mission.sections.firstIndex(where: { $0.kind == .status }) else { return }
            mission.sections[at].status = status
        case .upsert:
            guard let row = op.row, let at = sectionIndex(op, in: mission) else { return }
            if let existing = mission.sections[at].rows.firstIndex(where: { $0.id == row.id }) {
                mission.sections[at].rows[existing] = row
            } else {
                mission.sections[at].rows.append(row)
            }
        case .remove:
            guard let id = op.id ?? op.row?.id, let at = sectionIndex(op, in: mission) else { return }
            mission.sections[at].rows.removeAll { $0.id == id }
        case .replace:
            guard let at = sectionIndex(op, in: mission) else { return }
            mission.sections[at].rows = op.rows ?? []
        default:
            // An op this build does not know changes nothing.
            return
        }
    }

    private static func sectionIndex(_ op: HarnessDelta.Op, in mission: HarnessMission) -> Int? {
        guard let kind = op.section else { return nil }
        return mission.sections.firstIndex { $0.kind == kind }
    }
}
