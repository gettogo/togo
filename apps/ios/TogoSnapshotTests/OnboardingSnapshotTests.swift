import SnapshotTesting
import SwiftUI
import Testing
import TogoKit
@testable import Togo

@MainActor
struct OnboardingSnapshotTests {
    private let machine = "MacBook"

    @Test func firstRun() {
        assertScreen(FirstRunView {}, named: "first-run")
    }

    @Test func notificationAsk() {
        assertScreen(stack(NotificationAskView(machine: "Studio", onTurnOn: {}, onNotNow: {})), named: "notification-ask")
    }

    @Test func discoveryFound() {
        assertScreen(stack(discovery(OnboardingSamples.hosts, .searching), root: true), named: "discovery-found")
    }

    @Test func discoveryDiscovering() {
        assertScreen(stack(discovery([], .searching), root: true), named: "discovery-discovering")
    }

    @Test func discoveryEmpty() {
        assertScreen(stack(discovery([], .quiet), root: true), named: "discovery-empty")
    }

    @Test func discoveryPermissionDenied() {
        assertScreen(stack(discovery([], .denied), root: true), named: "discovery-denied")
    }

    @Test func formInvalid() {
        var draft = MachineDraft()
        draft.address = "sam@macbook.local"
        draft.port = "99999"
        assertScreen(stack(form(draft, showsErrors: true)), named: "form-invalid")
    }

    @Test func formFilled() {
        assertScreen(stack(form(OnboardingSamples.draft, showsErrors: false)), named: "form-filled")
    }

    @Test func formPassword() {
        var draft = OnboardingSamples.draft
        draft.auth = .password
        draft.password = "hunter2"
        assertScreen(stack(form(draft, showsErrors: false)), named: "form-password")
    }

    @Test func signInCode() {
        assertScreen(stack(phase(.signIn(OnboardingSamples.codeRequest, error: nil))), named: "sign-in-code")
    }

    @Test func signInCodeRefused() {
        assertScreen(stack(phase(.signIn(OnboardingSamples.codeRequest, error: AddMachineFlow.answerRefused))), named: "sign-in-refused")
    }

    @Test func liveSignInSheet() {
        assertScreen(signInSheet(.asking(OnboardingSamples.codeRequest)), named: "live-sign-in")
    }

    @Test func liveSignInConnecting() {
        assertScreen(signInSheet(.connecting), named: "live-sign-in-connecting")
    }

    @Test func liveSignInFailed() {
        assertScreen(signInSheet(.failed("Permission denied (publickey,password).")), named: "live-sign-in-failed")
    }

    private func signInSheet(_ phase: SignInModel.Phase) -> some View {
        NavigationStack {
            SignInPhaseView(machine: machine, phase: phase, onSubmit: { _ in }, onRetry: {})
                .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Cancel") {} } }
        }
    }

    @Test func connecting() {
        assertScreen(stack(phase(.connecting)), named: "connecting")
    }

    @Test func hostKeyNew() {
        assertScreen(stack(phase(.hostKeyNew(OnboardingSamples.hostKey))), named: "host-key-new")
    }

    @Test func hostKeyChanged() {
        assertScreen(stack(phase(.hostKeyChanged(OnboardingSamples.changedKey))), named: "host-key-changed")
    }

    @Test func keyNotAuthorised() {
        assertScreen(stack(phase(.keyNotAuthorised(publicKey: OnboardingSamples.deviceKey.publicKeyLine))), named: "key-not-authorised")
    }

    @Test func helperPresent() {
        assertScreen(stack(phase(.helper(OnboardingSamples.ready))), named: "helper-present")
    }

    @Test func helperPresentOverTailscaleSSH() {
        var report = OnboardingSamples.ready
        report.tailscaleSSH = true
        report.mosh = false
        assertScreen(stack(phase(.helper(report))), named: "helper-tailscale")
    }

    @Test func helperMissing() {
        assertScreen(stack(phase(.helper(OnboardingSamples.noHelper))), named: "helper-missing")
    }

    @Test func unreachable() {
        assertScreen(stack(phase(.unreachable)), named: "unreachable")
    }

    // MARK: Builders

    /// Only the discovery root has Cancel; pushed steps show the system back button, which a
    /// stack without history does not draw.
    private func stack(_ view: some View, root: Bool = false) -> some View {
        NavigationStack {
            view.toolbar {
                if root {
                    ToolbarItem(placement: .cancellationAction) { Button("Cancel") {} }
                }
            }
        }
        .togoControls()
    }

    private func discovery(_ hosts: [DiscoveredHost], _ phase: DiscoveryPhase) -> some View {
        DiscoveryList(hosts: hosts, phase: phase, onChoose: { _ in }, onEnterAddress: {})
    }

    private func form(_ draft: MachineDraft, showsErrors: Bool) -> some View {
        MachineForm(
            draft: .constant(draft), showsErrors: showsErrors, importError: nil, onImportKey: { _, _ in }, onConnect: {}
        )
    }

    private func phase(_ phase: ConnectPhase) -> some View {
        ConnectPhaseView(
            machine: machine, phase: phase, saving: false, saveError: nil,
            actions: .init(trust: {}, cancel: {}, back: {}, forget: {}, retry: {}, save: {}, skipHelper: {})
        )
    }
}
