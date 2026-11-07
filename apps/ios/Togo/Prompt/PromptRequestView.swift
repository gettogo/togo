import SwiftUI

/// A request in the bar: whose it is, the question, the command and the answers. Replying keeps
/// the question and swaps the command and answers for a reply line.
struct PromptRequestView: View {
    @Environment(\.loopsPaused) private var loopsPaused
    @Environment(\.dynamicTypeSize) private var typeSize
    @Bindable var model: PromptBarModel
    let request: PromptRequest
    let position: Int
    let count: Int
    let replying: Bool
    @FocusState private var replyFocused: Bool
    @State private var showingAll = false

    var body: some View {
        VStack(alignment: .leading, spacing: model.compact ? 6 : 10) {
            contextLine
            Text(request.question)
                .font(.headline)
                .foregroundStyle(Color.togoSnow)
                // At accessibility sizes the question keeps to three lines; the rest is in the sheet.
                // Over a terminal that has the keyboard it keeps to two, so the terminal keeps the height.
                .lineLimit(model.compact ? 2 : large ? 3 : nil)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityAddTraits(.isHeader)
            if replying {
                if request.state == .withdrawn { stateLine(PromptCopy.withdrawn, failure: false) }
                replyLine
            } else {
                if let command = request.command, !command.isEmpty {
                    PromptCommandWell(
                        command: command, types: model.introPending(.command, for: request.id),
                        lineLimit: large || model.compact ? min(2, PromptCommandWell.lineLimit) : PromptCommandWell.lineLimit,
                        onType: { _ = model.takeIntro(.command, for: request.id) },
                        onShowAll: { showingAll = true }
                    )
                } else if large {
                    Button(PromptCopy.showAll) { showingAll = true }
                        .font(.footnote.weight(.semibold))
                        .foregroundStyle(Color.togoSnow)
                        .frame(minHeight: 44)
                }
                outcome
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, model.compact ? 10 : 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .sheet(isPresented: $showingAll) {
            PromptCommandSheet(question: request.question, command: request.command ?? "")
                .togoSheet(.form)
        }
        .haptic(.approve, trigger: request.state) { _, new in new == .done(.approved) }
        .haptic(.deny, trigger: request.state) { _, new in new == .done(.denied) }
        .haptic(.error, trigger: request.state) { _, new in if case .failed = new { true } else { false } }
    }

    private var large: Bool { typeSize.isAccessibilitySize }

    /// The answers, or where the answer stands once one is on its way or back.
    @ViewBuilder private var outcome: some View {
        switch request.state {
        case .open:
            answers
        case .failed(let message):
            stateLine(message, failure: true)
            answers
        case .sending:
            answers.disabled(true)
        case .done(let outcome):
            stateLine(outcome.label, failure: false)
        case .withdrawn:
            stateLine(PromptCopy.withdrawn, failure: false)
        }
    }

    private func stateLine(_ text: String, failure: Bool) -> some View {
        Text(text)
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(failure ? Color.red : Color.togoMute)
            .frame(maxWidth: .infinity, minHeight: 32, alignment: .leading)
            .accessibilityAddTraits(.updatesFrequently)
    }

    private var contextLine: some View {
        let queue = PromptBarModel.queueText(position: position, count: count)
        return HStack(spacing: 8) {
            PromptBeacon(
                lights: model.introPending(.beacon, for: request.id),
                onLight: { _ = model.takeIntro(.beacon, for: request.id) }
            )
            Text(verbatim: request.bib)
                .font(.system(.footnote, design: .monospaced))
                .monospacedDigit()
                .foregroundStyle(Color.togoSnow)
                .fixedSize()
            Text(PromptCopy.context(name: request.sessionName, agent: request.agentName, machine: request.machineName))
                .font(.footnote)
                .foregroundStyle(Color.togoMute)
                .lineLimit(1)
                .frame(maxWidth: .infinity, alignment: .leading)
            if let queue {
                Text(queue)
                    .font(.system(.caption2, design: .monospaced))
                    .monospacedDigit()
                    .foregroundStyle(Color.togoMute)
                    .fixedSize()
            }
        }
        .contentShape(.rect)
        .onTapGesture { if !request.sessionID.isEmpty { model.onOpen(request) } }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(PromptCopy.contextLabel(
            bib: request.bib, name: request.sessionName, agent: request.agentName,
            machine: request.machineName, queue: queue
        ))
        .accessibilityAddTraits(request.sessionID.isEmpty ? [] : .isButton)
        .accessibilityAction { if !request.sessionID.isEmpty { model.onOpen(request) } }
    }

    /// Yes, No and Reply in a row, or stacked when the text is too large for one.
    private var answers: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 8) { answerButtons }
            VStack(alignment: .leading, spacing: 8) { answerButtons }
        }
    }

    /// Each answer names this request, so one that lands after the bar moved on is dropped.
    @ViewBuilder private var answerButtons: some View {
        let hint = request.command.map { PromptCopy.answerHint(question: request.question, command: $0) } ?? request.question
        if request.asksYesOrNo {
            Button(PromptCopy.yes) { model.answer(.yes, to: request.id) }
                .buttonStyle(.promptCapsule(.beacon))
                .accessibilityHint(hint)
            Button(PromptCopy.no) { model.answer(.no, to: request.id) }
                .buttonStyle(.promptCapsule(.plain))
                .accessibilityHint(hint)
        }
        if !request.more.isEmpty {
            Menu {
                ForEach(request.more) { option in
                    Button(option.label) { model.answer(.option(option.id), to: request.id) }
                }
            } label: {
                Text(PromptCopy.more)
            }
            .buttonStyle(.promptCapsule(.plain))
            .accessibilityLabel(PromptCopy.moreOptions)
        }
        if request.allowsReply {
            Button(PromptCopy.reply) { model.beginReply() }
                .buttonStyle(.promptCapsule(.plain))
                .accessibilityHint(PromptCopy.replyHint)
        }
    }

    private var replyLine: some View {
        let empty = model.replyDraft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        let placeholder = PromptCopy.replyTo(request.sessionName)
        return HStack(spacing: 10) {
            PromptChevron()
            TextField(text: $model.replyDraft, prompt: Text(placeholder).foregroundStyle(Color.togoMute)) {
                Text(placeholder)
            }
            .font(.body)
            .foregroundStyle(Color.togoSnow)
            .focused($replyFocused)
            .submitLabel(.send)
            .onSubmit { model.sendReply() }
            .disabled(request.state == .withdrawn && empty)
            .onKeyPress(.escape) {
                model.cancelReply()
                return .handled
            }
            if request.state == .withdrawn {
                Button(PromptCopy.dismiss) { model.cancelReply() }
                    .buttonStyle(.promptCapsule(.plain))
            } else {
                Button(PromptCopy.send) { model.sendReply() }
                    .buttonStyle(.promptCapsule(.snow))
                    .disabled(empty)
            }
        }
        // A still snapshot leaves the field unfocused, so no caret blinks in it.
        .onAppear { replyFocused = !loopsPaused }
        .onChange(of: replyFocused, initial: true) { _, focused in model.barTextFocused = focused }
        .onDisappear { model.barTextFocused = false }
    }
}

