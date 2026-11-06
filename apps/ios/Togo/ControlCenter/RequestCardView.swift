import SwiftUI
import TogoKit

/// An agent waiting for an answer: who is asking, what for, and the answers it accepts.
/// The card is the only raised surface in the app.
struct RequestCardView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @State private var reply = ""

    let card: RequestCard
    var machineName: String?
    /// Shown under the answers after one failed to arrive; the card is open again by then.
    var failure: String?
    var onOpen: (() -> Void)?
    let onAnswer: (Decision) -> Void
    /// The card's fill. The sidebar already sits on the raised surface, so there the question
    /// is a plain row grouped by spacing.
    var surface: Color

    init(
        card: RequestCard, machineName: String? = nil, failure: String? = nil, onOpen: (() -> Void)? = nil,
        surface: Color = .togoRaised, onAnswer: @escaping (Decision) -> Void
    ) {
        self.card = card
        self.surface = surface
        self.machineName = machineName
        self.failure = failure
        self.onOpen = onOpen
        self.onAnswer = onAnswer
    }

    /// The agent's options split the way the card lays them out: one filled approve, one plain
    /// deny and the rest in a menu.
    struct Choices: Equatable {
        var approve: ApprovalOption?
        var deny: ApprovalOption?
        var more: [ApprovalOption]

        init(_ options: [ApprovalOption]) {
            let approve = options.first { $0.role == .approve } ?? options.first { $0.role != .deny }
            let deny = options.first { $0.role == .deny }
            self.approve = approve
            self.deny = deny
            more = options.filter { $0 != approve && $0 != deny }
        }
    }

    static func outcomeLabel(_ outcome: RequestCard.Outcome) -> String {
        switch outcome {
        case .approved: ControlCenterCopy.approved
        case .denied: ControlCenterCopy.denied
        case .answeredElsewhere: ControlCenterCopy.answeredElsewhere
        case .expired: ControlCenterCopy.expired
        }
    }

    private static func outcomeSymbol(_ outcome: RequestCard.Outcome) -> String {
        switch outcome {
        case .approved: "checkmark"
        case .denied: "xmark"
        case .answeredElsewhere: "desktopcomputer"
        case .expired: "clock"
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            question
                .tutorialTip(.request)
            answers
            if let failure {
                Label(failure, systemImage: "exclamationmark.circle")
                    .font(.footnote)
                    .foregroundStyle(Color.togoSnow)
            }
        }
        // 20 pt matches the list's row inset, so card text lines up with the rows below.
        .padding(.horizontal, 20)
        .padding(.vertical, 16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(surface, in: .rect(cornerRadius: 16))
        .motion(.confirm, value: card.phase)
        .haptic(.approve, trigger: card.phase) { sent($0) == .approved }
        .haptic(.deny, trigger: card.phase) { sent($0) == .denied }
        .haptic(.error, trigger: failure) { $0 != nil }
    }

    // MARK: Question

    /// The question opens its session when there is one. Without one it is plain text, not a
    /// disabled button, so it never reads as dimmed.
    @ViewBuilder private var question: some View {
        if let onOpen {
            Button(action: onOpen) { questionText }
                .buttonStyle(.plain)
                .accessibilityHint(ControlCenterCopy.open)
        } else {
            questionText
        }
    }

    private var questionText: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                if let bib = card.bib {
                    BibText(bib)
                }
                Text(source)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
                    .lineLimit(dynamicTypeSize.isAccessibilitySize ? 4 : 1)
            }
            Text(card.title)
                .font(.headline)
                .foregroundStyle(Color.togoSnow)
            if let detail = card.detail {
                Text(verbatim: detail)
                    .font(.system(.footnote, design: .monospaced))
                    .foregroundStyle(Color.togoSnow)
                    .lineLimit(dynamicTypeSize.isAccessibilitySize ? 4 : 2)
                    .truncationMode(.middle)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(.rect)
    }

    /// At accessibility sizes the machine takes its own line, because it is what tells the user
    /// where the request comes from and would be the first thing truncated.
    private var source: String {
        guard let machineName else { return card.agentName }
        let separator = dynamicTypeSize.isAccessibilitySize ? "\n" : " · "
        return "\(card.agentName)\(separator)\(machineName)"
    }

    // MARK: Answers

    @ViewBuilder private var answers: some View {
        if case .done(let outcome) = card.phase {
            Label(Self.outcomeLabel(outcome), systemImage: Self.outcomeSymbol(outcome))
                .font(.body.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
                .frame(minHeight: 44)
                .transition(reduceMotion ? .opacity : .opacity.combined(with: .scale(scale: 0.96, anchor: .leading)))
        } else {
            VStack(alignment: .leading, spacing: 10) {
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 8) { buttons }
                    VStack(alignment: .leading, spacing: 4) { buttons }
                }
                if card.allowText {
                    replyField
                }
            }
            .disabled(isSending)
            .transition(.opacity)
        }
    }

    @ViewBuilder private var buttons: some View {
        let choices = Choices(card.options)
        if card.options.isEmpty && card.allowText {
            // A free-text question is answered in the reply field alone.
            EmptyView()
        } else if card.options.isEmpty {
            ApproveButton(ControlCenterCopy.approve) { onAnswer(.approve) }
            Button(ControlCenterCopy.deny) { onAnswer(.deny) }
                .buttonStyle(PlainAnswerStyle())
        } else {
            if let approve = choices.approve {
                ApproveButton(approve.label) { onAnswer(.option(approve.n)) }
            }
            if let deny = choices.deny {
                Button(deny.label) { onAnswer(.option(deny.n)) }
                    .buttonStyle(PlainAnswerStyle())
            }
            if !choices.more.isEmpty {
                Menu {
                    ForEach(choices.more, id: \.n) { option in
                        Button(option.label) { onAnswer(.option(option.n)) }
                    }
                } label: {
                    Text(ControlCenterCopy.more)
                }
                .buttonStyle(PlainAnswerStyle())
            }
        }
    }

    private var replyField: some View {
        HStack(spacing: 8) {
            TextField(ControlCenterCopy.reply, text: $reply)
                .font(.body)
                .foregroundStyle(Color.togoSnow)
                .submitLabel(.send)
                .onSubmit(sendReply)
            Button(ControlCenterCopy.send, systemImage: "arrow.up.circle.fill", action: sendReply)
                .labelStyle(.iconOnly)
                .font(.title2)
                .foregroundStyle(Color.togoSnow)
                .frame(minWidth: 44, minHeight: 44)
                .disabled(trimmedReply.isEmpty)
        }
        .padding(.leading, 12)
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Color.togoLine))
    }

    private var trimmedReply: String {
        reply.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private func sendReply() {
        guard !trimmedReply.isEmpty else { return }
        onAnswer(.text(trimmedReply))
    }

    private var isSending: Bool {
        if case .sending = card.phase { true } else { false }
    }

    /// The outcome an answer on its way will have, which picks the haptic as it leaves.
    private func sent(_ phase: RequestCard.Phase) -> RequestCard.Outcome? {
        guard case .sending(let decision) = phase else { return nil }
        return AppStore.outcome(of: decision, on: card)
    }
}

#if DEBUG
#Preview {
    VStack(spacing: 16) {
        RequestCardView(card: Samples.request(id: "r1", bib: "04", title: "Run npm test?"), machineName: "Studio") { _ in }
        RequestCardView(
            card: Samples.request(id: "r2", bib: "02", title: "Edit package.json?"), machineName: "Build box",
            failure: ControlCenterCopy.answerFailed("Build box")
        ) { _ in }
    }
    .padding()
    .background(Color.togoInk)
}
#endif
