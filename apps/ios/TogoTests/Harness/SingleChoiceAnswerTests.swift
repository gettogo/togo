import Testing
import TogoKit
@testable import Togo

@MainActor struct SingleChoiceAnswerTests {
    private func question(multi: Bool = false, defaults: [String]? = nil) -> HarnessQuestion {
        HarnessQuestion(id: "q", prompt: "Choose", options: [.init(id: "one", label: "One")],
                        multi: multi, allowText: true, default: defaults)
    }

    @Test func typingOtherClearsASingleChoice() {
        let q = question()
        let model = ConfirmModel(confirm: .init(questions: [q], submit: "answer"), arms: false)
        model.toggle("one", in: q)
        model.setText("another", for: q)
        #expect(!model.isSelected("one", in: q))
        #expect(model.answers == ["q": ["another"]])
        model.toggle("one", in: q)
        #expect(model.text(for: q).isEmpty)
        #expect(model.answers == ["q": ["one"]])
    }

    @Test func multiChoiceStillAllowsOptionsWithOtherText() {
        let q = question(multi: true)
        let model = ConfirmModel(confirm: .init(questions: [q], submit: "answer"), arms: false)
        model.toggle("one", in: q)
        model.setText("another", for: q)
        #expect(model.answers == ["q": ["one", "another"]])
    }

    @Test func singleChoiceDefaultsAndDirectAnswerBuildingNeverSendTwoAnswers() {
        let q = question(defaults: ["one", "another"])
        let confirm = HarnessScreen.Confirm(questions: [q], submit: "answer")
        let model = ConfirmModel(confirm: confirm, arms: false)
        #expect(model.answers["q"]?.count == 1)
        #expect(ConfirmModel.answers(confirm, selections: ["q": ["one"]], texts: ["q": "another"]) == ["q": ["another"]])
    }
}
