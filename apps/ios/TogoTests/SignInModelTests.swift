import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor
struct SignInModelTests {
    @Test func successAfterCancelDoesNotFinishAReplacementSheet() async {
        let services = FakeAppServices()
        var finished = false
        let model = SignInModel(hostID: Samples.studio, machine: "Studio", services: services) { finished = true }
        services.beforeSignInPrompts = { model.cancel() }
        await model.run()
        #expect(!finished)
    }

    @Test func cancelledTaskNeverStartsOrFinishesSignIn() async {
        let services = FakeAppServices()
        var started = false
        var finished = false
        services.beforeSignInPrompts = { started = true }
        let model = SignInModel(hostID: Samples.studio, machine: "Studio", services: services) { finished = true }
        let run = Task { await model.run() }
        run.cancel()
        await run.value
        #expect(!started && !finished)
    }

    @Test func liveSignInPutsTheCodePromptToTheUser() async throws {
        let services = FakeAppServices()
        services.signInPrompts = [OnboardingSamples.codePrompt]
        var finished = false
        let model = SignInModel(hostID: Samples.studio, machine: "Studio", services: services) { finished = true }
        let run = Task { await model.run() }
        while case .connecting = model.phase { await Task.yield() }
        guard case .asking(let request) = model.phase else {
            Issue.record("expected a question, got \(model.phase)")
            return
        }
        #expect(request.title == "Studio is asking for a code")
        model.submit(["123456"])
        await run.value
        #expect(finished)
        guard case .responses(let values)?? = services.signInAnswers.first else {
            Issue.record("expected responses")
            return
        }
        #expect(values == ["123456"])
    }

    @Test func cancellingTheSheetEndsTheAttempt() async {
        let services = FakeAppServices()
        services.signInPrompts = [OnboardingSamples.codePrompt]
        var finished = false
        let model = SignInModel(hostID: Samples.studio, machine: "Studio", services: services) { finished = true }
        let run = Task { await model.run() }
        while case .connecting = model.phase { await Task.yield() }
        model.cancel()
        await run.value
        #expect(services.signInAnswers.count == 1)
        if case .some(.some) = services.signInAnswers.first { Issue.record("expected no answer") }
        #expect(!finished)
    }

    /// Sign-in goes on after the sheet closes; a question it asks then is declined at once
    /// rather than waiting for an answer nobody can give.
    @Test(.timeLimit(.minutes(1))) func questionAfterTheSheetClosedIsDeclined() async {
        let services = FakeAppServices()
        services.signInPrompts = [OnboardingSamples.codePrompt]
        let model = SignInModel(hostID: Samples.studio, machine: "Studio", services: services) {}
        services.beforeSignInPrompts = { model.cancel() }
        await model.run()
        #expect(services.signInAnswers.count == 1)
        if case .some(.some) = services.signInAnswers.first { Issue.record("expected no answer") }
    }
}
