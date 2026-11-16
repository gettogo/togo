import Foundation
import Testing
import TogoKit
@testable import Togo

struct ConnectOutcomeTests {
    @Test func declinedPromptAfterRefusedKeyIsAuthFailure() {
        #expect(LiveOnboardingServices.outcome(for: SSHError.cancelled, promptDeclined: true) == .authFailed)
        #expect(LiveOnboardingServices.outcome(for: SSHError.authFailed(tried: ["publickey"]), promptDeclined: false) == .authFailed)
    }

    @Test func userCancelStaysCancel() {
        #expect(LiveOnboardingServices.outcome(for: SSHError.cancelled, promptDeclined: false) == .cancelled)
        #expect(LiveOnboardingServices.outcome(for: SSHError.hostKeyRejected, promptDeclined: false) == .cancelled)
    }

    @Test func timeoutIsUnreachableAndOtherFailuresKeepTheirReason() {
        #expect(LiveOnboardingServices.outcome(for: SSHError.timeout, promptDeclined: false) == .unreachable)
        let refused = SSHError.unreachable("Connection refused")
        #expect(LiveOnboardingServices.outcome(for: refused, promptDeclined: false) == .failed("Connection refused."))
    }

    @Test func changedKeyIsReportedNeverTrusted() {
        let error = SSHError.hostKeyChanged(host: "[mac]:22", expected: ["SHA256:a"], presented: "SHA256:b")
        #expect(LiveOnboardingServices.outcome(for: error, promptDeclined: false)
            == .hostKeyChanged(ChangedHostKey(hostID: "[mac]:22", trusted: ["SHA256:a"], presented: "SHA256:b")))
    }

    @MainActor @Test func acceptedKeyThenCodeAsksForTheCodeBeforeThePublicKey() async {
        let request = OnboardingSamples.codeRequest
        let services = FakeOnboardingServices(outcomes: [.needsAnswer(request), .needsAnswer(request), .ready(OnboardingSamples.ready)])
        let flow = AddMachineFlow(services: services, save: { _ in }, finish: { _ in })
        flow.choose(OnboardingSamples.hosts[0])
        flow.draft.user = "sam"
        flow.connect()
        await flow.runAttempt()
        #expect(flow.phase == .signIn(request, error: nil))

        flow.answer(["000000"])
        await flow.runAttempt()
        #expect(flow.phase == .signIn(request, error: AddMachineFlow.answerRefused))

        flow.answer(["123456"])
        await flow.runAttempt()
        #expect(flow.phase == .helper(OnboardingSamples.ready))
        #expect(services.answered == [nil, ["000000"], ["123456"]])
    }

    @MainActor @Test func connectedMachineTicksTheChecklist() async {
        let services = FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.ready)])
        let flow = AddMachineFlow(services: services, save: { _ in }, finish: { _ in })
        flow.choose(OnboardingSamples.hosts[0])
        flow.draft.user = "sam"
        var detected: [SetupDetection] = []
        flow.onDetected = { detected.append($0) }
        flow.connect()
        await flow.runAttempt()
        #expect(detected == [SetupDetection(keyAccepted: true, report: OnboardingSamples.ready)])
    }

    @Test func declinedCodePromptMapsToNeedsAnswer() {
        let request = OnboardingSamples.codeRequest
        #expect(LiveOnboardingServices.outcome(for: SSHError.cancelled, promptDeclined: true, request: request) == .needsAnswer(request))
        #expect(
            LiveOnboardingServices.outcome(for: SSHError.authFailed(tried: ["publickey", "keyboard-interactive"]), promptDeclined: true, request: request)
                == .needsAnswer(request)
        )
        #expect(LiveOnboardingServices.outcome(for: SSHError.authFailed(tried: ["publickey"]), promptDeclined: false, request: nil) == .authFailed)
    }

    @Test func signInRequestKeepsTheServersWords() throws {
        let code = OnboardingSamples.codeRequest
        #expect(code.kind == .code)
        #expect(code.title == "MacBook is asking for a code")
        #expect(code.instruction == "Duo two-factor login")
        #expect(code.fields == [SignInRequest.Field(label: "Verification code:", secret: true)])

        let kiPassword = try #require(SignInRequest(
            .keyboardInteractive(name: "", instruction: "", prompts: [KIPrompt(text: "Password:", echo: false)]), machine: "MacBook"
        ))
        #expect(kiPassword.kind == .password)
        #expect(kiPassword.title == "MacBook is asking for its password")
        if case .responses(let values) = kiPassword.answer(["pw"]) { #expect(values == ["pw"]) } else { Issue.record("expected responses") }

        let password = try #require(SignInRequest(.password(user: "sam", host: "macbook.local"), machine: "MacBook"))
        if case .secret(let value) = password.answer(["pw"]) { #expect(value == "pw") } else { Issue.record("expected a secret") }
        #expect(SignInRequest(.passphrase(keyLabel: "k"), machine: "MacBook") == nil)
    }
}

