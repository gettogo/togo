import SwiftUI
import TogoKit

/// The add-machine sheet: discovery, the form, then the connection step, in one stack.
struct AddMachineSheet: View {
    @Environment(\.dismiss) private var dismiss
    @State private var flow: AddMachineFlow
    @State private var confirmingDiscard = false

    init(flow: AddMachineFlow) {
        _flow = State(initialValue: flow)
    }

    var body: some View {
        NavigationStack(path: $flow.path) {
            root
                .toolbar { cancel }
            .navigationDestination(for: AddMachineFlow.Route.self) { route in
                // Editing a machine opens straight on the form, and a pull down is off once a
                // step is pushed, so every step can close the sheet, not only the first. The
                // notification step comes after the machine is saved and has its own Not now.
                switch route {
                case .form:
                    form
                        .toolbar { cancel }
                case .connect:
                    ConnectStep(flow: flow)
                        .toolbar { cancel }
                case .notifications:
                    NotificationAskView(
                        machine: flow.machineName,
                        onTurnOn: { Task { await flow.answerNotifications(turnOn: true) } },
                        onNotNow: { Task { await flow.answerNotifications(turnOn: false) } }
                    )
                }
            }
        }
        .task {
            if !flow.startsOnForm { await flow.discover() }
        }
        // The attempt runs whether or not the connection step is on screen. Attempt 0 is
        // nothing asked for yet.
        .task(id: flow.attempt) {
            if flow.attempt > 0 { await flow.runAttempt() }
        }
        .interactiveDismissDisabled(flow.startsOnForm || !flow.path.isEmpty)
        .onDisappear { flow.cancel() }
        .togoControls()
    }

    @ViewBuilder private var root: some View {
        if flow.startsOnForm {
            form
        } else {
            DiscoveryList(
                hosts: flow.hosts, phase: flow.discovery,
                onChoose: flow.choose, onEnterAddress: flow.enterAddress
            )
        }
    }

    private var form: some View {
        MachineForm(
            draft: $flow.draft, showsErrors: flow.showsErrors, importError: flow.importError,
            onImportKey: { text, label, source in Task { await flow.importKey(text, label: label, source: source) } },
            isEditing: flow.isEditing, isWorking: flow.isWorking, canSave: flow.hasChanges, onConnect: flow.connect
        )
    }

    private var cancel: some ToolbarContent {
        ToolbarItem(placement: .cancellationAction) {
            Button(String(localized: "onboard.cancel", defaultValue: "Cancel"), systemImage: "xmark", role: .cancel) {
                // Edits to a saved machine are not thrown away without asking.
                if flow.isEditing, flow.hasChanges { confirmingDiscard = true } else { dismiss() }
            }
            .confirmationDialog(
                String(localized: "onboard.discard.title", defaultValue: "Discard your changes?"),
                isPresented: $confirmingDiscard, titleVisibility: .visible
            ) {
                Button(String(localized: "onboard.discard.confirm", defaultValue: "Discard changes"), role: .destructive) { dismiss() }
                Button(String(localized: "onboard.discard.keep", defaultValue: "Keep editing"), role: .cancel) {}
            }
        }
    }
}

extension AddMachineSheet {
    /// Saves through the app's services and closes the sheet once the machine is stored.
    init(store: AppStore, services: any OnboardingServices, tutorial: TutorialModel? = nil) {
        let flow = AddMachineFlow.adding(store: store, services: services)
        if let tutorial { flow.onDetected = { tutorial.record($0) } }
        flow.notificationAsk = .asking(store.services)
        self.init(flow: flow)
    }
}

extension AddMachineFlow {
    /// A new machine. One already in Togo, by its host key or helper, is not listed twice:
    /// the sheet closes on the machine that was there, saying so.
    static func adding(store: AppStore, services: any OnboardingServices) -> AddMachineFlow {
        let flow = AddMachineFlow(
            services: services,
            add: { try await store.services.addHost($0) },
            finish: { record in
                store.sheet = nil
                if !record.noHelper {
                    store.notice = Notice(
                        text: String(
                            localized: "onboard.added.notice",
                            defaultValue: "\(record.profile.name) added. Set up its agents in Settings › Agents."
                        ),
                        isError: false
                    )
                }
            }
        )
        flow.alreadyAdded = { store.alreadyAdded($0) }
        return flow
    }
}

extension AddMachineFlow.NotificationAsk {
    /// Asks while iOS has not had an answer yet; turning on also pairs the machines online.
    @MainActor static func asking(_ services: any AppServices) -> Self {
        Self(
            needed: { services.pushAuthorization == .notDetermined },
            turnOn: { await services.turnOnNotifications() }
        )
    }
}

/// Offered once a machine with the helper is saved, so the system's notification alert only
/// ever follows a tap that asked for it.
struct NotificationAskView: View {
    let machine: String
    let onTurnOn: () -> Void
    let onNotNow: () -> Void

    var body: some View {
        OnboardingStep(
            title: String(localized: "onboard.notifications.title", defaultValue: "Get notified when an agent needs you"),
            message: String(
                localized: "onboard.notifications.message",
                defaultValue: "Togo can tell you when an agent on \(machine) is waiting for an answer, even while the app is closed."
            )
        ) {
            EmptyView()
        } actions: {
            Button(String(localized: "onboard.notifications.turnOn", defaultValue: "Turn on notifications"), action: onTurnOn)
                .buttonStyle(.primary)
            Button(String(localized: "onboard.notifications.notNow", defaultValue: "Not now"), action: onNotNow)
                .buttonStyle(.secondary)
        }
        .background(Color.togoInk)
        .navigationTitle(machine)
        .navigationBarTitleDisplayMode(.inline)
        // The machine is saved; there is nothing to go back to.
        .navigationBarBackButtonHidden()
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
            .phaseSwap(phase)
            .background(Color.togoInk)
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
            // A new round may ask for a different number of answers; fresh fields for each.
            SignInPromptView(request: request, error: error, onSubmit: actions.answer)
                .id(request)
        case .keyNotAuthorised(let publicKey):
            PublicKeyView(machine: machine, publicKey: publicKey, onCheckAgain: actions.retry)
        case .helper(let report):
            HelperCheckView(
                machine: machine, report: report, saving: saving, saveError: saveError,
                onSave: actions.save, onCheckAgain: actions.retry, onUseWithoutHelper: actions.skipHelper
            )
        case .unreachable:
            ScrollsWhenTall {
                StateView(
                    kind: .offline,
                    title: String(localized: "onboard.connect.unreachable.title", defaultValue: "No answer from \(machine)"),
                    message: String(
                        localized: "onboard.connect.unreachable.message",
                        defaultValue: "\(machine) didn't answer within 30 seconds. It may be asleep, or on a network this \(DeviceName.model) can't reach."
                    ),
                    action: .init(String(localized: "onboard.tryAgain", defaultValue: "Try again"), run: actions.retry)
                )
            }
        case .failed(let message):
            // Some reasons run to several sentences, such as how to turn on Remote Login.
            ScrollsWhenTall {
                StateView(
                    kind: .error,
                    title: String(localized: "onboard.connect.failed.title", defaultValue: "Couldn't connect to \(machine)"),
                    message: message,
                    action: .init(String(localized: "onboard.tryAgain", defaultValue: "Try again"), run: actions.retry)
                )
            }
        }
    }
}