/// The `>` that leads every line the user types into.
struct PromptChevron: View {
    var body: some View {
        Text(verbatim: ">")
            .font(.system(.body, design: .monospaced).weight(.semibold))
            .foregroundStyle(Color.togoMute)
            .accessibilityHidden(true)
    }
}

/// The command as it would follow a shell prompt: mono in a deepest well, with a muted `$`.
/// It types itself out the first time its request shows. Past four lines it stops and offers
/// the whole command in a sheet.
struct PromptCommandWell: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.loopsPaused) private var loopsPaused
    let command: String
    /// Whether this is the request's first showing, when the command types itself out.
    let types: Bool
    var lineLimit = PromptCommandWell.lineLimit
    var onType: () -> Void = {}
    var onShowAll: () -> Void = {}
    @State private var typed: Int?
    @State private var fullHeight: CGFloat = 0
    @State private var shownHeight: CGFloat = 0

    static let lineLimit = 4

    var body: some View {
        let animates = types && !reduceMotion && !loopsPaused
        let count = typed ?? (animates ? 0 : command.count)
        VStack(alignment: .leading, spacing: 4) {
            ZStack(alignment: .topLeading) {
                // The whole command holds the well's size, so the bar does not grow as it types.
                line(command)
                    .hidden()
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { shownHeight = $0 }
                line(String(command.prefix(count)))
            }
            .background(alignment: .topLeading) {
                line(command, limited: false)
                    .hidden()
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { fullHeight = $0 }
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color.togoDeepest, in: .rect(cornerRadius: 12, style: .continuous))
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(command)
            if fullHeight > shownHeight + 1 {
                Button(PromptCopy.showAll, action: onShowAll)
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(Color.togoSnow)
                    .frame(minHeight: 44)
                    .contentShape(.rect)
            }
        }
        .task(id: command) {
            guard animates, typed == nil else { return }
            onType()
            typed = 0
            var frame = 0
            while let shown = typed, shown < command.count {
                do {
                    try await Task.sleep(for: .milliseconds(16))
                } catch {
                    typed = command.count
                    return
                }
                // One and two characters on alternate frames: about 1.5 a frame.
                frame += 1
                typed = min(command.count, shown + (frame.isMultiple(of: 2) ? 2 : 1))
            }
        }
    }

    private func line(_ text: String, limited: Bool = true) -> some View {
        Text(Self.attributed(text))
            .font(.system(.footnote, design: .monospaced))
            .lineLimit(limited ? lineLimit : nil)
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// `$ ` in mute, then the command in snow.
    static func attributed(_ command: String) -> AttributedString {
        var prompt = AttributedString("$ ")
        prompt.foregroundColor = Color.togoMute
        var body = AttributedString(command)
        body.foregroundColor = Color.togoSnow
        return prompt + body
    }
}

/// The whole request when it runs past what the bar shows: the question, then the command.
struct PromptCommandSheet: View {
    @Environment(\.dismiss) private var dismiss
    var question = ""
    let command: String

    var body: some View {
        NavigationStack {
            ScrollView {
                if !question.isEmpty {
                    Text(question)
                        .font(.headline)
                        .foregroundStyle(Color.togoSnow)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding([.horizontal, .top], Layout.margin)
                }
                if !command.isEmpty {
                    commandWell
                }
            }
            .background(Color.togoInk)
            .navigationTitle(PromptCopy.commandTitle)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button(PromptCopy.done, systemImage: "checkmark", role: .confirm) { dismiss() }
                }
            }
        }
    }

    private var commandWell: some View {
        Text(PromptCommandWell.attributed(command))
            .font(.system(.footnote, design: .monospaced))
            .textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(12)
            .background(Color.togoDeepest, in: .rect(cornerRadius: 12, style: .continuous))
            .padding(Layout.margin)
            .accessibilityLabel(command)
    }
}
