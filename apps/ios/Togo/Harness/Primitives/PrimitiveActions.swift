import Observation
import SwiftUI
import TogoKit

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
}

/// Runs a screen's actions: one at a time per action and target, asking first when the action
/// says to, and keeping what the last one said for the toast. A primitive owns one.
@MainActor @Observable final class PrimitiveActions {
    /// An action waiting on the user's yes before it runs.
    struct Pending: Identifiable {
        var id: String { PrimitiveActions.key(ref.id, target) }
        var ref: HarnessActionRef
        var target: String?
        var args: [String: JSONValue]?
    }

    private(set) var running: Set<String> = []
    private(set) var feedback: PrimitiveFeedback?
    var asking: Pending?

    init() {}

    nonisolated static func key(_ action: String, _ target: String?) -> String {
        "\(action)\u{1F}\(target ?? "")"
    }

    func isRunning(_ action: String, target: String? = nil) -> Bool {
        running.contains(Self.key(action, target))
    }

    var isBusy: Bool { !running.isEmpty }

    /// The action's fixed args under the user's: a key the user gives wins.
    nonisolated static func merged(fixed: [String: JSONValue]?, user: [String: JSONValue]?) -> [String: JSONValue]? {
        guard let fixed, !fixed.isEmpty else { return user }
        guard let user, !user.isEmpty else { return fixed }
        return fixed.merging(user) { _, mine in mine }
    }

    /// Runs an offered action, or asks first when it carries a confirm line.
    func trigger(_ ref: HarnessActionRef, target: String? = nil, args: [String: JSONValue]? = nil, context: PrimitiveContext) async {
        if let confirm = ref.confirm, !confirm.isEmpty {
            asking = Pending(ref: ref, target: target, args: args)
            return
        }
        await perform(ref.id, target: target, args: Self.merged(fixed: ref.args, user: args), context: context)
    }

    /// The user said yes to `pending`. It is passed in rather than read from `asking`: the
    /// dialog clears `asking` as it closes, before a task started by its button runs.
    func confirm(_ pending: Pending, context: PrimitiveContext) async {
        if asking?.id == pending.id { asking = nil }
        await perform(pending.ref.id, target: pending.target, args: Self.merged(fixed: pending.ref.args, user: pending.args), context: context)
    }

    /// Runs `action` unless the same one is already on its way. Returns the result, or nil
    /// when it did not run or failed in a way the context reported.
    @discardableResult
    func perform(_ action: String, target: String? = nil, args: [String: JSONValue]? = nil, context: PrimitiveContext) async -> HarnessActionResult? {
        let key = Self.key(action, target)
        guard running.insert(key).inserted else { return nil }
        defer { running.remove(key) }
        let result = await context.run(action, target, args)
        if let feedback = PrimitiveFeedback.from(result) { show(feedback) }
        if result?.result == .fallback { context.showTerminal() }
        return result
    }

    func show(_ feedback: PrimitiveFeedback) {
        self.feedback = feedback
    }

    func clearFeedback(_ id: UUID) {
        if feedback?.id == id { feedback = nil }
    }
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
                            .transition(reduceMotion ? .opacity : .move(edge: .top).combined(with: .opacity))
                            .task(id: feedback.id) {
                                AccessibilityNotification.Announcement(feedback.title).post()
                                guard !loopsPaused else { return }
                                try? await Task.sleep(for: .seconds(feedback.isFailure ? 4 : 2.4))
                                actions.clearFeedback(feedback.id)
                            }
                    }
                }
                .padding(.top, 8)
                .motion(.dropIn, value: actions.feedback?.id)
            }
            .confirmationDialog(
                actions.asking?.ref.confirm ?? "",
                isPresented: Binding(get: { actions.asking != nil }, set: { if !$0 { actions.asking = nil } }),
                titleVisibility: .visible,
                presenting: actions.asking
            ) { pending in
                Button(pending.ref.label, role: pending.ref.role == .destructive ? .destructive : nil) {
                    Task { await actions.confirm(pending, context: context) }
                }
                Button(PrimitiveCopy.cancel, role: .cancel) { actions.asking = nil }
            }
            .haptic(.done, trigger: actions.feedback) { _, new in
                guard let new else { return false }
                return !new.isFailure && new.kind != .terminal
            }
            .haptic(.error, trigger: actions.feedback) { _, new in new?.isFailure == true }
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

/// A row of action buttons, wrapping to a column when the text is too large for one line.
struct PrimitiveActionRow: View {
    let refs: [HarnessActionRef]
    var target: String?
    var large = false
    let actions: PrimitiveActions
    let context: PrimitiveContext

    var body: some View {
        if !refs.isEmpty {
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 8) { buttons }
                VStack(alignment: .leading, spacing: 8) { buttons }
            }
        }
    }

    @ViewBuilder private var buttons: some View {
        ForEach(refs) { ref in
            Button {
                Task { await actions.trigger(ref, target: target, context: context) }
            } label: {
                HStack(spacing: 6) {
                    if actions.isRunning(ref.id, target: target) {
                        ProgressView().controlSize(.small)
                    }
                    Text(ref.label)
                }
            }
            .buttonStyle(PrimitiveCapsuleStyle(role: ref.role, large: large))
            .disabled(actions.isRunning(ref.id, target: target))
            .accessibilityValue(actions.isRunning(ref.id, target: target) ? PrimitiveCopy.running : "")
        }
    }
}
