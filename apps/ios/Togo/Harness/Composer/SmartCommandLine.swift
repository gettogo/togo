import SwiftUI
import TogoKit

/// The composer: a text field that types into the session (or the harness API), with the
/// / and @ menus, the state chip (model · effort · mode), Stop in the send button's place while
/// the agent works, and how many prompts wait.
///
/// Hardware keys: Return sends (or takes the highlighted completion), Shift-Return breaks the
/// line, Up and Down move through the menu or the history, Tab takes a completion, Esc closes
/// the menu, the terminal peek or interrupts, Control-U empties a peeked command, Command-K
/// focuses the field.
///
/// Motion: what the bar shows besides the field (the menu, the queued count, notices, the
/// pictures) is mirrored in `shown`, so each change runs in one explicit transaction that moves
/// the glass and the chat's inset with it. The `/` menu unfolds upward from the composer's edge,
/// where `/` was typed; a picture's chip arrives by opacity as the strip makes room; status
/// lines (queued, notices) land at once. Leavers conceal first and the glass shrinks after. The
/// state chip is always there; its words morph in place. Stop takes the send button's slot.
struct SmartCommandLine: View {
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.loopsPaused) private var loopsPaused
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Bindable var model: CommandLineModel
    /// Where a screen's non-screen open targets go, such as a diff or a URL.
    var onOpen: (HarnessOpen) -> Void = { _ in }
    @FocusState private var focused: Bool
    @State private var shown: ComposerParts
    @State private var leaving: Set<ComposerParts.Part> = []

    init(model: CommandLineModel, onOpen: @escaping (HarnessOpen) -> Void = { _ in }) {
        self.model = model
        self.onOpen = onOpen
        _shown = State(initialValue: ComposerParts(model))
    }

    private func fades(_ part: ComposerParts.Part) -> Double { leaving.contains(part) ? 0 : 1 }

    /// Follows the model: arrivals and changes in one `resize` transaction; anything leaving
    /// conceals first, then the bar shrinks to what the model shows by then.
    private func follow(_ parts: ComposerParts) {
        let gone = shown.parts.subtracting(parts.parts)
        guard !gone.isEmpty else {
            // motion: preventing a jump — the glass grows for what arrives; the chat's inset moves with it.
            withAnimation(Motion.animation(.resize, reduce: reduceMotion)) {
                shown = parts
                leaving = []
            }
            return
        }
        // motion: preventing a jump — what leaves conceals first, then the glass shrinks.
        withAnimation(Motion.animation(.conceal, reduce: reduceMotion), completionCriteria: .logicallyComplete) {
            leaving.formUnion(gone)
        } completion: {
            // motion: preventing a jump — the glass closes the room the leaver had.
            withAnimation(Motion.animation(.resize, reduce: reduceMotion)) {
                shown = ComposerParts(model)
                leaving = []
            }
        }
    }

    static let radius: CGFloat = 28

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if shown.menu {
                CommandMenuPanel(model: model, screenTitle: screenTitle)
                    .opacity(fades(.menu))
                    // motion: origin — the menu opens upward from the composer's edge, where `/` was typed.
                    .transition(.asymmetric(insertion: reduceMotion ? .opacity : AnyTransition(UnfoldingUp()),
                                            removal: Motion.conceal(reduce: reduceMotion)))
            } else if shown.peek {
                TerminalPeekStrip(model: model)
                    .opacity(fades(.peek))
                    // motion: origin — the agent's menu opens upward from the composer, as the `/` menu does.
                    .transition(.asymmetric(insertion: reduceMotion ? .opacity : AnyTransition(UnfoldingUp()),
                                            removal: Motion.conceal(reduce: reduceMotion)))
            }
            bar
                // The field always shows: the menu gives up its height first when room is short,
                // such as a folded Duo with the keyboard up.
                .layoutPriority(1)
        }
        .onChange(of: ComposerParts(model)) { _, parts in follow(parts) }
        // The keyboard going (a swipe, a tap on the chat, Hide Keyboard) takes the menu with it.
        .onChange(of: focused) { _, isFocused in
            if !isFocused, model.menu != .none { model.dismissMenu() }
        }
        .haptic(.error, trigger: model.notice) { _, notice in notice != nil }
        .task(id: model.draft) { await model.draftChanged() }
        // The terminal peek reads the agent's screen while it runs, and lets go of the agent's
        // line when it stops for any reason (a turn starts, a picture is added).
        .task(id: model.peekActive) {
            guard model.peekActive, !loopsPaused else { return }
            while !Task.isCancelled {
                model.refreshPeek()
                try? await Task.sleep(for: .milliseconds(150))
            }
        }
        .onChange(of: model.peekActive) { model.syncPeek() }
        // The screen's class (from the descriptor, before it loads) picks its presentation.
        .harnessPresent(
            item: Binding(
                get: { model.presentedScreen.map(ScreenRequest.init) },
                set: { model.presentedScreen = $0?.id }
            ),
            as: AdaptivePresentation.content(forScreen: model.presentedScreen ?? "", in: model.store.descriptor),
            name: "composer.screen"
        ) { request in
            HarnessScreenSheet(store: model.store, screenID: request.id, onOpen: onOpen)
        }
        // The dialog a command or a session change asks with, while the palette is not up.
        .actionConfirmation(Binding(
            get: { model.paletteOpen ? nil : model.gate.asking },
            set: { model.gate.asking = $0 }
        ))
        // The palette or a sheet may still be closing; the focus is asked for until it holds.
        .onChange(of: model.focusRequests) {
            Task { await FocusRetry.land(isFocused: { focused }, focus: { focused = true }) }
        }
        // "With instructions…" from a sheet or mission control: the line comes here, focused.
        .onChange(of: model.store.draftRequest, initial: true) { _, request in
            guard let request else { return }
            model.store.draftRequest = nil
            model.compose(request.text)
        }
        .background { shortcuts }
    }

    private var bar: some View {
        VStack(alignment: .leading, spacing: 0) {
            // The prompts themselves show in the chat, each with Take back; here only how many.
            if shown.queued > 0 {
                Text(ComposerCopy.queuedCount(shown.queued))
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                    // The queued count is an event: it rolls, downward as prompts go.
                    .countChange(Double(shown.queued))
                    .padding(.horizontal, 16)
                    .padding(.top, 10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .opacity(fades(.queued))
                    // motion: state change — status: the line is simply there (a cut).
                    .transition(.identity)
            }
            if let notice = shown.notice {
                ComposerNoticeLine(text: notice, action: model.noticeAction) { action in
                    switch action {
                    case .openTerminal: model.store.layer = .terminal
                    case .retry: Task { await model.submit() }
                    case .sendAsText: Task { await model.submit(asText: true) }
                    }
                    model.clearNotice()
                }
                .opacity(fades(.notice))
                // motion: state change — errors land at once, at the composer that failed (a cut).
                .transition(.identity)
                // How long the notice stays is the model's hold; its leaving is choreographed.
                .task(id: notice) {
                    guard !loopsPaused else { return }
                    try? await Task.sleep(for: .seconds(model.noticeAction == nil ? 5 : 8))
                    model.clearNotice()
                }
            }
            if let undo = shown.undo {
                UndoLine(title: undo.title) {
                    model.store.undo = nil
                    Task { await undo.run() }
                }
                .opacity(fades(.undo))
                // motion: state change — Undo is simply there after the restart (a cut).
                .transition(.identity)
                .task(id: undo.id) {
                    guard !loopsPaused else { return }
                    try? await Task.sleep(for: .seconds(10))
                    if model.store.undo?.id == undo.id { model.store.undo = nil }
                }
            }
            if let notice = shown.attachNotice {
                Text(notice)
                    .font(.footnote)
                    .foregroundStyle(Color.togoMute)
                    .padding(.horizontal, 16)
                    .padding(.top, 10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .opacity(fades(.attachNotice))
                    // motion: state change — a note about the pictures lands at once (a cut).
                    .transition(.identity)
                    .task(id: notice) {
                        guard !loopsPaused else { return }
                        try? await Task.sleep(for: .seconds(5))
                        model.attachments.clearNotice()
                    }
            }
            if shown.pictures {
                AttachmentStrip(attachments: model.attachments)
                    .opacity(fades(.pictures))
                    // motion: cause → effect — the picked picture's strip arrives by opacity as the glass makes room.
                    .transition(Motion.arrive(reduce: reduceMotion))
            }
            field
            accessories
        }
        // The glass is the container: it resizes in the same transaction as what it holds.
        .clipShape(.rect(cornerRadius: Self.radius, style: .continuous))
        .glassEffect(glass, in: .rect(cornerRadius: Self.radius, style: .continuous))
    }

    private var field: some View {
        HStack(alignment: .bottom, spacing: 10) {
            PromptChevron()
                .padding(.bottom, 12)
            TextField(
                // What the keyboard writes goes through `typed`, which drops a late dictation
                // result that would put the words just sent back in the field.
                text: Binding(get: { model.draft }, set: { model.typed($0) }),
                prompt: Text(placeholder).foregroundStyle(Color.togoMute),
                axis: .vertical
            ) { Text(placeholder) }
            .font(.mono(.body))
            .foregroundStyle(Color.togoSnow)
            .lineLimit(1...6)
            // A prompt is natural language; a "/" command or an "@" file is typed exactly.
            .textInputAutocapitalization(assists ? .sentences : .never)
            .autocorrectionDisabled(!assists)
            // Prompts hold commands, flags and paths an agent may run: `--` stays two hyphens
            // and quotes stay straight, as the terminal takes them.
            .straightPunctuation($model.draft)
            .focused($focused)
            .accessibilityIdentifier("composer.field")
            .padding(.vertical, 12)
            .onKeyPress(phases: .down, action: key)
            sendButton
                .padding(.bottom, 4)
        }
        .padding(.leading, 16)
        .padding(.trailing, 6)
        .frame(minHeight: 50)
    }

    /// Stop takes the send button's slot while the agent works and nothing waits in the field;
    /// a draft turns it back into Queue. One button, its symbol replaced in place.
    private var stops: Bool { model.canInterrupt && CommandLineLogic.outgoing(model.draft) == nil && model.attachments.isEmpty }

    private var sendButton: some View {
        Button {
            if stops {
                Task { await model.interrupt() }
            } else {
                // The keyboard's dictation stops with the Send, not after it.
                KeyboardDictation.end()
                Task { await model.submit() }
            }
        } label: {
            Image(systemName: stops ? "stop.fill" : (model.working ? "text.append" : "arrow.up"))
                // motion: state change — the send button becomes Stop in place: same slot, new job.
                .contentTransition(reduceMotion ? .identity : .symbolEffect(.replace))
                .font(.body.weight(.bold))
                .foregroundStyle(Color.togoInk)
                .frame(width: 34, height: 34)
                .background(Color.togoSnow, in: .circle)
                .frame(width: 44, height: 44)
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .hoverEffect(.highlight)
        .disabled(!stops && !model.canSend)
        .opacity(stops || model.canSend ? 1 : 0.35)
        .accessibilityLabel(stops ? ComposerCopy.interrupt : (model.working ? ComposerCopy.queue : ComposerCopy.send))
        // motion: state change — the button dims or lights as sending becomes possible.
        .motion(.respond, value: stops || model.canSend)
    }

    /// The chips in one row, or the state chip on a row of its own above the others when its
    /// words and the text size leave no room, so the model, effort and mode are never cut short.
    private var accessories: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 6) {
                if model.showsStateChip { SessionStateChip(model: model) }
                tools
            }
            VStack(alignment: .leading, spacing: 0) {
                if model.showsStateChip { SessionStateChip(model: model) }
                HStack(spacing: 6) { tools }
            }
        }
        .padding(.horizontal, 10)
        .padding(.bottom, 8)
    }

    @ViewBuilder private var tools: some View {
        if model.routes.has(HarnessCapability.commands) {
            Button(ComposerCopy.commands, systemImage: "slash.circle") { model.paletteOpen = true }
                .labelStyle(.iconOnly)
                .buttonStyle(ComposerChipStyle())
                // Every command is a large list to search: a sheet sized by the window's room.
                .harnessPresent(isPresented: $model.paletteOpen, as: .browse, anchored: true, name: "composer.palette", onDismiss: model.paletteClosed) {
                    CommandPaletteView(model: model, screenTitle: screenTitle)
                }
        }
        if model.attachments.available {
            AttachMenu(attachments: model.attachments)
        }
        if model.routes.has(HarnessCapability.files) {
            Button(ComposerCopy.mention, systemImage: "at") {
                model.draft += model.draft.isEmpty || model.draft.last?.isWhitespace == true ? "@" : " @"
                focused = true
            }
            .labelStyle(.iconOnly)
            .buttonStyle(ComposerChipStyle())
        }
        Spacer(minLength: 0)
    }

    /// The keyboard helps with natural language only.
    private var assists: Bool { CommandLineLogic.keyboardAssists(model.draft) }

    private var placeholder: String {
        ComposerCopy.message(model.store.agentName)
            .trimmingCharacters(in: .whitespaces)
    }

    private func screenTitle(_ id: String) -> String {
        model.store.descriptor?.screens.first { $0.id == id }?.title ?? id
    }

    private var glass: Glass {
        colorScheme == .dark
            ? .regular.tint(Color.togoGrouped.opacity(0.5))
            : .regular.tint(Color.togoRaised.opacity(0.6))
    }

    private func key(_ press: KeyPress) -> KeyPress.Result {
        let menuOpen = model.menu != .none
        // Control-U empties the field and, while the peek runs, the agent's line with it.
        let controlU = (press.key == KeyEquivalent("u") && press.modifiers.contains(.control)) || press.characters == "\u{15}"
        if controlU, model.peekActive {
            model.draft = ""
            return .handled
        }
        switch press.key {
        case .return:
            if press.modifiers.contains(.shift) { return .ignored }
            KeyboardDictation.end()
            Task { await model.returnPressed() }
            return .handled
        case .upArrow:
            if menuOpen { model.moveHighlight(-1); return .handled }
            if model.draft.isEmpty || !model.draft.contains("\n") {
                let before = model.draft
                model.recall(older: true)
                return before == model.draft ? .ignored : .handled
            }
            return .ignored
        case .downArrow:
            if menuOpen { model.moveHighlight(1); return .handled }
            let before = model.draft
            model.recall(older: false)
            return before == model.draft ? .ignored : .handled
        case .tab:
            // Shift+Tab steps the mode, as at the desk; focus stays in the field.
            if press.modifiers.contains(.shift), !menuOpen, model.nextMode != nil {
                Task { await model.cycleMode() }
                return .handled
            }
            guard menuOpen, let item = model.highlighted else { return .ignored }
            model.choose(item)
            return .handled
        case .escape:
            if menuOpen { model.closeMenu(); return .handled }
            if model.peekActive { model.cancelPeek(); return .handled }
            if model.canInterrupt { Task { await model.interrupt() }; return .handled }
            return .ignored
        default:
            return .ignored
        }
    }

    /// Command-K focuses the field from anywhere in the window.
    private var shortcuts: some View {
        Button(ComposerCopy.focusField) { focused = true }
            .keyboardShortcut("k", modifiers: .command)
            .frame(width: 0, height: 0)
            .opacity(0)
            .accessibilityHidden(true)
    }
}

