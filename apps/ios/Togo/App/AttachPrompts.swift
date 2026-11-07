import Observation
import Synchronization
import SwiftUI
import TogoKit

/// Questions a machine asks while a terminal the user opened attaches, such as a one-time
/// code. Each question belongs to one attach attempt and leaves the moment that attempt ends,
/// whatever ends it. Answers go to a question by its id, never to whichever is first.
@MainActor @Observable final class AttachPrompts {
    struct Question: Identifiable {
        let id: UUID
        let request: SignInRequest
        let hostID: HostID
        let address: String
        fileprivate let owner: ObjectIdentifier
        fileprivate let attempt: UUID
        fileprivate let reply: CheckedContinuation<AuthAnswer?, Never>
    }

    /// Past this many waiting questions the oldest is declined, so a machine that asks and
    /// hangs up over and over cannot pile them up.
    static let limit = 4

    private(set) var queue: [Question] = []
    @ObservationIgnored var isForeground = true {
        didSet { if !isForeground { withdrawAll() } }
    }

    var current: Question? { queue.first }

    fileprivate func ask(
        _ prompt: AuthPrompt, owner: ObjectIdentifier, attempt: UUID, hostID: HostID, machine: String, address: String
    ) async -> AuthAnswer? {
        guard isForeground, let request = SignInRequest(prompt, machine: machine) else { return nil }
        let id = UUID()
        return await withTaskCancellationHandler {
            await withCheckedContinuation { reply in
                guard !Task.isCancelled else {
                    reply.resume(returning: nil)
                    return
                }
                queue.append(Question(
                    id: id, request: request, hostID: hostID, address: address, owner: owner, attempt: attempt, reply: reply
                ))
                while queue.count > Self.limit {
                    resolve(queue[0].id, with: nil)
                }
            }
        } onCancel: {
            Task { @MainActor in self.resolve(id, with: nil) }
        }
    }

    /// Does nothing when the question is already gone, so a second tap or a second window
    /// cannot answer a different one.
    func answer(_ id: UUID, _ values: [String]) {
        guard let question = queue.first(where: { $0.id == id }) else { return }
        resolve(id, with: question.request.answer(values))
    }

    func cancel(_ id: UUID) {
        resolve(id, with: nil)
    }

    func withdraw(attempt: UUID) {
        for question in queue where question.attempt == attempt { resolve(question.id, with: nil) }
    }

    func withdraw(owner: ObjectIdentifier) {
        for question in queue where question.owner == owner { resolve(question.id, with: nil) }
    }

    func withdraw(hostID: HostID) {
        for question in queue where question.hostID == hostID { resolve(question.id, with: nil) }
    }

    func withdrawAll() {
        for question in queue { resolve(question.id, with: nil) }
    }

    private func resolve(_ id: UUID, with answer: AuthAnswer?) {
        guard let index = queue.firstIndex(where: { $0.id == id }) else { return }
        let question = queue.remove(at: index)
        question.reply.resume(returning: answer)
    }
}

/// One terminal's prompter. Only the first attach attempt of a terminal the user started may
/// ask; a reconnect declines, so the tab lands on Sign in again instead of a sheet appearing
/// on its own.
final class AttachPrompter: ConnectionPrompter {
    private struct State {
        var mayAsk: Bool
        var attempt: UUID?
        var askedThisAttempt = false
        var askedLastAttempt = false
    }

    private let prompts: AttachPrompts
    private let hostID: HostID
    private let machine: String
    private let address: String
    private let state: Mutex<State>

    init(prompts: AttachPrompts, hostID: HostID, machine: String, address: String, userStarted: Bool) {
        self.prompts = prompts
        self.hostID = hostID
        self.machine = machine
        self.address = address
        state = Mutex(State(mayAsk: userStarted))
    }

    /// Whether the machine asked a question during the attempt that ended last. Only then does
    /// a failed attach read as signed out.
    var askedLastAttempt: Bool { state.withLock { $0.askedLastAttempt } }

    func beginAttempt() -> UUID {
        let id = UUID()
        state.withLock {
            $0.attempt = id
            $0.askedThisAttempt = false
        }
        return id
    }

    func endAttempt(_ id: UUID) async {
        state.withLock {
            guard $0.attempt == id else { return }
            $0.askedLastAttempt = $0.askedThisAttempt
            $0.attempt = nil
            // The user's own action covers one attempt; every later one is automatic.
            $0.mayAsk = false
        }
        await prompts.withdraw(attempt: id)
    }

    func answer(_ prompt: AuthPrompt) async -> AuthAnswer? {
        let (allowed, attempt) = state.withLock { state -> (Bool, UUID?) in
            state.askedThisAttempt = true
            return (state.mayAsk, state.attempt)
        }
        guard allowed, let attempt else { return nil }
        return await prompts.ask(
            prompt, owner: ObjectIdentifier(self), attempt: attempt, hostID: hostID, machine: machine, address: address
        )
    }

    /// A host key is checked where the machine is added or edited, with its fingerprint on screen.
    func trustNewHostKey(host: String, keyType: String, fingerprint: String) async -> Bool { false }
}

extension View {
    /// Shows the attach question on screen while `active`. The control center and the
    /// workspace cover each carry it, active only when they are the one on top. Continue and
    /// Cancel act on the question this sheet shows, by its id.
    ///
    /// `nests`: the host is itself a sheet (new session) that stays up under the question.
    func attachPrompts(_ prompts: AttachPrompts?, active: Bool, nests: Bool = false) -> some View {
        presentSheet("attach.prompt", item: Binding(
            get: { active ? prompts?.current : nil },
            set: { _ in }
        ), nests: nests) { question in
            NavigationStack {
                SignInPromptView(request: question.request) { prompts?.answer(question.id, $0) }
                    .navigationTitle(question.request.machine)
                    .navigationSubtitle(Text(verbatim: question.address))
                    .navigationBarTitleDisplayMode(.inline)
                    .toolbar {
                        ToolbarItem(placement: .cancellationAction) {
                            Button(String(localized: "onboard.cancel", defaultValue: "Cancel"), systemImage: "xmark", role: .cancel) { prompts?.cancel(question.id) }
                        }
                    }
            }
            .id(question.id)
            .togoSheet(.prompt)
            // Only Cancel declines the question, so a swipe cannot leave it waiting.
            .interactiveDismissDisabled()
            .togoControls()
        }
    }
}
