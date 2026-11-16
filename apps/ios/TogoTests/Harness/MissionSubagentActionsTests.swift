import Testing
import TogoKit
@testable import Togo

@MainActor struct MissionSubagentActionsTests {
    @Test func missionSubagentKeepsApprovalAndLinkActions() {
        let store = HarnessSessionStore.preview()
        let card = RequestCard(id: "request", hostID: Samples.studio, sessionID: "s-04", bib: "04",
            agentName: "OMP", title: "Permission", detail: nil, options: [], allowText: false, phase: .open)
        var answered: [String] = []
        var quoted: [String] = []
        let actions = ChatActions(card: { $0 == card.id ? card : nil }, answer: { id, _ in answered.append(id) },
                                  quote: { quoted.append($0) })
        let stack = MissionControlStack(store: store, actions: actions)
        let destination = stack.subagentView("agent")
        #expect(destination.actions.card(card.id)?.id == card.id)
        destination.actions.quote("quote")
        #expect(quoted == ["quote"])
        destination.actions.answer(card.id, .deny)
        #expect(answered == [card.id])
    }
}
