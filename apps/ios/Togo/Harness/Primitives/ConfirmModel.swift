import Foundation
import Observation
import TogoKit

/// A Confirm's answers and the same guards the prompt bar keeps: answers work only after the
/// question has been on screen for the arming delay, a submit names the question it was made
/// for and is dropped when another one has replaced it, and one answer is on its way at a time.
@MainActor @Observable final class ConfirmModel {
    enum Phase: Equatable {
        case open
        case sending
        /// The last answer did not arrive; the question is open again.
        case failed(String)
        /// The harness took the answer; carries what it said.
        case answered(String?)
    }

    private(set) var confirm: HarnessScreen.Confirm
    /// Chosen option ids by question id.
    private(set) var selections: [String: Set<String>] = [:]
    /// Typed answers by question id.
    var texts: [String: String] = [:]
    private(set) var phase: Phase = .open
    /// Whether the answers work yet.
    private(set) var armed = false
    /// The question changed under the user while they were answering it.
    private(set) var changed = false
    /// No route from the phone reached the question (`fallback`, or no result): it is still
    /// open in the terminal, where the user finishes it. Answering again stays possible.
    private(set) var handedOff = false
    @ObservationIgnored private(set) var arming: Task<Void, Never>?
    private let armDelay: Duration

    /// `arms` false waits for `beginArming()`, for a view that makes its model on every render
    /// and keeps only the first.
    init(confirm: HarnessScreen.Confirm, armDelay: Duration = PromptBarModel.defaultArmDelay, arms: Bool = true) {
        self.confirm = confirm
        self.armDelay = armDelay
        if arms { rearm() }
    }

    /// Starts the arming delay once the question is on screen; again does nothing.
    func beginArming() {
        guard !armed, arming == nil else { return }
        rearm()
    }

    /// The answer went through but the question is still open (the harness did not close it):
    /// it can be answered again.
    func reopen() {
        if case .answered = phase { phase = .open }
    }

    /// A fresh read of the screen. The same question keeps its answers; a different one starts
    /// over, disarmed, and says it changed when the user had begun answering.
    func update(_ new: HarnessScreen.Confirm) {
        guard new != confirm else { return }
        let begun = !selections.isEmpty || texts.values.contains { !$0.isEmpty }
        confirm = new
        selections = [:]
        texts = [:]
        changed = begun
        if case .answered = phase {} else { phase = .open }
        rearm()
    }

    // MARK: Answering

    func isSelected(_ option: String, in question: HarnessQuestion) -> Bool {
        selections[question.id]?.contains(option) ?? false
    }

    /// Picks an option: the only one for a single-choice question, toggled for a multi.
    func toggle(_ option: String, in question: HarnessQuestion) {
        guard phase != .sending, question.options.contains(where: { $0.id == option }) else { return }
        var chosen = selections[question.id] ?? []
        if question.multi {
            if chosen.contains(option) { chosen.remove(option) } else { chosen.insert(option) }
        } else {
            chosen = chosen.contains(option) ? [] : [option]
        }
        selections[question.id] = chosen.isEmpty ? nil : chosen
        changed = false
    }

    func text(for question: HarnessQuestion) -> String { texts[question.id] ?? "" }

    func setText(_ text: String, for question: HarnessQuestion) {
        guard question.allowText else { return }
        texts[question.id] = text
        if !text.isEmpty { changed = false }
    }

    /// Option ids in the question's own order, then the typed words when there are any.
    static func answers(_ confirm: HarnessScreen.Confirm, selections: [String: Set<String>], texts: [String: String]) -> [String: [String]] {
        var out: [String: [String]] = [:]
        for question in confirm.questions {
            var answer = question.options.map(\.id).filter { selections[question.id]?.contains($0) ?? false }
            if question.allowText {
                let typed = (texts[question.id] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
                if !typed.isEmpty { answer.append(typed) }
            }
            if !answer.isEmpty { out[question.id] = answer }
        }
        return out
    }

    var answers: [String: [String]] { Self.answers(confirm, selections: selections, texts: texts) }

    /// `{"answers": {question id: [option ids or text]}}`; empty for a plain confirmation.
    static func args(_ answers: [String: [String]]) -> [String: JSONValue] {
        ["answers": .object(answers.mapValues { .array($0.map(JSONValue.string)) })]
    }

    /// Every question has an answer.
    var isComplete: Bool {
        let given = answers
        return confirm.questions.allSatisfy { given[$0.id] != nil }
    }

    var canSubmit: Bool {
        guard armed, isComplete else { return false }
        switch phase {
        case .open, .failed: return true
        case .sending, .answered: return false
        }
    }

    /// Sends the answers for `shown`, the question that was on screen. Dropped when another
    /// question has replaced it, before the arming delay, or while an answer is on its way.
    func submit(for shown: HarnessScreen.Confirm, send: (_ args: [String: JSONValue]) async -> HarnessActionResult?) async {
        guard shown == confirm, canSubmit else { return }
        phase = .sending
        handedOff = false
        let result = await send(Self.args(answers))
        guard shown == confirm else { return }
        switch result?.result {
        case .applied?, .typed?, .queued?, .relaunched?:
            phase = .answered(result?.message)
        case .failed?:
            phase = .failed(result?.message ?? PrimitiveCopy.failed)
            rearm()
        default:
            // No route reached it, or the terminal took over: the question is still open there,
            // and the card says to finish it in the terminal.
            phase = .open
            handedOff = true
        }
    }

    /// Arms at once, for a still snapshot where no time passes.
    func armNow() {
        arming?.cancel()
        armed = true
    }

    private func rearm() {
        armed = false
        arming?.cancel()
        let delay = armDelay
        let target = confirm
        arming = Task { @MainActor [weak self] in
            if delay > .zero { try? await Task.sleep(for: delay) }
            guard let self, !Task.isCancelled, self.confirm == target else { return }
            self.armed = true
        }
    }
}
