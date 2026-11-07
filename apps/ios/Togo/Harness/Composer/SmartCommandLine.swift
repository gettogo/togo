import SwiftUI
import TogoKit

/// The composer: a text field that types into the session (or the harness API), with the
/// / and @ menus, the mode chip, Interrupt while the agent works, and how many prompts wait.
///
/// Hardware keys: Return sends (or takes the highlighted completion), Shift-Return breaks the
/// line, Up and Down move through the menu or the history, Tab takes a completion, Esc closes
/// the menu or interrupts, Command-K focuses the field.
///
/// Motion: what the bar shows besides the field (the menu, the queued count, notices, the
/// pictures, the mode chip, Stop) is mirrored in `shown`, so each change runs in one explicit
/// transaction that moves the glass and the chat's inset with it. Arrivals reveal as the
/// glass grows; leavers conceal first and the glass shrinks after (spec §2.1/§2.2).
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
            withAnimation(Motion.animation(.resize, reduce: reduceMotion)) {
                shown = parts
                leaving = []
            }
            return
        }
        withAnimation(Motion.animation(.conceal, reduce: reduceMotion), completionCriteria: .logicallyComplete) {
            leaving.formUnion(gone)
        } completion: {
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
                    .transition(Motion.revealConceal(reduce: reduceMotion))
            }
            bar
        }
        .onChange(of: ComposerParts(model)) { _, parts in follow(parts) }
        .haptic(.error, trigger: model.notice) { _, notice in notice != nil }
        .task(id: model.draft) { await model.draftChanged() }
        .sheet(item: Binding(
            get: { model.presentedScreen.map(ScreenRequest.init) },
            set: { model.presentedScreen = $0?.id }
        )) { request in
            HarnessScreenSheet(store: model.store, screenID: request.id, onOpen: onOpen)
        }
        .sheet(isPresented: $model.paletteOpen) {
            CommandPaletteView(model: model, screenTitle: screenTitle)
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
                    .rollingNumber(Double(shown.queued))
                    .padding(.horizontal, 16)
                    .padding(.top, 10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .opacity(fades(.queued))
                    .transition(Motion.revealConceal(reduce: reduceMotion))
            }
            if let notice = shown.notice {
                Text(notice)
                    .font(.footnote.weight(.semibold))
                    .foregroundStyle(PrimitiveTint.fail)
                    .padding(.horizontal, 16)
                    .padding(.top, 10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .accessibilityAddTraits(.updatesFrequently)
                    .opacity(fades(.notice))
                    .transition(Motion.revealConceal(reduce: reduceMotion))
                    // How long the notice stays is the model's hold; its leaving is choreographed.
                    .task(id: notice) {
                        guard !loopsPaused else { return }
                        try? await Task.sleep(for: .seconds(5))
                        model.clearNotice()
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
                    .transition(Motion.revealConceal(reduce: reduceMotion))
                    .task(id: notice) {
                        guard !loopsPaused else { return }
                        try? await Task.sleep(for: .seconds(5))
                        model.attachments.clearNotice()
                    }
            }
            if shown.pictures {
                AttachmentStrip(attachments: model.attachments)
                    .opacity(fades(.pictures))
                    .transition(Motion.revealConceal(reduce: reduceMotion))
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
                text: $model.draft,
                prompt: Text(placeholder).foregroundStyle(Color.togoMute),
                axis: .vertical
            ) { Text(placeholder) }
            .font(.mono(.body))
            .foregroundStyle(Color.togoSnow)
            .lineLimit(1...6)
            // A prompt is natural language; a "/" command or an "@" file is typed exactly.
            .textInputAutocapitalization(assists ? .sentences : .never)
            .autocorrectionDisabled(!assists)
            .focused($focused)
            .padding(.vertical, 12)
            .onKeyPress(phases: .down, action: key)
            sendButton
                .padding(.bottom, 4)
        }
        .padding(.leading, 16)
        .padding(.trailing, 6)
        .frame(minHeight: 50)
    }

    private var sendButton: some View {
        Button {
            Task { await model.submit() }
        } label: {
            Image(systemName: model.working ? "text.append" : "arrow.up")
                .contentTransition(reduceMotion ? .identity : .symbolEffect(.replace))
                .font(.body.weight(.bold))
                .foregroundStyle(Color.togoInk)
                .frame(width: 34, height: 34)
                .background(Color.togoSnow, in: .circle)
                .frame(width: 44, height: 44)
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .disabled(!model.canSend)
        .opacity(model.canSend ? 1 : 0.35)
        .accessibilityLabel(model.working ? ComposerCopy.queue : ComposerCopy.send)
        .motion(.respond, value: model.canSend)
        .motion(.respond, value: model.working)
    }

    /// The chips in one row, or the mode chip on a row of its own above the others when the
    /// mode's name and the text size leave no room, so the mode is never cut short.
    private var accessories: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 6) {
                if shown.modeChip { shownModeChip }
                tools
            }
            VStack(alignment: .leading, spacing: 0) {
                if shown.modeChip { shownModeChip }
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
        if shown.stop {
            StopChip { Task { await model.interrupt() } }
                .opacity(fades(.stop))
                .transition(Motion.revealConceal(reduce: reduceMotion))
        }
    }

    private var shownModeChip: some View {
        modeChip
            .opacity(fades(.modeChip))
            .transition(Motion.revealConceal(reduce: reduceMotion))
    }

    private var modeChip: some View {
        Menu {
            Picker(ComposerCopy.mode, selection: Binding(
                get: { model.mode },
                set: { id in Task { await model.setMode(id) } }
            )) {
                ForEach(model.modes) { mode in
                    Text(mode.label).tag(mode.id)
                }
            }
        } label: {
            Label(model.modeLabel ?? ComposerCopy.mode, systemImage: "dial.medium")
                .labelStyle(.titleAndIcon)
                // trunc-ok: two lines in a full-width row of its own. A long mode name wraps inside its chip rather than losing its end.
                .lineLimit(2)
                .multilineTextAlignment(.leading)
        }
        .menuStyle(.button)
        .buttonStyle(ComposerChipStyle())
        .accessibilityLabel(ComposerCopy.mode)
        .accessibilityValue(model.modeLabel ?? "")
        .help(model.modeLabel.map(ComposerCopy.modeValue) ?? ComposerCopy.mode)
    }

    /// The keyboard helps with natural language only.
    private var assists: Bool { CommandLineLogic.keyboardAssists(model.draft) }

    private var placeholder: String {
        ComposerCopy.message(model.store.descriptor?.adapter?.name ?? "")
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
        switch press.key {
        case .return:
            if press.modifiers.contains(.shift) { return .ignored }
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
            guard menuOpen, let item = model.highlighted else { return .ignored }
            model.choose(item)
            return .handled
        case .escape:
            if menuOpen { model.closeMenu(); return .handled }
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

/// What the bar shows besides the field, mirrored so each change is one transaction.
struct ComposerParts: Equatable {
    enum Part: Hashable { case menu, queued, notice, attachNotice, pictures, modeChip, stop }

    var menu = false
    var queued = 0
    var notice: String?
    var attachNotice: String?
    var pictures = false
    var modeChip = false
    var stop = false

    @MainActor init(_ model: CommandLineModel) {
        menu = model.menu != .none
        queued = model.queuedCount
        notice = model.notice
        attachNotice = model.attachments.notice
        pictures = !model.attachments.isEmpty
        modeChip = model.showsModeChip
        stop = model.canInterrupt
    }

    init() {}

    /// The parts present, for telling what leaves.
    var parts: Set<Part> {
        var out: Set<Part> = []
        if menu { out.insert(.menu) }
        if queued > 0 { out.insert(.queued) }
        if notice != nil { out.insert(.notice) }
        if attachNotice != nil { out.insert(.attachNotice) }
        if pictures { out.insert(.pictures) }
        if modeChip { out.insert(.modeChip) }
        if stop { out.insert(.stop) }
        return out
    }
}

/// A screen id the composer opens in a sheet.
struct ScreenRequest: Identifiable {
    var id: String
}

/// Stop for the running turn: the stop mark and "Stop", or the mark alone when the row is too
/// narrow for both, so the word never truncates. VoiceOver says "Interrupt" either way.
private struct StopChip: View {
    let action: () -> Void

    var body: some View {
        ViewThatFits(in: .horizontal) {
            button(.titleAndIcon)
            button(.iconOnly)
        }
        .layoutPriority(1)
    }

    private func button(_ style: some LabelStyle) -> some View {
        Button(action: action) {
            Label(ComposerCopy.stop, systemImage: "stop.fill")
                .labelStyle(style)
                .fixedSize()
        }
        .buttonStyle(ComposerChipStyle(prominent: true))
        .accessibilityLabel(ComposerCopy.interrupt)
    }
}

/// The small capsules under the field: the mode chip, commands, @ and Stop.
struct ComposerChipStyle: ButtonStyle {
    @Environment(\.displayScale) private var displayScale
    var prominent = false

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.subheadline.weight(.semibold))
            // trunc-ok: chips are icons, Stop (which drops to its icon via ViewThatFits) or the mode chip, which overrides this and wraps
            .foregroundStyle(prominent ? Color.togoInk : Color.togoSnow)
            .lineLimit(1)
            .padding(.horizontal, 12)
            .frame(minWidth: 34, minHeight: 32)
            .background(prominent ? Color.togoSnow : Color.togoSnow.opacity(0.06), in: .capsule)
            .overlay {
                if !prominent { Capsule().strokeBorder(Color.togoLine, lineWidth: 1 / displayScale) }
            }
            .frame(minHeight: 44)
            .contentShape(.capsule)
            .opacity(configuration.isPressed ? 0.8 : 1)
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
