import Foundation
import Observation
import Synchronization
import Testing
import TogoKit
@testable import Togo

@MainActor struct MissionNeedsAliasTests {
    private func fixture(owner: String? = "question", questionLoaded: Bool = true) -> HarnessSessionStore {
        let approval = HarnessItem(id: "approval-request", seq: 1, at: WireDate(.now),
                                  body: .approval(.init(requestId: "request", owner: owner)))
        let question = HarnessItem(id: "question", seq: 2, at: WireDate(.now), body: .question(.init(questions: [
            .init(id: "permission", prompt: "Allow this?", options: [.init(id: "allow", label: "Allow")], multi: false, allowText: false)
        ])))
        let rows = [HarnessRow(id: approval.id, title: "Approve?", state: .waiting),
                    HarnessRow(id: question.id, title: "Question", state: .waiting)]
        let mission = HarnessMission(rev: 1, summary: MissionSummary(needsYou: 2), sections: [.init(kind: .needs, rows: rows)])
        let store = HarnessSessionStore.preview()
        store.feed.load(.init(epoch: "e", cursor: questionLoaded ? 2 : 1, items: questionLoaded ? [approval, question] : [approval],
                              more: false, mission: mission))
        return store
    }

    @Test func onePromptHasOneMissionCardAndBadge() {
        let store = fixture()
        #expect(store.mission.section(.needs)?.rows.map(\.id) == ["question"])
        #expect(store.mission.summary.needsYou == 1)
        let item = MissionToolbarItem(store: store, presentation: .sheet)
        #expect(item.badge == 1)
        // The wire snapshot and sequence remain authoritative and unchanged.
        #expect(store.feed.mission.summary.needsYou == 2)
        #expect(store.timeline.cursor == 2)
    }

    @Test func anApprovalIsKeptUntilItsQuestionIsActuallyAvailable() {
        let store = fixture(questionLoaded: false)
        #expect(store.mission.section(.needs)?.rows.map(\.id) == ["approval-request", "question"])
        #expect(store.mission.summary.needsYou == 2)
    }

    @Test func aLateOwnerUpdateRefreshesMissionEvenWithoutAMissionDelta() {
        let store = fixture(owner: nil)
        let changed = Mutex(false)
        withObservationTracking { _ = store.mission } onChange: { changed.withLock { $0 = true } }
        let relinked = HarnessItem(id: "approval-request", seq: 3, at: WireDate(.now),
                                  body: .approval(.init(requestId: "request", owner: "question")))
        _ = store.feed.apply(.init(sessionId: store.subject.sessionID, kind: .items, ts: WireDate(.now), epoch: "e", items: [relinked]))
        #expect(changed.withLock { $0 })
        #expect(store.mission.section(.needs)?.rows.map(\.id) == ["question"])
        #expect(store.mission.summary.needsYou == 1)
    }
}
