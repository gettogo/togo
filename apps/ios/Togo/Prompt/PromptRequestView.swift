import SwiftUI

/// A request in the bar: whose it is, the question, the command and the answers. Replying keeps
/// the question and swaps the command and answers for a reply line.
struct PromptRequestView: View {
    @Environment(\.loopsPaused) private var loopsPaused
    @Environment(\.dynamicTypeSize) private var typeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Bindable var model: PromptBarModel
    let request: PromptRequest
    let position: Int
    let count: Int
    let replying: Bool
    @FocusState private var replyFocused: Bool
    @State private var showingAll = false
    @State private var showingMore = false
    /// Where the answer stands on screen. It follows `request.state` (the network) through the
    /// swap choreography: the answers conceal, then the bar resizes as the outcome line
    /// reveals (motion spec §2.3).
    @State private var shownState: Mirrored<PromptRequestState>
    @Namespace private var answerSpace
    /// Yes or No as the person tapped it here, so the outcome line comes out of that button.
    @State private var tapped = AnswerTap<PromptAnswer>()
    @State private var settle: AnswerSettle = .collapse

    init(model: PromptBarModel, request: PromptRequest, position: Int, count: Int, replying: Bool) {
        self.model = model
        self.request = request
        self.position = position
        self.count = count
        self.replying = replying
        _shownState = State(initialValue: Mirrored(request.state))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: model.compact ? 6 : 10) {
            contextLine
            Text(request.question)
                .font(.headline)
                .foregroundStyle(Color.togoSnow)
                // trunc-ok: at accessibility sizes the question keeps to three lines; Show all opens the rest in a sheet.
                // Over a terminal that has the keyboard it keeps to two, so the terminal keeps the height.
                .lineLimit(model.compact ? 2 : large ? 3 : nil)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityAddTraits(.isHeader)
            if replying {
                if shownState.value == .withdrawn {
                    stateLine(PromptCopy.withdrawn, failure: false)
                        .settling(shownState)
                }
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
                Group { outcome }
                    .settling(shownState)
            }
        }
        .padding(.horizontal, 16)
        .padding(.vertical, model.compact ? 10 : 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .sheet(isPresented: $showingAll) {
            PromptCommandSheet(question: request.question, command: request.command ?? "")
                .togoSheet(.form)
        }
        // Extra answers in a list with their full words; a menu would clip them.
        .sheet(isPresented: $showingMore) {
            AnswerOptionsSheet(
                question: request.question,
                options: request.more.map { AnswerOption(id: $0.id, label: $0.label) }
            ) { model.answer(.option($0.id), to: request.id) }
        }
        .onChange(of: request.state) { _, new in follow(new) }
        .haptic(.approve, trigger: request.state) { _, new in new == .done(.approved) }
        .haptic(.deny, trigger: request.state) { _, new in new == .done(.denied) }
        .haptic(.error, trigger: request.state) { _, new in if case .failed = new { true } else { false } }
    }

    private var large: Bool { typeSize.isAccessibilitySize }

    /// The person's own Yes or No becomes the outcome line in one transaction; anything else
    /// conceals the answers first, then the bar resizes to the line.
    private func follow(_ new: PromptRequestState) {
        if case .done(let outcome) = new {
            settle = tapped.settle(answeredHere: outcome == .approved || outcome == .denied)
        } else {
            settle = .collapse
        }
        guard settle == .travel else {
            Motion.settle(
                $shownState, to: new, small: PromptOutcomeMotion.isSmall(from: shownState.value, to: new),
                reduce: reduceMotion
            )
            return
        }
        shownState.generation += 1
        // motion: cause → effect — the tapped Yes or No travels into the outcome line as the answers give way around it.
        withAnimation(Motion.animation(.resize, reduce: reduceMotion)) {
            shownState.value = new
            shownState.shown = true
        }
    }

    /// The answers, or where the answer stands once one is on its way or back.
    @ViewBuilder private var outcome: some View {
        switch shownState.value {
        case .open:
            answers
        case .failed(let message):
            stateLine(message, failure: true)
            answers
        case .sending:
            answers.disabled(true)
        case .done(let outcome):
            stateLine(outcome.label, failure: false)
                .answerSource(AnswerMatch.id(request.id), in: answerSpace, active: settle == .travel)
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
                // trunc-ok: one line only over a terminal that has the keyboard; tapping opens the
                // session and VoiceOver reads the context whole. Elsewhere it wraps.
                .lineLimit(model.compact ? 1 : nil)
                .frame(maxWidth: .infinity, alignment: .leading)
            if let queue {
                Text(queue)
                    .font(.system(.caption2, design: .monospaced))
                    .foregroundStyle(Color.togoMute)
                    .fixedSize()
                    .countChange(Double(position))
            }
        }
        // motion: state change — the queue count appears in place as a second request waits; the bar's height does not change.
        .motion(.respond, value: queue)
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
            Button(PromptCopy.yes) { answer(.yes) }
                .buttonStyle(.promptCapsule(.beacon))
                .accessibilityHint(hint)
                .answerSource(AnswerMatch.id(request.id), in: answerSpace, active: tapped.isSource(.yes))
            Button(PromptCopy.no) { answer(.no) }
                .buttonStyle(.promptCapsule(.plain))
                .accessibilityHint(hint)
                .answerSource(AnswerMatch.id(request.id), in: answerSpace, active: tapped.isSource(.no))
        }
        if !request.more.isEmpty {
            Button(PromptCopy.more) { showingMore = true }
                .buttonStyle(.promptCapsule(.plain))
                .accessibilityLabel(PromptCopy.moreOptions)
        }
        if request.allowsReply {
            Button(PromptCopy.reply) { model.beginReply() }
                .buttonStyle(.promptCapsule(.plain))
                .accessibilityHint(PromptCopy.replyHint)
        }
    }

    private func answer(_ answer: PromptAnswer) {
        tapped.record(answer)
        model.answer(answer, to: request.id)
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

/// Which outcome changes are a swap in place rather than a resize: an answer on its way only
/// dims the answers, so open ↔ sending is one `respond` transaction (spec §2.3, rule 3).
enum PromptOutcomeMotion {
    static func isSmall(from old: PromptRequestState, to new: PromptRequestState) -> Bool {
        switch (old, new) {
        case (.open, .sending), (.sending, .open): true
        default: false
        }
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
                // Known from the first layout, so it is there from the first frame; a later
                // change (rotation, text size) lands with that change.
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
