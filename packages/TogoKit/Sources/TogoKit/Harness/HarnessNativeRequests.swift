import Foundation

/// Picks the question the chat should attach the agent's own screen to.
///
/// A question the phone cannot answer through the chat (`answerable` false) is
/// one the agent is showing in its terminal. Where the session can show that
/// screen natively, the chat attaches it once per question: closing it, or a
/// reconnect, must not open it again while the same question is still waiting.
/// A question that is answered, or gone from the conversation, is forgotten,
/// so a later one with the same id is treated as new.
public struct HarnessNativeRequests: Sendable {
    /// Open questions the chat has already attached a screen to.
    private var attached: Set<String> = []

    public init() {}

    /// The id of the oldest open question the phone cannot answer and that has
    /// not been attached yet, marking it attached. Nil when there is none, or
    /// when the session cannot show the screen now: nothing is marked then, so
    /// the question is offered once it can.
    ///
    /// `skipping` holds ids that must not be offered by this call, such as
    /// the questions that were already waiting when a command was sent. They
    /// are neither returned nor marked, and still count as waiting, so one
    /// that was attached before stays attached.
    public mutating func next(in items: [HarnessItem], available: Bool, skipping: Set<String> = []) -> String? {
        var waiting: [(id: String, seq: Int)] = []
        for item in items {
            guard case .question(let question) = item.body, !question.isAnswerable,
                  question.answered?.isEmpty ?? true else { continue }
            waiting.append((item.id, item.seq))
        }
        attached.formIntersection(waiting.map(\.id))
        guard available else { return nil }
        let oldest = waiting.filter { !attached.contains($0.id) && !skipping.contains($0.id) }.min { $0.seq < $1.seq }
        guard let oldest else { return nil }
        attached.insert(oldest.id)
        return oldest.id
    }

    /// Forgets every attachment. Call it when the conversation changes.
    public mutating func reset() {
        attached.removeAll()
    }
}
