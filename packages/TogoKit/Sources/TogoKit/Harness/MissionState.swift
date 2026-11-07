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

    public mutating func load(_ mission: HarnessMission) {
        self.mission = mission
    }

    public mutating func apply(_ delta: HarnessDelta) -> HarnessApply {
        guard mission != nil else { return .early }
        return .reopen
    }

    /// The section of `kind`, when the harness supports it.
    public func section(_ kind: MissionSectionKind) -> HarnessSection? {
        mission?.sections.first { $0.kind == kind }
    }
}
