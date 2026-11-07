import Foundation
import Testing
@testable import TogoKit

struct HarnessMissionTests {
    typealias S = HarnessSamples

    private func loaded(rev: Int = 1) -> MissionState {
        var state = MissionState()
        state.load(S.mission(rev: rev, sections: [.needs, .background, .status]))
        return state
    }

    @Test func deltaBeforeLoadIsEarly() {
        var state = MissionState()
        #expect(state.apply(HarnessDelta(rev: 1, ops: [])) == .early)
    }

    @Test func upsertAppendsThenReplaces() {
        var state = loaded()
        #expect(state.apply(HarnessDelta(rev: 2, ops: [.init(op: .upsert, section: .background, row: S.row("j1"))])) == .applied)
        #expect(state.apply(HarnessDelta(rev: 3, ops: [
            .init(op: .upsert, section: .background, row: S.row("j2")),
            .init(op: .upsert, section: .background, row: S.row("j1", state: .ok)),
        ])) == .applied)
        let rows = state.section(.background)?.rows ?? []
        #expect(rows.map(\.id) == ["j1", "j2"])
        #expect(rows.first?.state == .ok)
        #expect(state.mission?.rev == 3)
    }

    @Test func removeAndReplace() {
        var state = loaded()
        _ = state.apply(HarnessDelta(rev: 2, ops: [.init(op: .replace, section: .background, rows: [S.row("a"), S.row("b")])]))
        _ = state.apply(HarnessDelta(rev: 3, ops: [.init(op: .remove, section: .background, id: "a")]))
        #expect(state.section(.background)?.rows.map(\.id) == ["b"])
    }

    @Test func statusAndSummary() {
        var state = loaded()
        let status = HarnessStatus(model: "Opus", contextPercent: 40)
        _ = state.apply(HarnessDelta(rev: 2, ops: [
            .init(op: .status, status: status),
            .init(op: .summary, summary: MissionSummary(needsYou: 1, working: true)),
        ]))
        #expect(state.section(.status)?.status == status)
        #expect(state.summary.needsYou == 1)
        #expect(state.summary.working)
    }

    @Test func opForAMissingSectionIsSkipped() {
        var state = loaded()
        #expect(state.apply(HarnessDelta(rev: 2, ops: [.init(op: .upsert, section: .ports, row: S.row("3000"))])) == .applied)
        #expect(state.section(.ports) == nil)
        #expect(state.mission?.rev == 2)
    }

    @Test func oldRevIsIgnoredAndAGapReopens() {
        var state = loaded(rev: 4)
        #expect(state.apply(HarnessDelta(rev: 4, ops: [.init(op: .summary, summary: MissionSummary(working: true))])) == .ignored)
        #expect(!state.summary.working)
        #expect(state.apply(HarnessDelta(rev: 6, ops: [])) == .reopen)
        #expect(state.mission?.rev == 4)
    }
}
