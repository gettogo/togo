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
    /// Why the last answer to a request did not arrive, while it is still open.
    var failure: @MainActor (_ requestID: String) -> String? = { _ in nil }
    /// Opens a link tapped in the chat: web pages, files, anything else (`ChatLinks`).
    var open: @MainActor (_ url: URL) -> OpenURLAction.Result = { _ in .systemAction }

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
/// Motion (motion principles v2): the card's body unfolds when the card arrives for a request
/// that waited in the terminal; More unfolds its list; the answer the person tapped shows its
/// progress in place while the others dim, then becomes the answered line ("Approve" travels
/// into "Approved"). Answered anywhere else, the card collapses in place to its line and the
/// check bounces once.
struct ApprovalRow: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let item: HarnessItem
    let approval: HarnessItem.Approval
    let card: RequestCard?
    /// Why the last answer did not arrive, while the request is still open.
    var failure: String?
    /// The way to the prompt in the terminal, when the item has a place there.
    var showInTerminal: (() -> Void)?
    let answer: (Decision) -> Void

    @Namespace private var answerSpace
    @State private var approvals = 0
    @State private var denials = 0
    /// The answer the person tapped here, so the answered line comes out of it.
    @State private var tapped = AnswerTap<Decision>()
    /// How the card last settled, so the line's check bounces only when nothing here caused it.
    @State private var settle: AnswerSettle = .collapse
    @State private var more = Disclosure()
    @State private var shown: Mirrored<NeedsShape>
    /// The card as last seen, so it can conceal after the control center drops it.
    @State private var lastCard: RequestCard?

    init(
        item: HarnessItem, approval: HarnessItem.Approval, card: RequestCard?, failure: String? = nil,
        showInTerminal: (() -> Void)? = nil, answer: @escaping (Decision) -> Void
    ) {
        self.item = item
        self.approval = approval
        self.card = card
        self.failure = failure
        self.showInTerminal = showInTerminal
        self.answer = answer
        _shown = State(initialValue: Mirrored(Self.shape(approval: approval, card: card)))
        _lastCard = State(initialValue: card)
    }

    var body: some View {
        ZStack(alignment: .topLeading) {
            switch shown.value {
            case .settled(let text):
                SettledLine(text: text, celebrates: shown.generation > 0 && settle.bounces)
                    .answerSource(AnswerMatch.id(item.id), in: answerSpace, active: settle == .travel)
                    .settling(shown)
            case .waiting, .card:
                cardView
                    .settling(shown)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .haptic(.approve, trigger: approvals)
        .haptic(.deny, trigger: denials)
        .haptic(.error, trigger: failure) { _, new in new != nil }
        .onChange(of: card) { _, card in if let card { lastCard = card } }
        .onChange(of: target) { _, target in follow(target) }
    }

    /// The card arriving for a request that waited in the terminal grows in place on its
    /// ground. The person's own answer becomes the line in one transaction; any other answer
    /// conceals the card first, then the row shrinks to its line (spec §2.3).
    private func follow(_ target: NeedsShape) {
        switch (shown.value, target) {
        case (.waiting, .card), (.card, .waiting):
            // motion: preventing a jump — the card grows in place as its body unfolds under the header.
            withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { shown.value = target }
        case (_, .settled):
            settle = tapped.settle(answeredHere: Self.answeredHere(approval: approval, card: card))
            guard settle == .travel else {
                Motion.settle($shown, to: target, reduce: reduceMotion)
                return
            }
            shown.generation += 1
            // motion: cause → effect — the tapped answer travels into the answered line as the card shrinks around it.
            withAnimation(Motion.animation(.resize, reduce: reduceMotion)) {
                shown.value = target
                shown.shown = true
            }
        default:
            Motion.settle($shown, to: target, reduce: reduceMotion)
        }
    }

    /// The request was answered by this phone (approved or denied here), not elsewhere and not
    /// by expiring.
    static func answeredHere(approval: HarnessItem.Approval, card: RequestCard?) -> Bool {
        if let card, case .done(let outcome) = card.phase { return outcome == .approved || outcome == .denied }
        return approval.resolved == "phone"
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
                    if let failure, card.phase == .open {
                        // Instant, at the answers that failed, with the error haptic.
                        Text(failure)
                            .font(.footnote)
                            .foregroundStyle(Color.togoSnow)
                            .fixedSize(horizontal: false, vertical: true)
                            .accessibilityIdentifier("chat.needs.failure")
                    }
                }
                // motion: state change — the card's body is uncovered under the header as the card grows.
                .transition(Motion.unfold(reduce: reduceMotion))
            } else {
                // Not answerable from here: the prompt waits in the terminal, and the card
                // says so with the way there. Never an Approve or Deny that cannot work.
                VStack(alignment: .leading, spacing: 8) {
                    Text(ChatCopy.waitingInTerminal)
                        .font(.subheadline)
                        .foregroundStyle(Color.togoMute)
                    if let showInTerminal {
                        Button(ChatCopy.showInTerminal, systemImage: "terminal", action: showInTerminal)
                            .buttonStyle(PlainAnswerStyle())
                            .font(.subheadline)
                    }
                }
                // motion: state change — the terminal line is uncovered in place as the card changes shape.
                .transition(Motion.unfold(reduce: reduceMotion))
            }
        }
        .modifier(NeedsCard())
        .accessibilityElement(children: .combine)
        .accessibilityLabel(ChatCopy.spokenNeeds(shownCard?.title ?? ChatCopy.waitingInTerminal))
        .accessibilityIdentifier("chat.needs.approval")
        // A double-tap never answers: the card is heard as its request only, so Approve and Deny
        // are named actions. It shows the prompt in the terminal, when it has a place there.
        .accessibilityAction { showInTerminal?() }
        .accessibilityActions {
            if let card, card.phase == .open {
                if Self.yesOrNo(card) {
                    Button(approveLabel(card)) { approve(card) }
                    Button(ChatCopy.deny) { deny(card) }
                }
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
        guard let card, answerable(card) else { return .waiting }
        return .card
    }

    /// A request the phone can answer: it offers options, or takes words. One with neither (a
    /// hook saw the prompt before the screen did) waits in the terminal.
    static func answerable(_ card: RequestCard) -> Bool {
        !card.options.isEmpty || card.allowText
    }

    /// Yes and No in a row, stacked when the labels are too long for one. Extra answers open
    /// as a list under them, each with its full words, never a menu that clips them.
    private func answers(_ card: RequestCard) -> some View {
        let extra = extraOptions(card)
        return VStack(alignment: .leading, spacing: 10) {
            if Self.yesOrNo(card) {
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 8) { answerButtons(card, hasExtra: !extra.isEmpty) }
                    VStack(alignment: .leading, spacing: 8) { answerButtons(card, hasExtra: !extra.isEmpty) }
                }
            }
            // More: the card's growing edge uncovers the extra answers top to bottom. A list
            // of choices with no yes and no is the answers, open from the start.
            if !Self.yesOrNo(card) || more.open, !extra.isEmpty {
                AnswerOptionList(
                    options: extra, sending: sendingOption(card),
                    source: { tapped.isSource(.option($0)) ? (id: AnswerMatch.id(item.id), namespace: answerSpace) : nil }
                ) { pick($0) }
                    .unfolds(more)
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
            .answerSource(AnswerMatch.id(item.id), in: answerSpace, active: tapped.isSource(approveDecision(card)))
        Button { deny(card) } label: {
            BusyLabel(title: ChatCopy.deny, busy: sending == denyDecision(card))
        }
        .buttonStyle(PlainAnswerStyle())
        .opacity(Self.dims(sending, keeping: denyDecision(card)) ? 0.4 : 1)
        .answerSource(AnswerMatch.id(item.id), in: answerSpace, active: tapped.isSource(denyDecision(card)))
        if hasExtra {
            Button { Motion.toggle($more, reduce: reduceMotion) } label: {
                Text(more.isOpen ? AnswerOptionsCopy.hide : ChatCopy.more)
                    .contentTransition(.interpolate)
            }
            .buttonStyle(PlainAnswerStyle())
            .opacity(sending == nil ? 1 : 0.4)
            // motion: cause → effect — "More" becomes "Fewer" in place as the list opens under it.
            .motion(.respond, value: more.isOpen)
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
            .filter { !Self.yesOrNo(card) || ($0.role != .approve && $0.role != .deny) }
            .map { AnswerOption(id: $0.n, label: $0.label) }
    }

    /// Yes and No are drawn only when the agent offers both. A question with choices has no
    /// Yes: one invented for its first choice would pick that choice.
    static func yesOrNo(_ card: RequestCard) -> Bool {
        card.options.contains { $0.role == .approve } && card.options.contains { $0.role == .deny }
    }

    private func approve(_ card: RequestCard) {
        approvals += 1
        tapped.record(approveDecision(card))
        answer(approveDecision(card))
    }

    private func deny(_ card: RequestCard) {
        denials += 1
        tapped.record(denyDecision(card))
        answer(denyDecision(card))
    }

    private func pick(_ option: AnswerOption) {
        tapped.record(.option(option.id))
        answer(.option(option.id))
    }

    /// The approve option's own words; any " · " explanation is left to the hint.
    private func approveLabel(_ card: RequestCard) -> String {
        card.options.first { $0.role == .approve }.map { AnswerOptionText.split($0.label).title } ?? ChatCopy.approve
    }

    private func approveDecision(_ card: RequestCard) -> Decision {
        Self.approveDecision(card)
    }

    private func denyDecision(_ card: RequestCard) -> Decision {
        Self.denyDecision(card)
    }

    static func approveDecision(_ card: RequestCard) -> Decision {
        card.options.first { $0.role == .approve }.map { .option($0.n) } ?? .approve
    }

    static func denyDecision(_ card: RequestCard) -> Decision {
        card.options.first { $0.role == .deny }.map { .option($0.n) } ?? .deny
    }
}

/// ⌘↩ approves and ⌘. denies the topmost open request in a chat, from a hardware keyboard,
/// even while the composer holds the keyboard.
enum ChatKeyAnswers {
    struct Target: Equatable {
        var requestID: String
        var approve: Decision
        var deny: Decision
    }

    /// The first request, top to bottom, that is open and answers yes or no here.
    @MainActor static func target(requestIDs: [String], card: (String) -> RequestCard?) -> Target? {
        for id in requestIDs {
            guard let card = card(id), card.phase == .open, !card.options.isEmpty else { continue }
            let roles = Set(card.options.map(\.role))
            guard roles.contains(.approve), roles.contains(.deny) else { continue }
            return Target(requestID: id, approve: ApprovalRow.approveDecision(card), deny: ApprovalRow.denyDecision(card))
        }
        return nil
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
                }
            }
            // motion: cause → effect — the tapped answer's words give way to a spinner in their own room.
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

/// An answered request, settled into one quiet line. When it settles on screen from an answer
/// given elsewhere its check bounces once; the person's own answer and history show it still.
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
