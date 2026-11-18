import Testing
import TogoKit
@testable import Togo

@MainActor struct OnboardingPasswordTests {
    @Test(arguments: ["", " ", "example-secret"])
    func formOnlyPassesAPasswordWhenOneWasEntered(_ password: String) async {
        let services = FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.ready)])
        let flow = AddMachineFlow(services: services, save: { _ in }, finish: { _ in })
        flow.choose(OnboardingSamples.hosts[0])
        flow.draft.user = "sam"
        flow.draft.auth = .password
        flow.draft.password = password
        await flow.runAttempt()
        #expect(services.passwords.count == 1)
        #expect(services.passwords[0] == (password.isEmpty ? nil : password))
    }

    @Test(arguments: ["", " ", "example-secret"])
    func anEmptyInitialPasswordAsksInsteadOfSendingAnEmptySecret(_ password: String) async {
        let prompter = OnboardingPrompter(password: password, answers: nil, trustNewKey: { _ in false })
        let answer = await prompter.answer(.password(user: "sam", host: "example.test"))
        if password.isEmpty {
            if case .secret = answer { Issue.record("Empty form field became a password offer") }
            #expect(answer == nil)
            #expect(prompter.declined)
        } else {
            guard case .secret(let value) = answer else { Issue.record("Entered password was not offered"); return }
            #expect(value == password)
        }
    }
}
