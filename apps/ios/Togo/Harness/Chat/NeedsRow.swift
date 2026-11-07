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

/// An approval inline in the chat: the one place it uses the beacon, a card on its faint
/// moving light, with the same approve button as the prompt bar. Never folded; once answered
/// it settles into a quiet line.
struct ApprovalRow: View {
    let item: HarnessItem
    let approval: HarnessItem.Approval
    let card: RequestCard?
    let answer: (Decision) -> Void

    @State private var approvals = 0
    @State private var denials = 0
    @State private var showingMore = false

    var body: some View {
        content
            .haptic(.approve, trigger: approvals)
            .haptic(.deny, trigger: denials)
    }

    @ViewBuilder private var content: some View {
        if let settled {
            SettledLine(text: settled)
        } else {
            VStack(alignment: .leading, spacing: 10) {
                NeedsHeader()
                if let card {
                    Text(card.title)
                        .font(.body)
                        .foregroundStyle(Color.togoSnow)
                        .fixedSize(horizontal: false, vertical: true)
                    if let detail = card.detail, !detail.isEmpty {
                        CodeWell(text: detail)
                    }
                    answers(card)
                } else {
                    Text(ChatCopy.waitingInTerminal)
                        .font(.subheadline)
                        .foregroundStyle(Color.togoMute)
                }
            }
            .modifier(NeedsCard())
            .accessibilityElement(children: .combine)
            .accessibilityLabel(ChatCopy.spokenNeeds(card?.title ?? ChatCopy.waitingInTerminal))
            .accessibilityActions {
                if let card, card.phase == .open {
                    Button(approveLabel(card)) { approve(card) }
                    Button(ChatCopy.deny) { deny(card) }
                    ForEach(extraOptions(card)) { option in
                        Button(option.title) { answer(.option(option.id)) }
                    }
                }
            }
        }
    }

    /// The answered state, from the item or the card, whichever knows first.
    private var settled: String? {
        if let card, case .done(let outcome) = card.phase {
            return switch outcome {
            case .approved: ChatCopy.approved
            case .denied: ChatCopy.denied
            case .answeredElsewhere: ChatCopy.answeredElsewhere
            case .expired: ChatCopy.expired
            }
        }
        return approval.resolved.map(ChatCopy.resolved)
    }

