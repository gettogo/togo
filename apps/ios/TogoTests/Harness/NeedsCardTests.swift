import Foundation
import Testing
import TogoKit
@testable import Togo

/// Needs cards that can always be acted on: no Approve or Deny for a request the phone cannot
/// answer, a specific reason for every failed answer, read-only questions for harnesses with no
/// answer route, and the prompt bar keeping what the chat does not draw.
@MainActor
struct NeedsCardTests {
    private let approval = HarnessItem.Approval(requestId: "r1")

    private func card(options: [ApprovalOption], allowText: Bool = false) -> RequestCard {
        RequestCard(
            id: "r1", hostID: Samples.studio, sessionID: "s-04", bib: "04", agentName: "Claude Code",
            title: "Claude needs your permission", detail: nil, options: options, allowText: allowText, phase: .open
        )
    }

    // MARK: Approvals

    @Test func aRequestWithNoOptionsAndNoAskWaitsInTheTerminal() {
        #expect(ApprovalRow.shape(approval: approval, card: card(options: [])) == .waiting)
        #expect(!ApprovalRow.answerable(card(options: [])))
    }

    @Test func aRequestWithOptionsOrWordsIsACard() {
        let yes = ApprovalOption(n: 1, label: "Yes", role: .approve)
        #expect(ApprovalRow.shape(approval: approval, card: card(options: [yes])) == .card)
        #expect(ApprovalRow.shape(approval: approval, card: card(options: [], allowText: true)) == .card)
        #expect(ApprovalRow.shape(approval: approval, card: nil) == .waiting)
    }

    @Test func answerFailuresSayWhy() {
        let refused = HelperError.rpc(RPCErrorObject(code: -32602, message: "approval offers no approve option"))
        let line = AppStore.answerFailure(refused, machine: "Studio")
        #expect(!line.contains("Couldn't reach"))
        #expect(AppStore.answerFailure(HelperError.closed, machine: "Studio") == ControlCenterCopy.answerFailed("Studio"))
        #expect(AppStore.answerFailure(HelperError.conflict("gone"), machine: "Studio") != ControlCenterCopy.answerFailed("Studio"))
    }

    // MARK: Questions

    private let question = HarnessItem.Question(questions: [
        HarnessQuestion(id: "q1", prompt: "Which?", options: [HarnessChoice(id: "a", label: "A")], multi: false, allowText: true),
    ])

    @Test func aKimiQuestionIsReadOnly() {
        var kimi = question
        kimi.answerable = false
        #expect(!QuestionRow.answerable(kimi))
        #expect(QuestionRow.answerable(question))
    }

    @Test func theQuestionMapsToAConfirm() {
        let confirm = QuestionRow.confirm(question)
        #expect(confirm.questions == question.questions)
        #expect(confirm.submit == ChatCopy.send)
    }

    @Test func aFailedAnswerKeepsTheHelpersMessage() async {
        let model = ConfirmModel(confirm: QuestionRow.confirm(question), armDelay: .zero, arms: false)
        #expect(!model.armed)
        model.armNow()
        model.toggle("a", in: question.questions[0])
        let shown = model.confirm
        await model.submit(for: shown) { _ in
            HarnessActionResult(result: .failed, message: "The question is not waiting at the terminal")
        }
        #expect(model.phase == .failed("The question is not waiting at the terminal"))
    }

    @Test func aFallbackSaysToFinishInTheTerminal() async {
        let model = ConfirmModel(confirm: QuestionRow.confirm(question), armDelay: .zero)
        model.armNow()
        model.toggle("a", in: question.questions[0])
        await model.submit(for: model.confirm) { _ in HarnessActionResult(result: .fallback) }
        #expect(model.handedOff)
        #expect(model.phase == .open)
        #expect(model.canSubmit)
        await model.submit(for: model.confirm) { _ in HarnessActionResult(result: .applied) }
        #expect(!model.handedOff)
    }

    @Test func otherAnswerIsTypedWords() {
        let model = ConfirmModel(confirm: QuestionRow.confirm(question), armDelay: .zero)
        model.setText("something else", for: question.questions[0])
        #expect(model.answers == ["q1": ["something else"]])
    }

    @Test func armingWaitsForTheCardToShow() async {
        let model = ConfirmModel(confirm: QuestionRow.confirm(question), armDelay: .zero, arms: false)
        #expect(model.arming == nil)
        model.beginArming()
        await model.arming?.value
        #expect(model.armed)
    }

    @Test func aStoreReportsAThrownAnswerAsAFailure() async {
        let chat = HarnessSessionStore(
            subject: SessionOpenTests.subject, services: FailingDescribe(), cached: ChatTests.claude
        )
        let item = HarnessItem(id: "q", seq: 1, at: WireDate(.now), body: .question(question))
        let result = await chat.answerReporting(item, args: ConfirmModel.args(["q1": ["a"]]))
        #expect(result.result == .failed)
        #expect(result.message == ServiceErrors.message(HelperError.closed))
    }

    // MARK: Prompt bar

    @Test func thePromptBarKeepsARequestTheChatDoesNotDraw() {
        var center = Samples.controlCenter
        center.machines[0].capabilities = [Capability.harness]
        let app = AppStore(
            services: FakeAppServices(), controlCenter: center,
            harnessServices: FakeHarnessServices(descriptor: ChatTests.claude)
        )
        defer { HarnessTabsRegistry.shared.release(app) }
        let row = center.machines[0].sessions[1]
        app.open(hostID: row.hostID, sessionID: row.id)
        let tab = TabID(hostID: row.hostID, sessionID: row.id)
        // Claude's descriptor here declares no needs.approvals and the timeline is empty.
        #expect(PromptFeed.requests(in: app, inlineIn: tab).map(\.id) == ["r1"])
    }

    @Test func thePromptBarLeavesOutWhatTheChatDraws() async throws {
        var center = Samples.controlCenter
        center.machines[0].capabilities = [Capability.harness]
        let described = HarnessDescriptor(
            adapter: .init(id: "claude", name: "Claude Code", harnessVersion: "2", revision: 1),
            capabilities: [HarnessCapability.chat, HarnessCapability.needsApprovals]
        )
        let app = AppStore(
            services: FakeAppServices(), controlCenter: center,
            harnessServices: FakeHarnessServices(descriptor: described)
        )
        defer { HarnessTabsRegistry.shared.release(app) }
        let row = center.machines[0].sessions[1]
        let tab = TabID(hostID: row.hostID, sessionID: row.id)
        let tabs = app.harnessTabs
        tabs.sync(tabs: [tab], capabilities: { _ in [Capability.harness] }, expectsChat: { _ in true })
        // Not described yet: nothing is drawn inline, so the bar keeps it.
        #expect(PromptFeed.requests(in: app, inlineIn: tab).map(\.id) == ["r1"])
        let clock = ContinuousClock()
        let deadline = clock.now + .seconds(3)
        while tabs.store(for: tab)?.descriptor == nil, clock.now < deadline {
            try await Task.sleep(for: .milliseconds(5))
        }
        // Described with needs.approvals: the chat answers it inline.
        #expect(PromptFeed.requests(in: app, inlineIn: tab).isEmpty)
        #expect(PromptFeed.requests(in: app).map(\.id) == ["r1"])
        tabs.stopAll()
    }
}
