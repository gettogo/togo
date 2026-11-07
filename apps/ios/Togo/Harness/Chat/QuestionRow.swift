import SwiftUI
import TogoKit

/// Questions the agent asked, answered inline. A single question with single choice answers
/// on tap; anything more collects the choices and sends them together.
///
/// Motion: a choice's circle fills with a symbol replace; while the answer is on its way the
/// card dims on `respond`; a failure line reveals with the error haptic; once the helper says
/// it is answered the card conceals and the row settles to its line (spec §2.3).
struct QuestionRow: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
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
    @State private var failures = 0
    @State private var shown: Mirrored<Bool>

    init(item: HarnessItem, question: HarnessItem.Question, send: @escaping ([String: [String]]) async -> Bool) {
        self.item = item
        self.question = question
        self.send = send
        _shown = State(initialValue: Mirrored(Self.isAnswered(question)))
    }

    static func isAnswered(_ question: HarnessItem.Question) -> Bool {
        !(question.answered ?? [:]).isEmpty
    }

    var body: some View {
        ZStack(alignment: .topLeading) {
            if shown.value {
                SettledLine(text: ChatCopy.answered, celebrates: shown.generation > 0)
                    .settling(shown)
            } else {
                card.settling(shown)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .haptic(.error, trigger: failures)
        .onChange(of: Self.isAnswered(question)) { _, answered in
            Motion.settle($shown, to: answered, reduce: reduceMotion)
        }
    }

    private var card: some View {
        VStack(alignment: .leading, spacing: 14) {
            NeedsHeader()
            ForEach(question.questions) { q in
                questionView(q)
            }
            if !answersOnTap {
                ApproveButton(ChatCopy.send, busy: sending) { submit() }
                    .disabled(!complete || sending || sent)
            }
            if failed {
                Text(ChatCopy.answerFailed)
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                    .transition(Motion.revealConceal(reduce: reduceMotion))
            }
        }
        .allowsHitTesting(!(sending || sent))
        .opacity(sending || sent ? 0.6 : 1)
        .motion(.respond, value: sending || sent)
        .clipped()
        .modifier(NeedsCard())
        .accessibilityElement(children: .contain)
        .accessibilityLabel(ChatCopy.spokenNeeds(question.questions.first?.prompt ?? ""))
        .onChange(of: item.seq) { sent = false }
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
                let chosen = (picked[q.id] ?? []).contains(option.id)
                Button { choose(option.id, in: q) } label: {
                    HStack(alignment: .firstTextBaseline, spacing: 10) {
                        Image(systemName: chosen ? "checkmark.circle.fill" : "circle")
                            .foregroundStyle(Color.togoMute)
                            .contentTransition(reduceMotion ? .identity : .symbolEffect(.replace))
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
                .accessibilityAddTraits(chosen ? .isSelected : [])
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
        withAnimation(Motion.animation(.respond, reduce: reduceMotion)) { picked[q.id] = set }
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
        withAnimation(Motion.animation(.conceal, reduce: reduceMotion)) { failed = false }
        Task { await finish(answers) }
    }

    private func finish(_ answers: [String: [String]]) async {
        let ok = await send(answers)
        sent = ok
        sending = false
        guard !ok else { return }
        failures += 1
        withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { failed = true }
    }
}
