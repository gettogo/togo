import SwiftUI
import TogoKit

/// Questions the agent asked, answered inline. A single single-choice question answers on tap;
/// anything more collects the choices and sends them together. Built on the Confirm model, so
/// it keeps the prompt bar's guards: answers work only after the card has been on screen for
/// the arming delay, and a question that changes under the user says so.
///
/// Motion: a choice's circle fills with a symbol replace; while the answer is on its way the
/// card dims on `respond`; a failure says why at once, at Send, with the error haptic. The
/// person's own answer becomes the answered line: the Send button (or the tapped choice)
/// travels into "Answered". Answered anywhere else, the card collapses in place to its line.
struct QuestionRow: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let item: HarnessItem
    let question: HarnessItem.Question
    /// The way to the question in the terminal, when the item has a place there.
    var showInTerminal: (() -> Void)?
    /// Sends `{"answers": …}` and returns what the helper said; nil when nothing came back.
    let send: ([String: JSONValue]) async -> HarnessActionResult?

    @State private var model: ConfirmModel
    /// Questions whose "Other answer" field is open, and the one holding the keyboard.
    @State private var others = RevealedFields<String>()
    @FocusState private var typing: String?
    @State private var shown: Mirrored<Bool>
    @Namespace private var answerSpace
    /// What the person tapped to answer: `sendSource`, or the choice that answered on its tap.
    @State private var tapped = AnswerTap<String>()
    @State private var settle: AnswerSettle = .collapse

    /// The Send button, as an answer source.
    static let sendSource = "send"

    init(
        item: HarnessItem, question: HarnessItem.Question, showInTerminal: (() -> Void)?,
        send: @escaping ([String: JSONValue]) async -> HarnessActionResult?
    ) {
        self.item = item
        self.question = question
        self.showInTerminal = showInTerminal
        self.send = send
        _model = State(initialValue: ConfirmModel(confirm: Self.confirm(question), arms: false))
        _shown = State(initialValue: Mirrored(Self.isAnswered(question)))
    }

    /// The plain form: answers per question id, true when they went through.
    init(item: HarnessItem, question: HarnessItem.Question, send: @escaping ([String: [String]]) async -> Bool) {
        self.init(item: item, question: question, showInTerminal: nil) { args in
            await send(Self.answers(args)) ? HarnessActionResult(result: .applied) : HarnessActionResult(result: .failed)
        }
    }

    /// `{"answers": {id: [values]}}` back into plain answers.
    static func answers(_ args: [String: JSONValue]) -> [String: [String]] {
        guard case .object(let object)? = args["answers"] else { return [:] }
        return object.mapValues { value in
            guard case .array(let values) = value else { return [] }
            return values.compactMap { if case .string(let text) = $0 { text } else { nil } }
        }
    }

    static func isAnswered(_ question: HarnessItem.Question) -> Bool {
        !(question.answered ?? [:]).isEmpty
    }

    /// The question as the Confirm model holds it.
    static func confirm(_ question: HarnessItem.Question) -> HarnessScreen.Confirm {
        HarnessScreen.Confirm(questions: question.questions, submit: ChatCopy.send)
    }

    /// Whether the phone can answer it; a harness without an answer route shows it read-only.
    static func answerable(_ question: HarnessItem.Question) -> Bool {
        question.isAnswerable
    }

    var body: some View {
        ZStack(alignment: .topLeading) {
            if shown.value {
                SettledLine(text: ChatCopy.answered, celebrates: shown.generation > 0 && settle.bounces)
                    .answerSource(AnswerMatch.id(item.id), in: answerSpace, active: settle == .travel)
                    .settling(shown)
            } else {
                card.settling(shown)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .haptic(.error, trigger: model.phase) { _, new in
            if case .failed = new { true } else { false }
        }
        .onAppear { model.beginArming() }
        .onChange(of: Self.isAnswered(question)) { _, answered in follow(answered) }
        .onChange(of: question) { _, new in model.update(Self.confirm(new)) }
        // The helper took the answer but the item changed without closing: it can go again.
        .onChange(of: item.seq) { model.reopen() }
        .onChange(of: typing) { _, now in others.focusMoved(to: now) }
    }

    /// The person's own answer becomes the line in one transaction; any other conceals the
    /// card first, then the row shrinks to its line.
    private func follow(_ answered: Bool) {
        settle = answered ? tapped.settle(answeredHere: Self.answeredHere(model.phase)) : .collapse
        guard settle == .travel, !shown.value else {
            Motion.settle($shown, to: answered, reduce: reduceMotion)
            return
        }
        shown.generation += 1
        // motion: cause → effect — what the person tapped travels into the answered line as the card shrinks around it.
        withAnimation(Motion.animation(.resize, reduce: reduceMotion)) {
            shown.value = true
            shown.shown = true
        }
    }

    /// This card sent the answer that closed the question.
    static func answeredHere(_ phase: ConfirmModel.Phase) -> Bool {
        switch phase {
        case .sending, .answered: true
        case .open, .failed: false
        }
    }

    private var locked: Bool {
        switch model.phase {
        case .sending, .answered: true
        case .open, .failed: false
        }
    }

    private var card: some View {
        let answerable = Self.answerable(question)
        return VStack(alignment: .leading, spacing: 14) {
            NeedsHeader()
            ForEach(question.questions) { q in
                questionView(q, answerable: answerable)
            }
            if answerable {
                if showsSend {
                    ApproveButton(ChatCopy.send, busy: model.phase == .sending) { submit(from: Self.sendSource) }
                        .disabled(!model.isComplete || locked)
                        .answerSource(AnswerMatch.id(item.id), in: answerSpace, active: tapped.isSource(Self.sendSource))
                }
                status
            } else {
                terminalOnly(ChatCopy.answerInTerminal)
            }
        }
        // motion: state change — the card dims in place while its answer is on its way.
        .opacity(locked ? 0.6 : 1)
        .motion(.respond, value: locked)
        .clipped()
        .modifier(NeedsCard())
        .accessibilityElement(children: .contain)
        .accessibilityLabel(ChatCopy.spokenNeeds(question.questions.first?.prompt ?? ""))
        .accessibilityIdentifier("chat.needs.question")
    }

    /// Why the last answer did not land, at the Send button, instantly: errors never fly, and
    /// the card's dimming animation does not reach them.
    @ViewBuilder private var status: some View {
        Group {
            if case .failed(let message) = model.phase {
                line(message.isEmpty ? ChatCopy.answerFailed : message)
            } else if model.handedOff {
                terminalOnly(ChatCopy.finishInTerminal)
            } else if model.changed {
                line(ChatCopy.questionChanged)
            }
        }
        .transaction { $0.animation = nil }
    }

    private func line(_ text: String) -> some View {
        Text(text)
            .font(.footnote)
            .foregroundStyle(Color.togoSnow)
            .fixedSize(horizontal: false, vertical: true)
            .accessibilityIdentifier("chat.needs.failure")
    }

    /// A line saying where to finish, with the way there when the item has a place in the terminal.
    private func terminalOnly(_ text: String) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(text)
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
            if let showInTerminal {
                Button(ChatCopy.showInTerminal, systemImage: "terminal", action: showInTerminal)
                    .buttonStyle(PlainAnswerStyle())
                    .font(.subheadline)
            }
        }
    }

    /// One single-choice question answers on its tap; anything else, or typed words, sends.
    private var answersOnTap: Bool {
        question.questions.count == 1 && !question.questions[0].multi && !question.questions[0].options.isEmpty
    }

    private var showsSend: Bool {
        !answersOnTap || !others.isEmpty
    }

    @ViewBuilder private func questionView(_ q: HarnessQuestion, answerable: Bool) -> some View {
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
                optionRow(option, in: q)
            }
            // A card that cannot be answered here lists the options only.
            .allowsHitTesting(answerable && model.armed && !locked)
            if answerable, q.allowText {
                if q.options.isEmpty || others.isOpen(q.id) {
                    TextField(ChatCopy.answerPlaceholder, text: binding(for: q), axis: .vertical)
                        .textFieldStyle(.plain)
                        // Opened by "Other answer": the keyboard comes up with it, and the card
                        // rises above the composer, so one tap is enough to type.
                        .focusOnReveal($typing, equals: q.id, revealed: others.focus == q.id, row: item.id)
                        .padding(12)
                        .background(Color.togoRaised, in: .rect(cornerRadius: 12, style: .continuous))
                        .disabled(locked)
                } else {
                    // The typed answer is one more choice, behind its own row, so a card of
                    // several questions is not a column of empty fields.
                    Button(ChatCopy.otherAnswer, systemImage: "character.cursor.ibeam") {
                        // motion: cause → effect — the field opens where the row was tapped.
                        withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { others.reveal(q.id) }
                    }
                        .buttonStyle(PlainAnswerStyle())
                        .font(.subheadline)
                        .disabled(locked)
                }
            }
        }
    }

    private func optionRow(_ option: HarnessChoice, in q: HarnessQuestion) -> some View {
        let chosen = model.isSelected(option.id, in: q)
        let source = answersOnTap && others.isEmpty && tapped.isSource(option.id)
        return Button { choose(option.id, in: q) } label: {
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
        .hoverEffect(.highlight)
        .answerSource(AnswerMatch.id(item.id), in: answerSpace, active: source)
        .accessibilityAddTraits(chosen ? .isSelected : [])
    }

    private func binding(for q: HarnessQuestion) -> Binding<String> {
        Binding { model.text(for: q) } set: { model.setText($0, for: q) }
    }

    private func choose(_ option: String, in q: HarnessQuestion) {
        // motion: cause → effect — the tapped choice's circle fills in place.
        withAnimation(Motion.animation(.respond, reduce: reduceMotion)) { model.toggle(option, in: q) }
        if answersOnTap, others.isEmpty, model.isSelected(option, in: q) { submit(from: option) }
    }

    private func submit(from source: String) {
        tapped.record(source)
        let shown = model.confirm
        let send = self.send
        Task { await model.submit(for: shown) { args in await send(args) } }
    }
}
