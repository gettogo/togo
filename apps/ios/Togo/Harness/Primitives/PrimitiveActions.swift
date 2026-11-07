import Observation
import SwiftUI
import TogoKit
import UIKit

/// What an action's result says to the user, briefly: "Done", "Sent", a failure line, or that
/// the terminal opened because no native route works.
struct PrimitiveFeedback: Equatable, Identifiable {
    enum Kind: Equatable { case done, sent, queued, restarted, failed, terminal }

    var id = UUID()
    var kind: Kind
    var title: String
    var detail: String?

    var isFailure: Bool { kind == .failed }

    /// nil when there is nothing to say: the call failed in a way its caller reported.
    static func from(_ result: HarnessActionResult?) -> PrimitiveFeedback? {
        guard let result else { return nil }
        let message = result.message.flatMap { $0.isEmpty ? nil : $0 }
        switch result.result {
        case .applied: return PrimitiveFeedback(kind: .done, title: PrimitiveCopy.done, detail: message)
        case .typed: return PrimitiveFeedback(kind: .sent, title: PrimitiveCopy.sent, detail: message)
        case .queued: return PrimitiveFeedback(kind: .queued, title: PrimitiveCopy.queued, detail: message)
        case .relaunched: return PrimitiveFeedback(kind: .restarted, title: PrimitiveCopy.restarted, detail: message)
        case .fallback: return PrimitiveFeedback(kind: .terminal, title: PrimitiveCopy.openedTerminal, detail: message)
        case .failed: return PrimitiveFeedback(kind: .failed, title: message ?? PrimitiveCopy.failed)
        default: return PrimitiveFeedback(kind: .done, title: PrimitiveCopy.done, detail: message)
        }
    }

    static func == (a: PrimitiveFeedback, b: PrimitiveFeedback) -> Bool { a.id == b.id }

    /// The outcome in a button's own words: "Restarted", "Sent", "Done".
    static func shortTitle(_ feedback: PrimitiveFeedback) -> String {
        switch feedback.kind {
        case .restarted: PrimitiveCopy.restartedShort
        case .failed: PrimitiveCopy.didntWork
        default: feedback.title
        }
    }
}

/// Where an action's outcome shows.
enum FeedbackPlace: Equatable {
    /// On the control that ran it: its label becomes the outcome, a failure's reason under it.
    case control
    /// On the control for success; a failure, which needs room for its reason, in the toast
    /// (bar buttons).
    case label
    /// In the toast: the control is gone or out of sight (swipe and context-menu actions).
    case toast
    /// Nowhere: the caller says it (a pick that closes its sheet, a settings field).
    case none
}

/// A selection or a field change that may restart the agent: it goes through `ActionGate`.
struct GatedChange {
    var action: String
    var target: String?
    var args: [String: JSONValue]?
    var confirm: HarnessSelectConfirm?
    /// The setting's name: "Model", for "Model Sonnet".
    var setting: String
    /// What it changes to: "Sonnet".
    var value: String
    /// The value it replaces and the args that put it back, for Undo after a restart.
    var previous: (label: String, args: [String: JSONValue]?)?
}

/// Runs a screen's actions: one at a time per action and target, asking first when the action
/// says to (`ActionGate`), and keeping what the last one said on its control.
@MainActor @Observable final class PrimitiveActions {
    private(set) var running: Set<String> = []
    /// The toast, only for results whose control is not on screen.
    private(set) var feedback: PrimitiveFeedback?
    /// What each control's last run said, by action and target; it reverts after a moment.
    private(set) var outcomes: [String: PrimitiveFeedback] = [:]
    /// The latest in-place outcome, for its haptic.
    private(set) var lastOutcome: PrimitiveFeedback?
    /// The question the system dialog is asking.
    var asking: ActionGateRequest?

    init() {}

    nonisolated static func key(_ action: String, _ target: String?) -> String {
        "\(action)\u{1F}\(target ?? "")"
    }

    func isRunning(_ action: String, target: String? = nil) -> Bool {
        running.contains(Self.key(action, target))
    }

