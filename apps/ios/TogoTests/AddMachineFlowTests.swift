import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor
struct AddMachineFlowTests {
    /// The key command makes ~/.ssh, appends the key on its own line with sshd's permissions,
    /// and quotes the key so its comment cannot run as shell.
    @Test func publicKeyCommandAppendsWithSafePermissions() {
        let command = PublicKeyCommand.append("ssh-ed25519 AAAAC3Nza sam's iPhone\n")
        #expect(command.hasPrefix("mkdir -p ~/.ssh && chmod 700 ~/.ssh && printf '%s\\n' "))
        #expect(command.hasSuffix(" >> ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys"))
        #expect(command.contains(#"'ssh-ed25519 AAAAC3Nza sam'\''s iPhone'"#))
        #expect(!command.contains("\n'"))
    }

    private func flow(_ services: FakeOnboardingServices, saved: SavedRecords = SavedRecords()) -> AddMachineFlow {
        let flow = AddMachineFlow(
            services: services, save: { saved.records.append($0) }, finish: { saved.finished.append($0.id) }
        )
        flow.choose(OnboardingSamples.hosts[0])
        flow.draft.user = "sam"
        return flow
    }

    @Test func invalidFormStaysOnFormWithFieldErrors() {
        let flow = flow(FakeOnboardingServices())
        flow.draft.port = "0"
        flow.draft.user = ""
        flow.connect()
        #expect(flow.path == [.form])
        #expect(flow.showsErrors)
        #expect(Set(flow.draft.errors.keys) == [.port, .user])
    }

    @Test func discoveredMachinePrefillsTheForm() {
        let flow = flow(FakeOnboardingServices())
        #expect(flow.draft.name == "MacBook Pro")
        #expect(flow.draft.address == "192.168.1.20")
        #expect(flow.draft.port == "22")
        #expect(flow.draft.symbol == .laptop)
    }

    /// A machine found with no usable address opens the form with the address empty, so the
    /// form asks for it instead of saving something that only works on one network.
    @Test func machineFoundWithoutAnAddressAsksForOne() {
        let flow = AddMachineFlow(services: FakeOnboardingServices(), save: { _ in }, finish: { _ in })
        flow.choose(DiscoveredHost(id: "mac", name: "Sam's Mac", host: "", port: 22, tailnet: false))
        flow.draft.user = "sam"
        flow.connect()
        #expect(flow.draft.errors.keys.contains(.address))
    }

    @Test func zonedAddressExplainsItself() {
        var draft = MachineDraft()
        draft.address = "fe80::1c2b%en0"
        #expect(draft.addressNote != nil)
        draft.address = "studio.local"
        #expect(draft.addressNote == nil)
    }