    /// Yes and No in a row, stacked when the labels are too long for one. Extra answers open
    /// as a list under them, each with its full words, never a menu that clips them.
    private func answers(_ card: RequestCard) -> some View {
        let sending = card.phase != .open
        let extra = extraOptions(card)
        return VStack(alignment: .leading, spacing: 10) {
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 8) { answerButtons(card, hasExtra: !extra.isEmpty) }
                VStack(alignment: .leading, spacing: 8) { answerButtons(card, hasExtra: !extra.isEmpty) }
            }
            if showingMore, !extra.isEmpty {
                AnswerOptionList(options: extra) { answer(.option($0.id)) }
                    .transition(.opacity)
            }
        }
        .motion(.rowGrow, value: showingMore)
        .disabled(sending)
    }

    @ViewBuilder private func answerButtons(_ card: RequestCard, hasExtra: Bool) -> some View {
        ApproveButton(approveLabel(card)) { approve(card) }
        Button(ChatCopy.deny) { deny(card) }
            .buttonStyle(PlainAnswerStyle())
        if hasExtra {
            Button(showingMore ? AnswerOptionsCopy.hide : ChatCopy.more) { showingMore.toggle() }
                .buttonStyle(PlainAnswerStyle())
                .accessibilityValue(showingMore ? Text(AnswerOptionsCopy.hide) : Text(ChatCopy.more))
        }
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

/// Questions the agent asked, answered inline. A single question with single choice answers
/// on tap; anything more collects the choices and sends them together.
struct QuestionRow: View {
    let item: HarnessItem
    let question: HarnessItem.Question
    let send: ([String: [String]]) async -> Bool

    @State private var picked: [String: Set<String>] = [:]
    @State private var typed: [String: String] = [:]
    @State private var sending = false
    /// The answer went through: the options stay closed until the item itself updates, so a
    /// second tap cannot answer again in the meantime.
    @State private var sent = false
    @State private var failed = false

    var body: some View {
        if let answered = question.answered, !answered.isEmpty {
            SettledLine(text: ChatCopy.answered)
        } else {
            VStack(alignment: .leading, spacing: 14) {
                NeedsHeader()
                ForEach(question.questions) { q in
                    questionView(q)
                }
                if !answersOnTap {
                    ApproveButton(ChatCopy.send) { submit() }
                        .disabled(!complete || sending || sent)
                }
                if failed {
                    Text(ChatCopy.answerFailed)
                        .font(.footnote)
                        .foregroundStyle(Color.togoMute)
                }
            }
            .disabled(sending || sent)
            .modifier(NeedsCard())
            .accessibilityElement(children: .contain)
            .accessibilityLabel(ChatCopy.spokenNeeds(question.questions.first?.prompt ?? ""))
            .onChange(of: item.seq) { sent = false }
        }
    }

    private var answersOnTap: Bool {
        question.questions.count == 1 && !(question.questions[0].multi || question.questions[0].allowText)
    }

    private var complete: Bool {
        question.questions.allSatisfy { q in
            !(picked[q.id] ?? []).isEmpty || !(typed[q.id] ?? "").trimmingCharacters(in: .whitespaces).isEmpty
        }
    }

    @ViewBuilder private func questionView(_ q: HarnessQuestion) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            if let header = q.header {
                Text(header)
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color.togoMute)
            }
            Text(q.prompt)
                .font(.body)
                .foregroundStyle(Color.togoSnow)
                .fixedSize(horizontal: false, vertical: true)
            ForEach(q.options) { option in
                Button { choose(option.id, in: q) } label: {
                    HStack(alignment: .firstTextBaseline, spacing: 10) {
                        Image(systemName: (picked[q.id] ?? []).contains(option.id) ? "checkmark.circle.fill" : "circle")
                            .foregroundStyle(Color.togoMute)
                            .accessibilityHidden(true)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: option.shownTitle).foregroundStyle(Color.togoSnow)
                            if let detail = option.shownDetail {
                                Text(verbatim: detail).font(.footnote).foregroundStyle(Color.togoMute)
                            }
                        }
                        .fixedSize(horizontal: false, vertical: true)
                        Spacer(minLength: 0)
                    }
                    .frame(minHeight: 44)
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits((picked[q.id] ?? []).contains(option.id) ? .isSelected : [])
            }
            if q.allowText {
                TextField(ChatCopy.answerPlaceholder, text: binding(for: q.id), axis: .vertical)
                    .textFieldStyle(.plain)
                    .padding(12)
                    .background(Color.togoRaised, in: .rect(cornerRadius: 12, style: .continuous))
            }
        }
    }

    private func binding(for id: String) -> Binding<String> {
        Binding { typed[id] ?? "" } set: { typed[id] = $0 }
    }

    private func choose(_ option: String, in q: HarnessQuestion) {
        var set = picked[q.id] ?? []
        if q.multi {
            if set.contains(option) { set.remove(option) } else { set.insert(option) }
        } else {
            set = [option]
        }
        picked[q.id] = set
        if answersOnTap { submit() }
    }

    private func submit() {
        guard !sending, !sent else { return }
        var answers: [String: [String]] = [:]
        for q in question.questions {
            var values = q.options.map(\.id).filter { (picked[q.id] ?? []).contains($0) }
            let text = (typed[q.id] ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
            if !text.isEmpty { values.append(text) }
            answers[q.id] = values
        }
        sending = true
        failed = false
        Task {
            let ok = await send(answers)
            sent = ok
            sending = false
            failed = !ok
        }
    }
}

/// "Needs you" in the beacon, the only accent in the chat.
private struct NeedsHeader: View {
    var body: some View {
        StatusWord(status: .needsYou, font: .footnote)
            .accessibilityAddTraits(.isHeader)
    }
}

/// An answered request, settled into one quiet line.
private struct SettledLine: View {
    let text: String

    var body: some View {
        Label(text, systemImage: "checkmark")
            .font(.footnote)
            .foregroundStyle(Color.togoMute)
            .frame(minHeight: 32, alignment: .leading)
    }
}
