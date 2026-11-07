import Foundation
import Testing
@testable import TogoKit

/// A question and the approval that is the same prompt at the terminal draw one card.
struct HarnessOwnedApprovalTests {
    typealias S = HarnessSamples

    static func question(_ id: String, seq: Int, turn: String?) -> HarnessItem {
        HarnessItem(id: id, seq: seq, turnId: turn, at: S.at(2), body: .question(.init(questions: [
            HarnessQuestion(id: "q1", prompt: "Which color?", options: [HarnessChoice(id: "opt1", label: "Red")], multi: false, allowText: true),
        ], requestId: "r-a1")))
    }

    @Test func ownedApprovalIsNotDrawn() {
        let owned = HarnessItem(id: "a1", seq: 3, turnId: "u1", at: S.at(3), body: .approval(.init(requestId: "r-a1", owner: "q1")))
        let rows = TurnFold.rows([S.user("u1", seq: 1), Self.question("q1", seq: 2, turn: "u1"), owned, S.approval("a2", seq: 4, turn: "u1")])
        let needs = rows.compactMap { row -> String? in
            if case .needs(let item) = row { return item.id }
            return nil
        }
        #expect(needs == ["q1", "a2"])
    }

    @Test func ownedApprovalWithoutATurnIsNotDrawn() {
        let owned = HarnessItem(id: "a1", seq: 2, turnId: nil, at: S.at(3), body: .approval(.init(requestId: "r-a1", owner: "q1")))
        #expect(TurnFold.rows([owned]).isEmpty)
    }
}
