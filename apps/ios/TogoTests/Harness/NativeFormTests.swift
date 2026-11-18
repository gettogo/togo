import Foundation
import Testing
import TogoKit
@testable import Togo

/// Question bodies mapped from OpenCode 2.2.5's recorded form events.
@MainActor struct NativeFormTests {
    private func form(_ name: String) throws -> HarnessScreen.Confirm {
        let url = try #require(Bundle(for: FormFixtureBundle.self).url(forResource: "opencode-native-forms", withExtension: "json"))
        let bodies = try JSONDecoder().decode([String: HarnessItem.Question].self, from: Data(contentsOf: url))
        return HarnessScreen.Confirm(questions: try #require(bodies[name]).questions, submit: "answer")
    }

    @Test func recordedDefaultsSeedTextAndDoNotRequireOptionalFields() throws {
        let confirm = try form("plain")
        let model = ConfirmModel(confirm: confirm, arms: false)
        #expect(model.text(for: confirm.questions[0]) == "scratch")
        #expect(model.isComplete)
        #expect(model.answers == ["project_name": ["scratch"]])
        model.setText("changed", for: confirm.questions[0])
        model.update(confirm)
        #expect(model.text(for: confirm.questions[0]) == "changed")
    }

    @Test func recordedConditionalDefaultsDoNotLeakHiddenAnswers() throws {
        let confirm = try form("conditional")
        let model = ConfirmModel(confirm: confirm, arms: false)
        #expect(model.answers == ["notify!": ["false"], "targets[]": ["ios"], "retries#": ["2"]])
        #expect(model.isComplete)
        #expect(model.visibleQuestions.map(\.id) == ["notify!", "targets[]", "retries#", "quiet_reason"])
        let notice = confirm.questions[0], channel = confirm.questions[1]
        model.toggle("true", in: notice)
        #expect(model.answers["channel"] == ["mail"])
        model.toggle("chat", in: channel)
        model.toggle("false", in: notice)
        #expect(model.answers["channel"] == nil)
        model.toggle("true", in: notice)
        #expect(model.answers["channel"] == ["chat"])
    }

    @Test func changingMultiSelectHidesAnAnswerWithoutLosingLocalText() throws {
        let confirm = try form("conditional")
        let model = ConfirmModel(confirm: confirm, arms: false)
        let targets = confirm.questions[2], note = confirm.questions[3]
        model.toggle("mac", in: targets)
        model.setText("signed", for: note)
        #expect(model.answers["mac_note"] == ["signed"])
        model.toggle("mac", in: targets)
        #expect(model.answers["mac_note"] == nil)
        #expect(model.text(for: note) == "signed")
        model.toggle("mac", in: targets)
        #expect(model.answers["mac_note"] == ["signed"])
    }

    @Test(arguments: ["fields_17", "fields_64"])
    func recordedLargeFormsSendEveryField(_ name: String) async throws {
        let confirm = try form(name)
        let model = ConfirmModel(confirm: confirm, arms: false)
        #expect(!model.isComplete)
        for field in confirm.questions { model.setText("value \(field.id)", for: field) }
        #expect(model.answers.count == confirm.questions.count)
        #expect(model.isComplete)
        model.armNow()
        var sent: [String: JSONValue]?
        await model.submit(for: confirm) { sent = $0; return HarnessActionResult(result: .applied) }
        #expect(sent == ConfirmModel.args(model.answers))
    }

    @Test func legacyMissingRequiredStillRequiresEveryQuestion() throws {
        let data = Data(#"{"id":"old","prompt":"Required before the additive fields","options":[],"multi":false,"allowText":true}"#.utf8)
        let field = try JSONDecoder().decode(HarnessQuestion.self, from: data)
        let model = ConfirmModel(confirm: .init(questions: [field], submit: "answer"), arms: false)
        #expect(!model.isComplete)
        model.setText("  ", for: field)
        #expect(!model.isComplete)
        model.setText("100%! <div> /resume", for: field)
        #expect(model.isComplete)
        #expect(model.answers["old"] == ["100%! <div> /resume"])
    }

    @Test func replacedFormSeedsNewDefaultsAndRetiresOldAnswers() throws {
        let original = try form("plain")
        let model = ConfirmModel(confirm: original, arms: false)
        model.setText("old draft", for: original.questions[0])
        model.update(try form("conditional"))
        #expect(model.answers["project_name"] == nil)
        #expect(model.answers["notify!"] == ["false"])
        model.arming?.cancel()
    }

    @Test func untouchedDefaultsDoNotCountAsUserEditsOnReplacement() throws {
        let model = ConfirmModel(confirm: try form("plain"), arms: false)
        model.update(try form("conditional"))
        #expect(!model.changed)
        model.arming?.cancel()
    }
}

private final class FormFixtureBundle: NSObject {}