    func outcome(_ action: String, target: String? = nil) -> PrimitiveFeedback? {
        outcomes[Self.key(action, target)]
    }

    var isBusy: Bool { !running.isEmpty }

    /// The action's fixed args under the user's: a key the user gives wins.
    nonisolated static func merged(fixed: [String: JSONValue]?, user: [String: JSONValue]?) -> [String: JSONValue]? {
        guard let fixed, !fixed.isEmpty else { return user }
        guard let user, !user.isEmpty else { return fixed }
        return fixed.merging(user) { _, mine in mine }
    }

    /// Runs an offered action, or asks first when it carries a confirm line. The dialog offers
    /// "With instructions…" when a command runs this action with an optional argument
    /// (compact's focus): that leaves the command in the composer instead.
    func trigger(
        _ ref: HarnessActionRef, target: String? = nil, args: [String: JSONValue]? = nil,
        place: FeedbackPlace = .control, context: PrimitiveContext
    ) async {
        let merged = Self.merged(fixed: ref.args, user: args)
        let line = context.compose == nil ? nil : context.instructionsLine(ref.id)
        switch ActionGate.decide(confirm: ref.confirm, effect: nil, working: context.working(), label: ref.label,
                                 agent: context.agent, instructions: line) {
        case .confirm(var dialog):
            dialog.destructive = ref.role == .destructive
            asking = ActionGateRequest(
                dialog: dialog,
                run: { [weak self] in await self?.perform(ref.id, target: target, args: merged, place: place, context: context) },
                instructions: context.compose
            )
        case .run, .runWithUndo:
            await perform(ref.id, target: target, args: merged, place: place, context: context)
        }
    }

    /// A selection or field change (`onSelectConfirm`, `actionConfirm`, the state chip): idle
    /// relaunches run and offer Undo in the chat; while the agent works a relaunch asks, with
    /// "After this reply". `done` gets the result once it ran (nil when cancelled or held).
    func change(_ change: GatedChange, context: PrimitiveContext, done: @escaping @MainActor (HarnessActionResult?) -> Void = { _ in }) async {
        let decision = ActionGate.decide(
            confirm: change.confirm?.text, effect: change.confirm?.effect, working: context.working(),
            label: change.setting, agent: context.agent, previous: change.previous?.label
        )
        switch decision {
        case .run:
            done(await apply(change, undo: nil, context: context))
        case .runWithUndo:
            done(await apply(change, undo: change.previous, context: context))
        case .confirm(let dialog):
            asking = ActionGateRequest(
                dialog: dialog,
                run: { [weak self] in done(await self?.apply(change, undo: nil, context: context)) },
                afterReply: Self.afterReply(change, context: context, done: done)
            )
        }
    }

    /// "After this reply": the change is held for the turn's end; the host may close now.
    private static func afterReply(
        _ change: GatedChange, context: PrimitiveContext, done: @escaping @MainActor (HarnessActionResult?) -> Void
    ) -> (@MainActor () -> Void)? {
        guard let hold = context.runAfterTurn else { return nil }
        return {
            hold(change.action, change.target, change.args, PrimitiveCopy.restartedWith(change.value))
            done(HarnessActionResult(result: .queued))
        }
    }

    /// Runs a change and leaves its line in the chat: "Effort high", "Restarted with Sonnet"
    /// with Undo, or "Effort high, waiting for Claude" while the harness has not shown it yet.
    private func apply(_ change: GatedChange, undo: (label: String, args: [String: JSONValue]?)?, context: PrimitiveContext) async -> HarnessActionResult? {
        let result = await perform(change.action, target: change.target, args: change.args, place: .none, context: context)
        guard let result, let note = context.note else { return result }
        switch result.result {
        case .relaunched:
            let back = undo.map { previous in
                SessionUndo(title: PrimitiveCopy.backTo(previous.label)) { [weak self] in
                    var reverse = change
                    reverse.args = previous.args
                    reverse.value = previous.label
                    reverse.previous = nil
                    _ = await self?.apply(reverse, undo: nil, context: context)
                }
            }
            note(PrimitiveCopy.restartedWith(change.value), back)
        case .typed:
            note(PrimitiveCopy.setWaiting(change.setting, change.value, agent: context.agent), nil)
        case .applied, .queued:
            note(PrimitiveCopy.setTo(change.setting, change.value), nil)
        default:
            break
        }
        return result
    }

