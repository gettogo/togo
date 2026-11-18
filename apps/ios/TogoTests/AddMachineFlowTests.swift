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
        #expect(flow.path.last == .connect, "the question is asked on its own screen")
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
        #expect(saved.records.isEmpty, "a missing helper is the user's decision")
        #expect(flow.path.last == .connect)
        await flow.save(withHelper: false)
        #expect(saved.records.count == 1)
        #expect(saved.records.first?.noHelper == true)
        #expect(saved.records.first?.profile.endpoint == HostEndpoint(host: "192.168.1.20", port: 22, user: "sam"))
        #expect(saved.records.first?.profile.auth == .key(id: OnboardingSamples.deviceKey.id))
        #expect(saved.finished == saved.records.map(\.id))
    }

    /// The checkmark finishes the job: a machine with the helper is saved without a second screen.
    @Test func unreadableHelperVersionRequiresRecoveryBeforeSaving() async {
        let saved = SavedRecords()
        let report = HelperReport(helper: .outdated(""), path: "/opt/bin/togo", tmux: "tmux 3.5a", os: "Darwin")
        let flow = flow(FakeOnboardingServices(outcomes: [.ready(report)]), saved: saved)
        flow.connect()
        await flow.runAttempt()
        #expect(saved.records.isEmpty)
        #expect(saved.finished.isEmpty)
        #expect(flow.path.last == .connect)
        #expect(flow.phase == .helper(report))
    }

    @Test func successSavesWithoutPushingAScreen() async {
        let saved = SavedRecords()
        let flow = flow(FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.ready)]), saved: saved)
        flow.connect()
        #expect(flow.path == [.form], "testing happens in place")
        #expect(flow.isWorking)
        await flow.runAttempt()
        #expect(flow.path == [.form])
        #expect(saved.records.count == 1)
        #expect(saved.finished == saved.records.map(\.id))
        #expect(!flow.isWorking)
    }

    @Test func missingHelperPushesTheHelperScreen() async {
        let saved = SavedRecords()
        let flow = flow(FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.noHelper)]), saved: saved)
        flow.connect()
        await flow.runAttempt()
        #expect(flow.path == [.form, .connect])
        #expect(saved.records.isEmpty)
    }

    @Test func oldTmuxPushesTheHelperScreen() async {
        let old = HelperReport(helper: .present(version: "0.3.0"), path: "/x/togo", tmux: "tmux 3.0", os: "Linux")
        let saved = SavedRecords()
        let flow = flow(FakeOnboardingServices(outcomes: [.ready(old)]), saved: saved)
        flow.connect()
        await flow.runAttempt()
        #expect(flow.path == [.form, .connect])
        #expect(saved.records.isEmpty)
    }

    @Test func failuresPushTheirScreen() async {
        let flow = flow(FakeOnboardingServices(outcomes: [.unreachable]))
        flow.connect()
        await flow.runAttempt()
        #expect(flow.path == [.form, .connect])
        #expect(flow.phase == .unreachable)
    }

    /// Editing a machine saved without the helper does not ask about it again.
    @Test func editingANoHelperMachineSavesWithoutAsking() async {
        let saved = SavedRecords()
        let flow = flow(FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.noHelper)]), saved: saved)
        flow.keepsNoHelper = true
        flow.connect()
        await flow.runAttempt()
        #expect(flow.path == [.form])
        #expect(saved.records.count == 1)
        #expect(saved.records.first?.noHelper == true)
        #expect(saved.finished == saved.records.map(\.id))
    }

    @Test func addedMachineWithTheHelperLeavesANotice() async {
        let store = AppStore(services: FakeAppServices())
        store.sheet = .addMachine
        let flow = AddMachineFlow.adding(store: store, services: FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.ready)]))
        flow.choose(OnboardingSamples.hosts[0])
        flow.draft.user = "sam"
        flow.connect()
        await flow.runAttempt()
        #expect(store.sheet == nil)
        #expect(store.notice?.text.hasSuffix("added. Set up its agents in Settings › Agents.") == true)
    }

    @Test func storedMachineRemembersItWasSavedWithoutTheHelper() {
        let id = UUID()
        let record = HostRecord(
            id: id,
            profile: HostProfile(id: id, name: "Studio", endpoint: HostEndpoint(host: "studio.local", user: "sam"), auth: .key(id: "elsewhere")),
            symbol: "desktop", noHelper: true
        )
        let flow = AddMachineFlow.editing(record, store: AppStore(services: FakeAppServices()), services: FakeOnboardingServices(), tutorial: nil)
        #expect(flow.keepsNoHelper && flow.isEditing)
    }

    /// A saved machine has nothing to save until a field changes, so ✓ and ✕ never do the same thing.
    @Test func editingHasNothingToSaveUntilAFieldChanges() {
        let id = UUID()
        let record = HostRecord(
            id: id,
            profile: HostProfile(id: id, name: "Studio", endpoint: HostEndpoint(host: "studio.local", user: "sam"), auth: .password),
            symbol: "desktop"
        )
        let flow = AddMachineFlow.editing(record, store: AppStore(services: FakeAppServices()), services: FakeOnboardingServices(), tutorial: nil)
        #expect(!flow.hasChanges)
        flow.draft.name = "Studio Mac"
        #expect(flow.hasChanges)
        flow.draft.name = "Studio"
        #expect(!flow.hasChanges, "changing a field back leaves nothing to save")
    }

    /// Check again on the helper screen saves and closes once the helper is found.
    @Test func retryThatFindsTheHelperSaves() async {
        let saved = SavedRecords()
        let flow = flow(
            FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.noHelper), .ready(OnboardingSamples.ready)]), saved: saved
        )
        flow.connect()
        await flow.runAttempt()
        #expect(saved.records.isEmpty)
        flow.retry()
        await flow.runAttempt()
        #expect(saved.records.count == 1)
        #expect(saved.records.first?.noHelper == false)
        #expect(saved.finished == saved.records.map(\.id))
    }

    @Test func saveWithHelperKeepsItsPath() async {
        let saved = SavedRecords()
        let flow = flow(FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.ready)]), saved: saved)
        flow.connect()
        await flow.runAttempt()
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
            if !withHelper { await flow.save(withHelper: false) }
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
        #expect(services.savedPasswords.map(\.password) == [nil])
    }

    @Test func keyMachineForgetsAnyOldPassword() async {
        let services = FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.ready)])
        let flow = flow(services)
        flow.connect()
        await flow.runAttempt()
        #expect(services.savedPasswords.map(\.password) == [nil])
    }

    @Test func editingWithTheFieldEmptyKeepsTheSavedPassword() async {
        let services = FakeOnboardingServices(outcomes: [.ready(OnboardingSamples.ready)])
        let saved = SavedRecords()
        let flow = passwordFlow(services, saved: saved)
        flow.draft.password = ""
        flow.connect()
        await flow.runAttempt()
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
