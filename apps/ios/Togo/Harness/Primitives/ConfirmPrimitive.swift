import SwiftUI
import TogoKit

/// A question for the user, or a plain "are you sure". One or more questions, each single or
/// multiple choice, with typed words when the harness takes them. It answers the way the
/// prompt bar does: capsules, a `>` reply line, Send only once the question has been on screen
/// long enough to read. `needsYou` marks a question the agent is waiting on: the beacon
/// lights and Send takes the approve fill.
struct ConfirmPrimitive: View {
    @Environment(\.loopsPaused) private var loopsPaused
    let confirm: HarnessScreen.Confirm
    let context: PrimitiveContext
    var needsYou = false
    /// Drawn inside another scroll view: no scroll of its own.
    var embedded = false
    @State private var model: ConfirmModel

    init(confirm: HarnessScreen.Confirm, context: PrimitiveContext, needsYou: Bool = false, embedded: Bool = false) {
        self.confirm = confirm
        self.context = context
        self.needsYou = needsYou
        self.embedded = embedded
        _model = State(initialValue: ConfirmModel(confirm: confirm))
    }

    var body: some View {
        Group {
            if embedded {
                content
            } else {
                ScrollView { content.padding(.horizontal, Layout.margin).padding(.vertical, 16) }
                    .background(Color.togoInk)
                    .scrollDismissesKeyboard(.interactively)
            }
        }
        .onChange(of: confirm) { _, new in model.update(new) }
        .onAppear { if loopsPaused { model.armNow() } }
        .haptic(needsYou ? .approve : .done, trigger: model.phase) { _, new in
            if case .answered = new { true } else { false }
        }
        .haptic(.error, trigger: model.phase) { _, new in
            if case .failed = new { true } else { false }
        }
        .motion(.dropIn, value: model.phase)
    }

    @ViewBuilder private var content: some View {
        if case let .answered(message) = model.phase {
            answered(message)
        } else {
            VStack(alignment: .leading, spacing: 18) {
                if needsYou {
                    HStack(spacing: 8) {
                        PromptBeacon(lights: false)
                        StatusWord(status: .needsYou, font: .footnote)
                    }
                    .accessibilityElement(children: .combine)
                }
                if let message = confirm.message, !message.isEmpty {
                    Text(PrimitiveMarkdownBlock.inline(message))
                        .font(confirm.questions.isEmpty ? .title3.weight(.semibold) : .body)
                        .foregroundStyle(Color.togoSnow)
                        .accessibilityAddTraits(confirm.questions.isEmpty ? .isHeader : [])
                }
                ForEach(Array(confirm.questions.enumerated()), id: \.element.id) { index, question in
                    ConfirmQuestionView(
                        question: question, index: index, count: confirm.questions.count, model: model
                    )
                }
                status
                submit
            }
        }
    }

    @ViewBuilder private var status: some View {
        if model.changed {
            Text(PrimitiveCopy.changedWhileOpen)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(Color.togoSnow)
                .accessibilityAddTraits(.updatesFrequently)
        }
        if case let .failed(message) = model.phase {
            Text(message)
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(PrimitiveTint.fail)
                .accessibilityAddTraits(.updatesFrequently)
        }
    }