    /// Runs `action` unless the same one is already on its way. Returns the result, or nil
    /// when it did not run or failed in a way the context reported.
    @discardableResult
    func perform(
        _ action: String, target: String? = nil, args: [String: JSONValue]? = nil,
        place: FeedbackPlace = .toast, context: PrimitiveContext
    ) async -> HarnessActionResult? {
        let key = Self.key(action, target)
        guard running.insert(key).inserted else { return nil }
        defer { running.remove(key) }
        let result = await context.run(action, target, args)
        if let feedback = PrimitiveFeedback.from(result) { say(feedback, key: key, place: place) }
        if result?.result == .fallback { context.showTerminal() }
        return result
    }

    private func say(_ feedback: PrimitiveFeedback, key: String, place: FeedbackPlace) {
        switch place {
        case .none:
            return
        case .toast:
            show(feedback)
        case .label where feedback.isFailure:
            show(feedback)
        case .control, .label:
            // motion: cause → effect — the tapped control's own label becomes the outcome.
            withAnimation(Motion.animation(.respond, reduce: Self.reduceMotion)) { outcomes[key] = feedback }
            lastOutcome = feedback
            AccessibilityNotification.Announcement(feedback.title).post()
            let id = feedback.id
            Task { [weak self] in
                try? await Task.sleep(for: .seconds(feedback.isFailure ? 5 : 2.4))
                guard let self, self.outcomes[key]?.id == id else { return }
                // motion: state change — the label returns to what the control does.
                withAnimation(Motion.animation(.respond, reduce: Self.reduceMotion)) { self.outcomes[key] = nil }
            }
        }
    }

    /// The toast, for a result whose control is not on screen. It comes from the top edge it
    /// lives on; its haptic plays with it.
    func show(_ feedback: PrimitiveFeedback) {
        // motion: origin — the toast comes from the top edge it lives on, away from any control.
        withAnimation(Motion.animation(.dropIn, reduce: Self.reduceMotion)) { self.feedback = feedback }
    }

    /// The toast leaves by concealing; it never rides the arrival spring out.
    func clearFeedback(_ id: UUID) {
        guard feedback?.id == id else { return }
        // motion: state change — the toast is done; it fades where it is.
        withAnimation(Motion.animation(.conceal, reduce: Self.reduceMotion)) { feedback = nil }
    }

    private static var reduceMotion: Bool { UIAccessibility.isReduceMotionEnabled }
}

extension View {
    /// The toast an action's result leaves, the confirmation an action asks for, and the
    /// haptic its outcome plays.
    func primitiveActions(_ actions: PrimitiveActions, context: PrimitiveContext) -> some View {
        modifier(PrimitiveActionsModifier(actions: actions, context: context))
    }
}

private struct PrimitiveActionsModifier: ViewModifier {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.loopsPaused) private var loopsPaused
    @Bindable var actions: PrimitiveActions
    let context: PrimitiveContext

    func body(content: Content) -> some View {
        content
            .overlay(alignment: .top) {
                ZStack {
                    if let feedback = actions.feedback {
                        FeedbackToast(feedback: feedback)
                            // motion: origin — the toast presents from the top edge it lives on.
                            .transition(.asymmetric(
                                insertion: Motion.present(from: .top, reduce: reduceMotion),
                                removal: Motion.conceal(reduce: reduceMotion)
                            ))
                            .task(id: feedback.id) {
                                AccessibilityNotification.Announcement(feedback.title).post()
                                guard !loopsPaused else { return }
                                try? await Task.sleep(for: .seconds(feedback.isFailure ? 4 : 2.4))
                                actions.clearFeedback(feedback.id)
                            }
                    }
                }
                .padding(.top, 8)
            }
            .actionConfirmation($actions.asking)
            .haptic(.done, trigger: actions.feedback) { _, new in
                guard let new else { return false }
                return !new.isFailure && new.kind != .terminal
            }
            .haptic(.error, trigger: actions.feedback) { _, new in new?.isFailure == true }
            .haptic(.done, trigger: actions.lastOutcome) { _, new in new.map { !$0.isFailure } ?? false }
            .haptic(.error, trigger: actions.lastOutcome) { _, new in new?.isFailure == true }
    }
}