    @Test func newHostKeyWaitsForTheUser() async {
        let services = FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.ready)], newKey: OnboardingSamples.hostKey)
        let flow = flow(services)
        flow.connect()
        let attempt = Task { await flow.runAttempt() }
        while flow.phase != .hostKeyNew(OnboardingSamples.hostKey) { await Task.yield() }
        flow.answerTrust(true)
        await attempt.value
        #expect(services.trustAnswers == [true])
        #expect(flow.phase == .helper(OnboardingSamples.ready))
    }

    @Test func refusedHostKeyReturnsToTheForm() async {
        let services = FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.ready)], newKey: OnboardingSamples.hostKey)
        let flow = flow(services)
        flow.connect()
        let attempt = Task { await flow.runAttempt() }
        while !flow.phase.isQuestion { await Task.yield() }
        flow.answerTrust(false)
        await attempt.value
        #expect(services.trustAnswers == [false])
        #expect(flow.path == [.form])
    }

    @Test func changedHostKeyIsOnlyForgottenOnRequest() async {
        let services = FakeOnboardingServices(outcomes: [.hostKeyChanged(OnboardingSamples.changedKey), .unreachable])
        let flow = flow(services)
        flow.connect()
        await flow.runAttempt()
        #expect(flow.phase == .hostKeyChanged(OnboardingSamples.changedKey))
        #expect(services.forgotten.isEmpty)

        await flow.forgetChangedKey()
        #expect(services.forgotten == ["[192.168.1.20]:22"])
        #expect(flow.phase == .connecting)
    }

    @Test func refusedDeviceKeyShowsThePublicKey() async {
        let flow = flow(FakeOnboardingServices(outcomes: [.authFailed]))
        flow.connect()
        await flow.runAttempt()
        #expect(flow.phase == .keyNotAuthorised(publicKey: OnboardingSamples.deviceKey.publicKeyLine))
    }

    @Test func refusedPasswordDoesNotOfferAKey() async {
        let flow = flow(FakeOnboardingServices(outcomes: [.authFailed]))
        flow.draft.auth = .password
        flow.connect()
        await flow.runAttempt()
        guard case .failed = flow.phase else {
            Issue.record("expected a failure, got \(flow.phase)")
            return
        }
    }

    @Test func useWithoutHelperSavesNoHelper() async {
        let saved = SavedRecords()
        let flow = flow(FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.noHelper)]), saved: saved)
        flow.connect()
        await flow.runAttempt()
        await flow.save(withHelper: false)
        #expect(saved.records.count == 1)
        #expect(saved.records.first?.noHelper == true)
        #expect(saved.records.first?.profile.endpoint == HostEndpoint(host: "192.168.1.20", port: 22, user: "sam"))
        #expect(saved.records.first?.profile.auth == .key(id: OnboardingSamples.deviceKey.id))
        #expect(saved.finished == saved.records.map(\.id))
    }

    @Test func saveWithHelperKeepsItsPath() async {
        let saved = SavedRecords()
        let flow = flow(FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.ready)]), saved: saved)
        flow.connect()
        await flow.runAttempt()
        await flow.save(withHelper: true)
        #expect(saved.records.first?.noHelper == false)
        #expect(saved.records.first?.helperPath == "/Users/sam/.togo/bin/togo")
    }

    /// Notifications are offered after the save, as their own step; only "Turn on" asks iOS.
    @Test func savedWithHelperOffersNotificationsBeforeClosing() async {
        let saved = SavedRecords()
        var turnedOn = 0
        let flow = flow(FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.ready)]), saved: saved)
        flow.notificationAsk = .init(needed: { true }, turnOn: { turnedOn += 1 })
        flow.connect()
        await flow.runAttempt()
        await flow.save(withHelper: true)
        #expect(saved.records.count == 1, "the machine is saved before the question")
        #expect(flow.path.last == .notifications)
        #expect(saved.finished.isEmpty)
        #expect(turnedOn == 0, "showing the question does not ask iOS")

        await flow.answerNotifications(turnOn: true)
        #expect(turnedOn == 1)
        #expect(saved.finished == saved.records.map(\.id))
    }

    @Test func notNowClosesWithoutAsking() async {
        let saved = SavedRecords()
        var turnedOn = 0
        let flow = flow(FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.ready)]), saved: saved)
        flow.notificationAsk = .init(needed: { true }, turnOn: { turnedOn += 1 })
        flow.connect()
        await flow.runAttempt()
        await flow.save(withHelper: true)
        await flow.answerNotifications(turnOn: false)
        #expect(turnedOn == 0)
        #expect(saved.finished == saved.records.map(\.id))
    }

    /// A second tap while the system alert is up neither asks again nor closes twice.
    @Test func turnOnTappedTwiceAsksOnce() async {
        let saved = SavedRecords()
        var turnedOn = 0
        let flow = flow(FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.ready)]), saved: saved)
        flow.notificationAsk = .init(needed: { true }, turnOn: {
            turnedOn += 1
            await Task.yield()
        })
        flow.connect()
        await flow.runAttempt()
        await flow.save(withHelper: true)
        let first = Task { await flow.answerNotifications(turnOn: true) }
        let second = Task { await flow.answerNotifications(turnOn: true) }
        await first.value
        await second.value
        #expect(turnedOn == 1)
        #expect(saved.finished.count == 1)
    }

    /// iOS reports Local Network as denied while its alert is up; allowing it brings the search
    /// back even when no machine has answered yet.
    @Test func discoveryRecoversWhenLocalNetworkIsAllowed() async {
        let services = FakeOnboardingServices(events: [])
        let flow = AddMachineFlow(services: services, save: { _ in }, finish: { _ in })
        let browsing = Task { await flow.discover() }
        while services.discoveryFeed == nil { await Task.yield() }
        services.discoveryFeed?.yield(.denied)
        while flow.discovery != .denied { await Task.yield() }
        services.discoveryFeed?.yield(.browsing)
        while flow.discovery != .searching { await Task.yield() }
        #expect(flow.hosts.isEmpty)
        browsing.cancel()
        await browsing.value
    }

    /// A port typed into the address, or an IPv6 address in brackets, as copied from a URL.
    @Test func addressWithAPortIsCaughtAndBracketsAreDropped() {
        var draft = MachineDraft()
        draft.name = "Studio"
        draft.user = "sam"
        for typed in ["studio.local:2222", "[fd7a:115c:a1e0::1]:22"] {
            draft.address = typed
            #expect(draft.errors[.address] != nil, "\(typed)")
        }
        draft.address = " [fd7a:115c:a1e0::1] "
        #expect(draft.errors[.address] == nil)
        #expect(draft.profile(id: UUID(), deviceKeyID: "key")?.endpoint.host == "fd7a:115c:a1e0::1")
        draft.address = "fe80::1c2b%en0"
        #expect(draft.errors[.address] == nil)
        #expect(MachineDraft.endpoint("sam@jump@bastion") == nil)
    }

    @Test func noNotificationQuestionWithoutHelperOrOnceAnswered() async {
        for (outcome, withHelper, needed) in [
            (OnboardingSamples.noHelper, false, true), (OnboardingSamples.ready, true, false),
        ] {
            let saved = SavedRecords()
            let flow = flow(FakeOnboardingServices(outcomes: [.ready(outcome)]), saved: saved)
            flow.notificationAsk = .init(needed: { needed }, turnOn: {})
            flow.connect()
            await flow.runAttempt()
            await flow.save(withHelper: withHelper)
            #expect(!flow.path.contains(.notifications))
            #expect(saved.finished == saved.records.map(\.id), "the sheet closes straight after saving")
        }
    }

    private func passwordFlow(_ services: FakeOnboardingServices, saved: SavedRecords) -> AddMachineFlow {
        let flow = flow(services, saved: saved)
        flow.draft.auth = .password
        flow.draft.password = "hunter2"
        return flow
    }

    @Test func passwordIsSavedOnThisDeviceByDefault() async throws {
        let services = FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.ready)])
        let saved = SavedRecords()
        let flow = passwordFlow(services, saved: saved)
        #expect(flow.draft.savePassword)
        flow.connect()
        await flow.runAttempt()
        await flow.save(withHelper: true)
        let id = try #require(saved.records.first?.id)
        #expect(services.savedPasswords == [SavedPassword(machine: id, password: "hunter2")])
    }

    @Test func passwordNotKeptWhenTheUserSaysNo() async {
        let services = FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.ready)])
        let saved = SavedRecords()
        let flow = passwordFlow(services, saved: saved)
        flow.draft.savePassword = false
        flow.connect()
        await flow.runAttempt()
        await flow.save(withHelper: true)
        #expect(services.savedPasswords.map(\.password) == [nil])
    }

    @Test func keyMachineForgetsAnyOldPassword() async {
        let services = FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.ready)])
        let flow = flow(services)
        flow.connect()
        await flow.runAttempt()
        await flow.save(withHelper: true)
        #expect(services.savedPasswords.map(\.password) == [nil])
    }

    @Test func editingWithTheFieldEmptyKeepsTheSavedPassword() async {
        let services = FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.ready)])
        let saved = SavedRecords()
        let flow = passwordFlow(services, saved: saved)
        flow.draft.password = ""
        flow.connect()
        await flow.runAttempt()
        await flow.save(withHelper: true)
        #expect(saved.records.count == 1)
        #expect(services.savedPasswords.isEmpty)
    }

    @Test func editingAMachineWhoseKeyIsOnAnotherDeviceAsksForAKey() {
        let id = UUID()
        let record = HostRecord(
            id: id,
            profile: HostProfile(id: id, name: "Studio", endpoint: HostEndpoint(host: "studio.local", user: "sam"), auth: .key(id: "elsewhere")),
            symbol: "desktop"
        )
        let store = AppStore(services: FakeAppServices())
        let services = FakeOnboardingServices()
        let flow = AddMachineFlow.editing(record, store: store, services: services, tutorial: nil)
        #expect(flow.draft.auth == .importedKey)
        #expect(flow.draft.importedKey == nil)
        #expect(flow.draft.errors[.key] != nil)

        services.keyLabels = ["elsewhere": "Work laptop key"]
        let known = AddMachineFlow.editing(record, store: store, services: services, tutorial: nil)
        #expect(known.draft.importedKey == ImportedKey(id: "elsewhere", label: "Work laptop key"))
        #expect(known.startsOnForm && known.path.isEmpty, "the form is the first screen, so Back has nowhere to go")
        #expect(known.notificationAsk == nil, "editing never asks about notifications")
    }

    @Test func jumpHostParses() {
        #expect(MachineDraft.endpoint("sam@bastion") == HostEndpoint(host: "bastion", port: 22, user: "sam"))
        #expect(MachineDraft.endpoint("sam@bastion:2200") == HostEndpoint(host: "bastion", port: 2200, user: "sam"))
        #expect(MachineDraft.endpoint("sam@[fd7a:115c:a1e0::1]:22") == HostEndpoint(host: "fd7a:115c:a1e0::1", port: 22, user: "sam"))
        #expect(MachineDraft.endpoint("bastion") == nil)
        #expect(MachineDraft.endpoint("sam@bastion:0") == nil)
    }
}

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

@MainActor
struct CopyableTextTests {
    @Test func breaksOnlyInsideLongTokens() {
        let rendered = CopyableText.breakable("curl -fsSL https://example.com/a/very/long/path | sh")
        #expect(rendered.hasPrefix("curl -fsSL h\u{200B}t"))
        #expect(rendered.hasSuffix(" | sh"))
        #expect(rendered.replacingOccurrences(of: "\u{200B}", with: "") == "curl -fsSL https://example.com/a/very/long/path | sh")
    }
}

@MainActor final class SavedRecords {
    var records: [HostRecord] = []
    var finished: [UUID] = []
}

@MainActor
struct SignInModelTests {
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
        let model = SignInModel(hostID: Samples.studio, machine: "Studio", services: services) {}
        let run = Task { await model.run() }
        while case .connecting = model.phase { await Task.yield() }
        model.cancel()
        await run.value
        #expect(services.signInAnswers.count == 1)
        if case .some(.some) = services.signInAnswers.first { Issue.record("expected no answer") }
        guard case .failed = model.phase else {
            Issue.record("expected failed, got \(model.phase)")
            return
        }
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
