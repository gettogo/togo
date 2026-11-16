import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct RequestNavigationTests {
    private func item(_ id: String, _ seq: Int, parent: String? = nil, _ body: HarnessItem.Body) -> HarnessItem {
        HarnessItem(id: id, seq: seq, parent: parent, at: WireDate(.now), body: body)
    }

    @Test(arguments: [false, true])
    func aNewSentPromptSupersedesOldUnansweredNavigation(queued: Bool) {
        let old = item("old", 1, .approval(.init(requestId: "request")))
        let prompt = item("sent", 2, .user(.init(text: "continue", queued: queued, source: "phone")))
        let rows: [ChatRow] = [.needs(old), .prompt(prompt)]
        #expect(ChatScroll.marks(rows, ready: { _ in true }).map(\.id) == (queued ? ["old"] : []))
        #expect(WorkingLogic.hasOpenNeeds(rows: rows) == queued)
        let store = HarnessSessionStore.preview()
        store.feed.load(HarnessOpenResult(epoch: "e", cursor: 2, items: [old, prompt], more: false,
            mission: HarnessMission(rev: 1, summary: MissionSummary(), sections: [])))
        store.changed()
        #expect(store.oldestOpenNeeds == (queued ? "old" : nil))
    }

    @Test func onlyTheCurrentRequestIsOfferedForNavigation() {
        let old = item("old", 1, .approval(.init(requestId: "old")))
        let prompt = item("sent", 2, .user(.init(text: "continue", queued: false, source: "phone")))
        let current = item("current", 3, .question(.init(questions: [])))
        #expect(ChatScroll.marks([.needs(old), .prompt(prompt), .needs(current)], ready: { _ in true }).map(\.id) == ["current"])
    }

}