/// Snow on ink, at the top: what just happened, and the machine's own words under it.
private struct FeedbackToast: View {
    let feedback: PrimitiveFeedback

    var body: some View {
        VStack(spacing: 2) {
            HStack(spacing: 6) {
                if feedback.isFailure {
                    Image(systemName: "exclamationmark.circle.fill").foregroundStyle(PrimitiveTint.fail)
                }
                Text(feedback.title)
                    .font(.subheadline.weight(.semibold))
                    .multilineTextAlignment(.center)
            }
            if let detail = feedback.detail {
                Text(verbatim: detail)
                    .font(.mono(.caption2))
                    .foregroundStyle(Color.togoInk.opacity(0.6))
                    .multilineTextAlignment(.center)
            }
        }
        .foregroundStyle(Color.togoInk)
        .padding(.horizontal, 16)
        .padding(.vertical, 10)
        .background(Color.togoSnow, in: .rect(cornerRadius: 18, style: .continuous))
        .padding(.horizontal, 24)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.updatesFrequently)
    }
}

/// A row of action buttons, wrapping to a column when the text is too large for one line. A
/// button's own label becomes its outcome ("Restarted ✓"), and a failure's reason shows under
/// the row at once; no toast.
struct PrimitiveActionRow: View {
    let refs: [HarnessActionRef]
    var target: String?
    var large = false
    let actions: PrimitiveActions
    let context: PrimitiveContext

    var body: some View {
        if !refs.isEmpty {
            VStack(alignment: .leading, spacing: 6) {
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 8) { buttons }
                    VStack(alignment: .leading, spacing: 8) { buttons }
                }
                // Errors land at once, at the control that failed (motion principles v2 §2).
                ForEach(failures, id: \.id) { failure in
                    Text(verbatim: failure.title)
                        .font(.footnote)
                        .foregroundStyle(PrimitiveTint.fail)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }
    }

    private var failures: [PrimitiveFeedback] {
        refs.compactMap { actions.outcome($0.id, target: target) }.filter(\.isFailure)
    }

    @ViewBuilder private var buttons: some View {
        ForEach(refs) { ref in
            Button {
                Task { await actions.trigger(ref, target: target, context: context) }
            } label: {
                OutcomeLabel(title: ref.label, outcome: actions.outcome(ref.id, target: target),
                             busy: actions.isRunning(ref.id, target: target))
            }
            .buttonStyle(PrimitiveCapsuleStyle(role: ref.role, large: large))
            .disabled(actions.isRunning(ref.id, target: target))
            .accessibilityValue(actions.isRunning(ref.id, target: target) ? PrimitiveCopy.running : "")
        }
    }
}

/// A control's label that becomes its outcome in place: "Restart" → "Restarted ✓", or
/// "Didn't work" after a failure, then back. A spinner holds its place while it runs.
struct OutcomeLabel: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let title: String
    let outcome: PrimitiveFeedback?
    let busy: Bool

    var body: some View {
        HStack(spacing: 4) {
            Text(words)
                // motion: cause → effect — the words morph from what it does into what it did.
                .contentTransition(reduceMotion ? .opacity : .interpolate)
            if let outcome, !outcome.isFailure {
                Image(systemName: "checkmark").font(.caption.weight(.bold))
            }
        }
        .opacity(busy ? 0 : 1)
        .overlay {
            if busy { ProgressView().controlSize(.small) }
        }
    }

    private var words: String {
        guard let outcome else { return title }
        return outcome.isFailure ? PrimitiveCopy.didntWork : PrimitiveFeedback.shortTitle(outcome)
    }
}