    @ViewBuilder private var submit: some View {
        let title = confirm.questions.isEmpty ? PrimitiveCopy.confirm : PrimitiveCopy.submit
        let shown = confirm
        let action = confirm.submit
        let run = context.run
        let send: () -> Void = { _ = Task { await model.submit(for: shown) { await run(action, nil, $0) } } }
        HStack(spacing: 10) {
            if model.phase == .sending { ProgressView().controlSize(.small) }
            if confirm.destructive {
                Button(title, role: .destructive) { send() }
                    .buttonStyle(PrimitiveCapsuleStyle(role: .destructive, large: true))
                    .disabled(!model.canSubmit)
                    .keyboardShortcut(.defaultAction)
            } else if needsYou {
                ApproveButton(title) { send() }
                    .disabled(!model.canSubmit)
                    .keyboardShortcut(.defaultAction)
            } else {
                Button(title) { send() }
                    .buttonStyle(PrimitiveCapsuleStyle(role: .primary, large: true))
                    .disabled(!model.canSubmit)
                    .keyboardShortcut(.defaultAction)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func answered(_ message: String?) -> some View {
        VStack(spacing: 10) {
            Image(systemName: "checkmark")
                .font(.title2.weight(.semibold))
                .foregroundStyle(PrimitiveTint.ok)
                .frame(width: 48, height: 48)
                .background(PrimitiveTint.ok.opacity(0.14), in: .circle)
                .accessibilityHidden(true)
            Text(PrimitiveCopy.answered).font(.headline).foregroundStyle(Color.togoSnow)
            Text(verbatim: message ?? PrimitiveCopy.carriesOn)
                .font(.subheadline)
                .foregroundStyle(Color.togoMute)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, 32)
        .accessibilityElement(children: .combine)
    }
}

/// One question: its header, the prompt, the options as capsules and a reply line.
private struct ConfirmQuestionView: View {
    let question: HarnessQuestion
    let index: Int
    let count: Int
    let model: ConfirmModel
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 8) {
                if let header = question.header, !header.isEmpty {
                    PrimitiveChip(text: header, mono: true, tone: .strong)
                }
                if count > 1 {
                    Text(PrimitiveCopy.question(index + 1, of: count))
                        .font(.caption)
                        .foregroundStyle(Color.togoMute)
                }
            }
            Text(PrimitiveMarkdownBlock.inline(question.prompt))
                .font(.headline)
                .foregroundStyle(Color.togoSnow)
                .accessibilityAddTraits(.isHeader)
            if !question.options.isEmpty {
                Text(question.multi ? PrimitiveCopy.pickAll : PrimitiveCopy.pickOne)
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                VStack(alignment: .leading, spacing: 6) {
                    ForEach(question.options) { optionRow($0) }
                }
            }
            if question.allowText {
                HStack(spacing: 10) {
                    PromptChevron()
                    TextField(
                        text: Binding(get: { model.text(for: question) }, set: { model.setText($0, for: question) }),
                        prompt: Text(PrimitiveCopy.yourAnswer).foregroundStyle(Color.togoMute),
                        axis: .vertical
                    ) { Text(PrimitiveCopy.yourAnswer) }
                    .font(.mono(.body))
                    .foregroundStyle(Color.togoSnow)
                    .lineLimit(1...5)
                    .focused($focused)
                }
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .frame(minHeight: 44)
                .background(Color.togoDeepest, in: .rect(cornerRadius: 14, style: .continuous))
            }
        }
        .accessibilityElement(children: .contain)
    }

    private func optionRow(_ option: HarnessChoice) -> some View {
        let on = model.isSelected(option.id, in: question)
        return Button { model.toggle(option.id, in: question) } label: {
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Image(systemName: question.multi ? (on ? "checkmark.square.fill" : "square") : (on ? "largecircle.fill.circle" : "circle"))
                    .foregroundStyle(on ? Color.togoInk : Color.togoMute)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 2) {
                    Text(option.label).font(.body.weight(.semibold))
                    if let detail = option.detail, !detail.isEmpty {
                        Text(detail).font(.footnote).opacity(0.75)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .foregroundStyle(on ? Color.togoInk : Color.togoSnow)
            .padding(.horizontal, 14)
            .padding(.vertical, 10)
            .frame(minHeight: 44)
            .background(on ? Color.togoSnow : Color.togoRaised, in: .rect(cornerRadius: 16, style: .continuous))
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .motion(.confirm, value: on)
        .accessibilityLabel(option.label)
        .accessibilityHint(option.detail ?? "")
        .accessibilityAddTraits(on ? [.isButton, .isSelected] : .isButton)
    }
}