#if DEBUG
@MainActor private func previewModel(working: Bool = false, queued: [HarnessItem] = [], draft: String = "") -> CommandLineModel {
    let services = PreviewHarnessServices()
    let model = CommandLineModel(
        store: .preview(services), memory: CommandMemory(defaults: UserDefaults(suiteName: "composer-preview")!),
        routes: .preview(services, working: working, queued: queued)
    )
    model.draft = draft
    return model
}

#Preview("Composer, idle") {
    VStack { Spacer(); SmartCommandLine(model: previewModel()).padding(12) }
        .background(Color.togoInk)
}

#Preview("Composer, working with a queue") {
    VStack { Spacer(); SmartCommandLine(model: previewModel(working: true, queued: [HarnessSamples.queuedItem])).padding(12) }
        .background(Color.togoInk)
}

#Preview("Composer, / menu") {
    VStack { Spacer(); SmartCommandLine(model: previewModel(draft: "/")).padding(12) }
        .background(Color.togoInk)
}

#Preview("Composer, @ menu, Duo folded", traits: .fixedLayout(width: 466, height: 678)) {
    VStack { Spacer(); SmartCommandLine(model: previewModel(draft: "look at @pa")).padding(12) }
        .background(Color.togoInk)
}

#Preview("Composer, AX3") {
    VStack { Spacer(); SmartCommandLine(model: previewModel(working: true)).padding(12) }
        .background(Color.togoInk)
        .environment(\.dynamicTypeSize, .accessibility3)
}
#endif
