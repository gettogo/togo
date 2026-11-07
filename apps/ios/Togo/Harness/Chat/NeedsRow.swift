import SwiftUI
import TogoKit

/// What the chat needs from the window around it to answer approvals.
struct ChatActions {
    /// The open request an approval item points at, when the control center lists it.
    var card: @MainActor (_ requestID: String) -> RequestCard?
    /// Answers it through the same path as the prompt bar, the Watch and push.
    var answer: @MainActor (_ requestID: String, _ decision: Decision) -> Void
    /// Puts quoted text in the composer.
    var quote: @MainActor (_ text: String) -> Void

    @MainActor static let inert = ChatActions(card: { _ in nil }, answer: { _, _ in }, quote: { _ in })
}

/// What an approval row shows: the card still waiting in the terminal, the card with its
/// answers, or the quiet line it settles into once answered.
enum NeedsShape: Equatable {
    case waiting
    case card
    case settled(String)
}

/// An approval inline in the chat: the one place it uses the beacon, a card on its faint
/// moving light, with the same approve button as the prompt bar. Never folded; once answered
/// it settles into a quiet line.
///
/// Motion (spec §2.1–§2.3): More opens and closes as a two-phase disclosure; the answer the
/// person tapped shows its progress in place while the others dim; the helper's answer is
/// mirrored, so the card conceals, then the row shrinks to its line while the line reveals.
struct ApprovalRow: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let item: HarnessItem
    let approval: HarnessItem.Approval
    let card: RequestCard?
    let answer: (Decision) -> Void

    @State private var approvals = 0
    @State private var denials = 0
    @State private var more = Disclosure()
    @State private var shown: Mirrored<NeedsShape>
    /// The card as last seen, so it can conceal after the control center drops it.
    @State private var lastCard: RequestCard?

    init(item: HarnessItem, approval: HarnessItem.Approval, card: RequestCard?, answer: @escaping (Decision) -> Void) {
        self.item = item
        self.approval = approval
        self.card = card
        self.answer = answer
        _shown = State(initialValue: Mirrored(Self.shape(approval: approval, card: card)))
        _lastCard = State(initialValue: card)
    }

    var body: some View {
        ZStack(alignment: .topLeading) {
            switch shown.value {
            case .settled(let text):
                SettledLine(text: text, celebrates: shown.generation > 0)
                    .settling(shown)
            case .waiting, .card:
                cardView
                    .settling(shown)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .haptic(.approve, trigger: approvals)
        .haptic(.deny, trigger: denials)
        .onChange(of: card) { _, card in if let card { lastCard = card } }
        .onChange(of: target) { _, target in follow(target) }
    }

    /// The card arriving for a request that waited in the terminal grows in place on its
    /// ground; settling into the answered line conceals the card first (spec §2.3).
    private func follow(_ target: NeedsShape) {
        switch (shown.value, target) {
        case (.waiting, .card), (.card, .waiting):
            withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { shown.value = target }
        default:
            Motion.settle($shown, to: target, reduce: reduceMotion)
        }
    }

    private var target: NeedsShape { Self.shape(approval: approval, card: card) }

    private var shownCard: RequestCard? { card ?? lastCard }

    private var cardView: some View {
        VStack(alignment: .leading, spacing: 10) {
            NeedsHeader()
            if shown.value == .card, let card = shownCard {
                VStack(alignment: .leading, spacing: 10) {
                    Text(card.title)
                        .font(.body)
                        .foregroundStyle(Color.togoSnow)
                        .fixedSize(horizontal: false, vertical: true)
                    if let detail = card.detail, !detail.isEmpty {
                        CodeWell(text: detail)
                    }
                    answers(card)
                }
                .transition(Motion.revealConceal(reduce: reduceMotion))
            } else {
                Text(ChatCopy.waitingInTerminal)
                    .font(.subheadline)
                    .foregroundStyle(Color.togoMute)
                    .transition(Motion.revealConceal(reduce: reduceMotion))
            }
        }
        .modifier(NeedsCard())
        .accessibilityElement(children: .combine)
        .accessibilityLabel(ChatCopy.spokenNeeds(shownCard?.title ?? ChatCopy.waitingInTerminal))
        .accessibilityActions {
            if let card, card.phase == .open {
                Button(approveLabel(card)) { approve(card) }
                Button(ChatCopy.deny) { deny(card) }
                ForEach(extraOptions(card)) { option in
                    Button(option.title) { pick(option) }
                }
            }
        }
    }

    /// The answered state, from the item or the card, whichever knows first.
    static func shape(approval: HarnessItem.Approval, card: RequestCard?) -> NeedsShape {
        if let card, case .done(let outcome) = card.phase {
            let words: String = switch outcome {
            case .approved: ChatCopy.approved
            case .denied: ChatCopy.denied
            case .answeredElsewhere: ChatCopy.answeredElsewhere
            case .expired: ChatCopy.expired
            }
            return .settled(words)
        }
        if let resolved = approval.resolved { return .settled(ChatCopy.resolved(resolved)) }
        return card == nil ? .waiting : .card
    }

    /// Yes and No in a row, stacked when the labels are too long for one. Extra answers open
    /// as a list under them, each with its full words, never a menu that clips them.
    private func answers(_ card: RequestCard) -> some View {
        let extra = extraOptions(card)
        return VStack(alignment: .leading, spacing: 10) {
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 8) { answerButtons(card, hasExtra: !extra.isEmpty) }
                VStack(alignment: .leading, spacing: 8) { answerButtons(card, hasExtra: !extra.isEmpty) }
            }
            if more.open, !extra.isEmpty {
                AnswerOptionList(options: extra, revealing: true, sending: sendingOption(card)) { pick($0) }
                    .disclosed(more)
            }
        }
        // Never draws outside the card while it shrinks.
        .clipped()
        // Taps stop once an answer is on its way; the look is the buttons' own, not a dim.
        .allowsHitTesting(card.phase == .open)
    }

    @ViewBuilder private func answerButtons(_ card: RequestCard, hasExtra: Bool) -> some View {
        let sending = sendingDecision(card)
        ApproveButton(approveLabel(card), busy: sending == approveDecision(card)) { approve(card) }
            .opacity(Self.dims(sending, keeping: approveDecision(card)) ? 0.4 : 1)
        Button { deny(card) } label: {
            BusyLabel(title: ChatCopy.deny, busy: sending == denyDecision(card))
        }
        .buttonStyle(PlainAnswerStyle())
        .opacity(Self.dims(sending, keeping: denyDecision(card)) ? 0.4 : 1)
        if hasExtra {
            Button { Motion.toggle($more, reduce: reduceMotion) } label: {
                Text(more.isOpen ? AnswerOptionsCopy.hide : ChatCopy.more)
                    .contentTransition(.interpolate)
            }
            .buttonStyle(PlainAnswerStyle())
            .opacity(sending == nil ? 1 : 0.4)
            .motion(.respond, value: more.isOpen)
            .accessibilityValue(more.isOpen ? Text(AnswerOptionsCopy.hide) : Text(ChatCopy.more))
        }
    }

    /// A button dims while another answer is on its way.
    static func dims(_ sending: Decision?, keeping decision: Decision) -> Bool {
        sending != nil && sending != decision
    }

    private func sendingDecision(_ card: RequestCard) -> Decision? {
        if case .sending(let decision) = card.phase { return decision }
        return nil
    }

    private func sendingOption(_ card: RequestCard) -> Int? {
        if case .option(let n) = sendingDecision(card) { return n }
        return nil
    }

    private func extraOptions(_ card: RequestCard) -> [AnswerOption] {
        card.options
            .filter { $0.role != .approve && $0.role != .deny }
            .map { AnswerOption(id: $0.n, label: $0.label) }
    }

    private func approve(_ card: RequestCard) {
        approvals += 1
        answer(approveDecision(card))
    }

    private func deny(_ card: RequestCard) {
        denials += 1
        answer(denyDecision(card))
    }

    private func pick(_ option: AnswerOption) {
        answer(.option(option.id))
    }

    /// The approve option's own words; any " · " explanation is left to the hint.
    private func approveLabel(_ card: RequestCard) -> String {
        card.options.first { $0.role == .approve }.map { AnswerOptionText.split($0.label).title } ?? ChatCopy.approve
    }

    private func approveDecision(_ card: RequestCard) -> Decision {
        card.options.first { $0.role == .approve }.map { .option($0.n) } ?? .approve
    }

    private func denyDecision(_ card: RequestCard) -> Decision {
        card.options.first { $0.role == .deny }.map { .option($0.n) } ?? .deny
    }
}

/// A plain answer's words, or a spinner in their place while that answer is on its way; the
/// words keep their room, so nothing reflows.
struct BusyLabel: View {
    let title: String
    let busy: Bool

    var body: some View {
        Text(title)
            .opacity(busy ? 0 : 1)
            .overlay {
                if busy {
                    ProgressView()
                        .controlSize(.small)
                        .transition(.opacity)
                }
            }
            .motion(.respond, value: busy)
    }
}

/// "Needs you" in the beacon, the only accent in the chat.
struct NeedsHeader: View {
    var body: some View {
        StatusWord(status: .needsYou, font: .footnote)
            .accessibilityAddTraits(.isHeader)
    }
}

/// An answered request, settled into one quiet line. When it settles while on screen its
/// check bounces once; history shows it still.
struct SettledLine: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let text: String
    var celebrates = false

    @State private var bounces = 0

    var body: some View {
        Label {
            Text(text)
        } icon: {
            Image(systemName: "checkmark")
                .symbolEffect(.bounce, options: .nonRepeating, value: bounces)
        }
        .font(.footnote)
        .foregroundStyle(Color.togoMute)
        .frame(minHeight: 32, alignment: .leading)
        .onAppear { if celebrates, !reduceMotion { bounces += 1 } }
    }
}
