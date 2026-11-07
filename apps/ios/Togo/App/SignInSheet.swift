import Observation
import SwiftUI
import TogoKit

/// Signing in to a machine the monitor could not sign in to on its own. The machine's
/// questions, such as a password or a one-time code, are put to the user here; background
/// connections never ask.
@MainActor @Observable final class SignInModel {
    enum Phase: Equatable {
        case connecting
        case asking(SignInRequest)
        case failed(String)
    }

    private(set) var phase: Phase = .connecting
    let machine: String
    private let hostID: HostID
    private let services: any AppServices
    private let finish: @MainActor () -> Void
    @ObservationIgnored private var pending: CheckedContinuation<AuthAnswer?, Never>?

    init(hostID: HostID, machine: String, services: any AppServices, finish: @escaping @MainActor () -> Void) {
        self.hostID = hostID
        self.machine = machine
        self.services = services
        self.finish = finish
    }

    /// The least time the signing-in state stays up, so a refusal that comes back at once still
    /// reads as a fresh attempt rather than a button that did nothing.
    static let minimumAttempt: Duration = .milliseconds(700)

    func run() async {
        phase = .connecting
        let started = ContinuousClock.now
        let prompter = SheetPrompter { [weak self] prompt in await self?.ask(prompt) }
        do {
            try await services.signIn(hostID: hostID, prompter: prompter)
            finish()
        } catch {
            let shown = ContinuousClock.now - started
            if shown < Self.minimumAttempt { try? await Task.sleep(for: Self.minimumAttempt - shown) }
            guard !Task.isCancelled else { return }
            phase = .failed(ServiceErrors.message(error))
        }
    }

    func submit(_ values: [String]) {
        guard case .asking(let request) = phase else { return }
        phase = .connecting
        resume(request.answer(values))
    }

    /// Leaving the sheet answers a waiting question with nothing, which ends the attempt.
    func cancel() {
        resume(nil)
    }

    private func ask(_ prompt: AuthPrompt) async -> AuthAnswer? {
        guard let request = SignInRequest(prompt, machine: machine) else { return nil }
        resume(nil)
        phase = .asking(request)
        return await withCheckedContinuation { pending = $0 }
    }

    private func resume(_ answer: AuthAnswer?) {
        pending?.resume(returning: answer)
        pending = nil
    }
}

/// Puts each question to the sheet. A new host key is never trusted here: a key that changed
/// is a matter for the machine's settings, where its fingerprint is shown.
private struct SheetPrompter: ConnectionPrompter {
    let ask: @Sendable (AuthPrompt) async -> AuthAnswer?

    func answer(_ prompt: AuthPrompt) async -> AuthAnswer? { await ask(prompt) }
    func trustNewHostKey(host: String, keyType: String, fingerprint: String) async -> Bool { false }
}

struct SignInSheet: View {
    @Environment(\.dismiss) private var dismiss
    @State private var model: SignInModel
    @State private var attempt = 0

    init(model: SignInModel) {
        _model = State(initialValue: model)
    }

    var body: some View {
        NavigationStack {
            SignInPhaseView(machine: model.machine, phase: model.phase, onSubmit: model.submit) { attempt += 1 }
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) {
                        Button(String(localized: "onboard.cancel", defaultValue: "Cancel"), systemImage: "xmark", role: .cancel) { dismiss() }
                    }
                }
        }
        .task(id: attempt) { await model.run() }
        .onDisappear { model.cancel() }
        .togoControls()
    }
}

/// The sign-in sheet drawn from values, so each phase can be recorded.
struct SignInPhaseView: View {
    let machine: String
    let phase: SignInModel.Phase
    let onSubmit: ([String]) -> Void
    let onRetry: () -> Void

    var body: some View {
        content
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color.togoInk)
            .motion(.fade, value: phase)
            .navigationTitle(machine)
            .navigationBarTitleDisplayMode(.inline)
    }

    @ViewBuilder private var content: some View {
        switch phase {
        case .connecting:
            PawWalk(String(localized: "onboard.signIn.connecting", defaultValue: "Signing in to \(machine)"))
        case .asking(let request):
            SignInPromptView(request: request, onSubmit: onSubmit)
                .id(request)
        case .failed(let message):
            StateView(
                kind: .error,
                title: String(localized: "onboard.signIn.failed", defaultValue: "Couldn't sign in to \(machine)"),
                message: message,
                action: .init(String(localized: "onboard.tryAgain", defaultValue: "Try again"), run: onRetry)
            )
        }
    }
}
