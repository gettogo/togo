import SwiftUI

/// The add-machine sheet: discovery, the form, then the connection step, in one stack.
struct AddMachineSheet: View {
    @Environment(\.dismiss) private var dismiss
    @State private var flow: AddMachineFlow

    init(flow: AddMachineFlow) {
        _flow = State(initialValue: flow)
    }

    var body: some View {
        NavigationStack(path: $flow.path) {
            DiscoveryList(
                hosts: flow.hosts, phase: flow.discovery,
                onChoose: flow.choose, onEnterAddress: flow.enterAddress
            )
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button(String(localized: "onboard.cancel", defaultValue: "Cancel")) { dismiss() }
                }
            }
            .navigationDestination(for: AddMachineFlow.Route.self) { route in
                switch route {
                case .form:
                    MachineForm(
                        draft: $flow.draft, showsErrors: flow.showsErrors, importError: flow.importError,
                        onImportKey: { text, label in Task { await flow.importKey(text, label: label) } },
                        onConnect: flow.connect
                    )
                case .connect:
                    ConnectStep(flow: flow)
                        .task(id: flow.attempt) { await flow.runAttempt() }
                }
            }
        }
        .task { await flow.discover() }
        .interactiveDismissDisabled(!flow.path.isEmpty)
        .onDisappear { flow.cancel() }
        .togoControls()
    }
}

extension AddMachineSheet {
    /// Saves through the app's services and closes the sheet once the machine is stored.
    init(store: AppStore, services: any OnboardingServices, tutorial: TutorialModel? = nil) {
        let flow = AddMachineFlow(
            services: services,
            save: { try await store.services.saveHost($0) },
            finish: { _ in store.sheet = nil }
        )
        if let tutorial { flow.onDetected = { tutorial.record($0) } }
        self.init(flow: flow)
    }
}

/// Whatever the current connection attempt is waiting on.
struct ConnectStep: View {
    let flow: AddMachineFlow

    var body: some View {
        ConnectPhaseView(
            machine: flow.machineName, phase: flow.phase, saving: flow.saving, saveError: flow.saveError,
            actions: .init(
                trust: { flow.answerTrust(true) },
                cancel: { flow.answerTrust(false) },
                back: { flow.path.removeLast() },
                forget: { Task { await flow.forgetChangedKey() } },
                retry: flow.retry,
                save: { Task { await flow.save(withHelper: true) } },
                skipHelper: { Task { await flow.save(withHelper: false) } },
                answer: flow.answer
            )
        )
        // A pending key question is answered with its own buttons, so back cannot leave it hanging.
        .navigationBarBackButtonHidden(flow.phase.isQuestion)
    }
}

/// The connection step drawn from values, so every phase can be previewed and recorded.
struct ConnectPhaseView: View {
    struct Actions {
        var trust: () -> Void
        var cancel: () -> Void
        var back: () -> Void
        var forget: () -> Void
        var retry: () -> Void
        var save: () -> Void
        var skipHelper: () -> Void
        var answer: ([String]) -> Void = { _ in }
    }

    let machine: String
    let phase: ConnectPhase
    let saving: Bool
    let saveError: String?
    let actions: Actions

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color.togoInk)
            .transition(.opacity)
            .motion(.fade, value: phase)
            .haptic(.connect, trigger: phase) { $0.isReady }
            .haptic(.error, trigger: phase) { $0.isFailure }
            .navigationTitle(machine)
            .navigationBarTitleDisplayMode(.inline)
    }

    @ViewBuilder private var content: some View {
        switch phase {
        case .connecting:
            PawWalk(String(localized: "onboard.connect.connecting", defaultValue: "Connecting to \(machine)"))
        case .hostKeyNew(let prompt):
            HostKeyCheckView(machine: machine, kind: .new(prompt), onTrust: actions.trust, onCancel: actions.cancel, onForget: {})
        case .hostKeyChanged(let change):
            HostKeyCheckView(
                machine: machine, kind: .changed(change), onTrust: {}, onCancel: actions.back, onForget: actions.forget
            )
        case .signIn(let request, let error):
            SignInPromptView(request: request, error: error, onSubmit: actions.answer)
        case .keyNotAuthorised(let publicKey):
            PublicKeyView(machine: machine, publicKey: publicKey, onCheckAgain: actions.retry)
        case .helper(let report):
            HelperCheckView(
                machine: machine, report: report, saving: saving, saveError: saveError,
                onSave: actions.save, onCheckAgain: actions.retry, onUseWithoutHelper: actions.skipHelper
            )
        case .unreachable:
            StateView(
                kind: .offline,
                title: String(localized: "onboard.connect.unreachable.title", defaultValue: "No answer from \(machine)"),
                message: String(
                    localized: "onboard.connect.unreachable.message",
                    defaultValue: "\(machine) did not answer for 30 s; it may be asleep"
                ),
                action: .init(String(localized: "onboard.tryAgain", defaultValue: "Try again"), run: actions.retry)
            )
        case .failed(let message):
            StateView(
                kind: .error,
                title: String(localized: "onboard.connect.failed.title", defaultValue: "Couldn't connect to \(machine)"),
                message: message,
                action: .init(String(localized: "onboard.tryAgain", defaultValue: "Try again"), run: actions.retry)
            )
        }
    }
}
