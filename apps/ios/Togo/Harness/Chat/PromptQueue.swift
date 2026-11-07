import Foundation

/// A prompt the phone holds before the helper has it: while the agent works on a harness that
/// cannot queue, while a send is on its way, or after a send failed.
struct PendingPrompt: Identifiable, Equatable, Sendable {
    enum State: Equatable, Sendable {
        /// Waits for the turn to end.
        case waiting
        /// On its way to the helper.
        case sending
        /// The helper refused it or could not be reached; `message` says why.
        case failed(String)

        var isFailed: Bool { if case .failed = self { true } else { false } }
    }

    let id: UUID
    var text: String
    var state: State
}

/// The phone's own prompt queue, first in first out. One prompt is on its way at a time; the
/// next one goes once it is through and the session can take a prompt, which is when the agent
/// is idle or the harness queues prompts itself. A failed prompt stays, marked, until it is sent
/// again or cancelled.
struct PromptQueue: Equatable, Sendable {
    /// The most prompts held, as many as the helper's own queue.
    static let limit = 8

    private(set) var prompts: [PendingPrompt] = []

    var isEmpty: Bool { prompts.isEmpty }

    var isFull: Bool { prompts.count >= Self.limit }

    /// Adds a prompt at the end; nil when the text is blank or the queue is full.
    @discardableResult
    mutating func add(_ text: String, id: UUID = UUID()) -> UUID? {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, !isFull else { return nil }
        prompts.append(PendingPrompt(id: id, text: trimmed, state: .waiting))
        return id
    }

    /// The prompt to send now, marked as sending; nil while one is on its way, while the
    /// session cannot take one, or when nothing waits. A failed prompt holds the ones after it.
    mutating func next(canSend: Bool) -> PendingPrompt? {
        guard canSend, !prompts.contains(where: { $0.state == .sending }),
              let first = prompts.first, first.state == .waiting else { return nil }
        prompts[0].state = .sending
        return prompts[0]
    }

    /// The helper took it: it shows from the conversation now.
    mutating func sent(_ id: UUID) {
        prompts.removeAll { $0.id == id }
    }

    mutating func failed(_ id: UUID, message: String) {
        guard let at = prompts.firstIndex(where: { $0.id == id }) else { return }
        prompts[at].state = .failed(message)
    }

    /// The helper was busy: back to waiting, still first.
    mutating func requeue(_ id: UUID) {
        guard let at = prompts.firstIndex(where: { $0.id == id }) else { return }
        prompts[at].state = .waiting
    }

    mutating func retry(_ id: UUID) {
        requeue(id)
    }

    /// Takes a waiting or failed prompt away. One on its way cannot be called back.
    @discardableResult
    mutating func cancel(_ id: UUID) -> Bool {
        guard let at = prompts.firstIndex(where: { $0.id == id }), prompts[at].state != .sending else { return false }
        prompts.remove(at: at)
        return true
    }
}
