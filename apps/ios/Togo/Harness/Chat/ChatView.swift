import SwiftUI
import TogoKit
import UIKit

/// The calm, full-window conversation built from the session's items: prompts, folded work
/// lines, replies, and inline approvals and questions. The composer sits at its foot, placed by
/// the workspace; the chat keeps its height clear and starts at the top, with no band above it.
struct ChatView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let store: HarnessSessionStore
    var actions: ChatActions = .inert

    @State private var expanded: [String: Disclosure] = [:]
    @State private var detail: HarnessItem?
    @State private var selecting: SelectedText?
    @State private var announcer = ChatAnnouncer()
    @State private var scroll = ChatScrollModel()
    @Namespace private var rotorSpace
    /// Mirrors of store state that put a view in or take it out, so each change runs in its
    /// own transaction (spec §2.3): the live line and "Earlier". Until the store's first load
    /// they follow it without animation: what is there on open is simply there.
    @State private var live: WorkingState?
    @State private var showsEarlierButton: Bool

    init(store: HarnessSessionStore, actions: ChatActions = .inert) {
        self.store = store
        self.actions = actions
        _live = State(initialValue: store.liveWork)
        _showsEarlierButton = State(initialValue: Self.showsEarlier(store))
    }

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 20) {
                // "Earlier" is a navigation affordance at the top: it is simply there or not.
                if showsEarlierButton {
                    earlier
                }
                let gaps = store.gaps
                // Rows arriving while the chat is open land in the store's `resize`
                // transaction; rows on first render, from Earlier or by scrolling land at once.
                ForEach(rows) { row in
                    gapped(row, gap: gaps[row.id])
                        .id(row.id)
                        .modifier(NeedsVisibility(id: row.isNeeds ? row.id : nil, scroll: scroll))
                        .accessibilityRotorEntry(id: row.id, in: rotorSpace)
                        // motion: preventing a jump — the conversation glides up to make room while the new row only fades in place; a needs card instead grows in place as it is scrolled into view (attention).
                        .transition(row.isNeeds ? Motion.unfold(reduce: reduceMotion) : Motion.arrive(reduce: reduceMotion))
                }
                // The chat's one live indicator stays at the foot for the whole turn: one view,
                // so waiting and working morph in place. Stop lives in the composer.
                if let live {
                    LiveLine(state: live)
                        .id("chat.working")
                        .accessibilityIdentifier("chat.working")
                        // motion: continuity — the line is simply there when a turn starts (the spinner starting is the signal) and leaves as the work line above turns into "Worked … ✓".
                        .transition(.asymmetric(insertion: .identity, removal: Motion.conceal(reduce: reduceMotion)))
                }
                // Prompts the phone still holds, after the conversation: the one place they show.
                ForEach(store.queue.prompts) { prompt in
                    PendingPromptRow(
                        prompt: prompt,
                        onCancel: { store.cancelQueued(prompt.id) },
                        onRetry: { store.retryQueued(prompt.id) }
                    )
                    // motion: cause → effect — the sent prompt lands in the send's own transaction as the draft clears; it only fades, nothing flies.
                    .transition(Motion.arrive(reduce: reduceMotion))
                }
                // Pages the agent asked to open and the user has not used: here until used,
                // dismissed or expired, however the sheet that asked went.
                ForEach(LinkCenter.shared.requests.pending(for: TabID(hostID: store.subject.hostID, sessionID: store.subject.sessionID))) { open in
                    PendingOpenBanner(open: open)
                        .id("chat.open." + open.id.uuidString)
                        // motion: state change — the banner fades in at the foot and fades out once used; nothing moves.
                        .transition(Motion.arrive(reduce: reduceMotion))
                }
            }
            .padding(.horizontal, ChatLayout.inset)
            .padding(.vertical, 16)
            .frame(maxWidth: ChatLayout.columnWidth)
            .frame(maxWidth: .infinity)
        }
        .defaultScrollAnchor(.bottom)
        .defaultScrollAnchor(.bottom, for: .sizeChanges)
        .scrollDismissesKeyboard(.interactively)
        // While the composer's / or @ menu is up, a tap on the conversation puts the keyboard
        // away, and the menu goes with it: a way out that needs no aim.
        .simultaneousGesture(
            TapGesture().onEnded { LayerToolbarItem.resignKeyboard() },
            including: store.composerMenuOpen ? .all : .subviews
        )
        .overlay {
            placeholder
                // motion: state change — "Reading" gives way to the empty state in place, never a jump.
                .motion(.fade, value: placeholderKind)
        }
        // Keeps the composer's height clear, scrolls to what needs the user and offers the
        // "Needs you" capsule while a card is off screen.
        .modifier(ChatScrolling(store: store, marks: needsMarks, scroll: scroll))
        .background(Color.togoInk)
        .environment(\.chatImageFetch, imageFetch)
        .accessibilityRotor(ChatCopy.rotorPrompts) { rotor(where: \.isPrompt) }
        .accessibilityRotor(ChatCopy.rotorNeeds) { rotor(where: \.isNeeds) }
        .accessibilityRotor(ChatCopy.rotorReplies) { rotor(where: \.isReply) }
        .onChange(of: rows, initial: true) { _, rows in announcer.update(rows) }
        .onChange(of: store.liveWork) { _, work in follow(work) }
        .onChange(of: Self.showsEarlier(store)) { _, shows in
            // Instant: it sits above what the person reads, so nothing they see moves.
            withTransaction(Transaction(animation: nil)) { showsEarlierButton = shows }
        }
        .modifier(WorkingAnnouncements(active: store.liveWork != nil))
        .background { keyAnswers }
        // A step's details: a small panel, sized by the window's room (`AdaptivePresentation`).
        .harnessPresent(item: $detail, as: .smallForm, name: "chat.stepDetail") { step in
            StepDetail(step: step, canShowInTerminal: canShow(step)) {
                Task { await store.showInTerminal(step) }
            }
        }
        .presentSheet("chat.selectText", item: $selecting) { selected in
            SelectTextSheet(text: selected.text)
        }
        // After the sheets, so the step details' links go through the same router.
        .environment(\.openURL, OpenURLAction { url in open(url) })
    }

    /// The live line is simply there when a turn starts and leaves with it, in a `resize`
    /// transaction so the rows make room; while it stays, its contents change with no
    /// animation at all, so the store's arrival transaction never reaches the clock. Before
    /// the first load it is simply there.
    private func follow(_ work: WorkingState?) {
        let change = LiveFollow.change(from: live, to: work, loaded: store.hasLoaded)
        if change.animates {
            // motion: preventing a jump — the rows above make room as the turn's live line comes or goes.
            withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { live = work }
        } else {
            withTransaction(Transaction(animation: nil)) { live = work }
        }
        if change == .appear, let target = ChatScroll.liveAppeared(
            was: false, now: true, afterSend: store.awaitingSince != nil, dragging: scroll.dragging
        ) {
            scroll.go(target, reduce: reduceMotion)
        }
    }

    /// ⌘↩ and ⌘. answer the topmost open request from a hardware keyboard (`ChatKeyAnswers`).
    @ViewBuilder private var keyAnswers: some View {
        let ids = rows.compactMap { row -> String? in
            guard case .needs(let item) = row, case .approval(let approval) = item.body else { return nil }
            return approval.requestId
        }
        if let target = ChatKeyAnswers.target(requestIDs: ids, card: actions.card) {
            ZStack {
                Button(ChatCopy.approve) { actions.answer(target.requestID, target.approve) }
                    .keyboardShortcut(.return, modifiers: .command)
                Button(ChatCopy.deny) { actions.answer(target.requestID, target.deny) }
                    .keyboardShortcut(".", modifiers: .command)
            }
            .frame(width: 0, height: 0)
            .opacity(0)
            .accessibilityHidden(true)
        }
    }

    /// The open needs cards, for scrolling to them and for the capsule.
    private var needsMarks: [NeedsMark] {
        ChatScroll.marks(rows) { approval in actions.card(approval.requestId) != nil }
    }

    /// A link tapped in the chat. A file opens in the review, which waits for the step's
    /// details to close first.
    private func open(_ url: URL) -> OpenURLAction.Result {
        guard detail != nil, case .file = ChatLinks.route(url) else { return actions.open(url) }
        detail = nil
        let actions = self.actions
        Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(450))
            _ = actions.open(url)
        }
        return .handled
    }

    /// Pictures by path come through the helper where it serves `fs.image`.
    private var imageFetch: ChatImageFetch? {
        guard store.subject.capabilities.contains(Capability.image) else { return nil }
        let store = self.store
        return ChatImageFetch { path in try await store.services.image(store.subject, path: path) }
    }

    /// The tools' pictures in a work row, each once.
    static func pictures(_ steps: [HarnessItem]) -> [HarnessImage] {
        var seen: Set<String> = []
        var out: [HarnessImage] = []
        for step in steps {
            guard case .tool(let tool) = step.body else { continue }
            for image in tool.images ?? [] {
                let key = ChatImageLoader.key(image) ?? UUID().uuidString
                if seen.insert(key).inserted { out.append(image) }
            }
        }
        return Array(out.prefix(6))
    }

    /// The main conversation, with the subagents' waiting approvals and questions folded in
    /// where they arrived. A subagent's own conversation is `SubagentChatView`.
    private var rows: [ChatRow] { store.rows }

    // MARK: Rows

    /// A row after a pause of over a minute gets its time above it; the marker of a turn the
    /// agent started on its own says how long after instead.
    @ViewBuilder private func gapped(_ row: ChatRow, gap: TimeInterval?) -> some View {
        if gap != nil, !row.isContinued, let start = ChatGaps.start(of: row) {
            VStack(alignment: .leading, spacing: 12) {
                ChatTimestamp(date: start)
                rowView(row, gap: gap)
            }
        } else {
            rowView(row, gap: gap)
        }
    }

    @ViewBuilder private func rowView(_ row: ChatRow, gap: TimeInterval?) -> some View {
        switch row {
        case .prompt(let item):
            if case .user(let user) = item.body {
                VStack(alignment: .trailing, spacing: 6) {
                    // The pictures stay their own elements, so each opens full screen.
                    if !user.images.isEmpty {
                        ChatImageGrid(images: user.images, alignment: .trailing)
                            .padding(.leading, 48)
                            .opacity(user.queued ? 0.6 : 1)
                    }
                    VStack(alignment: .trailing, spacing: 2) {
                        PromptBubble(
                            text: user.text, note: promptNote(user), dimmed: user.queued,
                            command: user.images.isEmpty && CommandEcho.isCommand(user.text, known: store.commandList)
                        )
                        // Queued prompts show only here, so their Take back is on the row itself.
                        if user.queued {
                            Button(ComposerCopy.cancelQueued) { Task { await store.cancelQueued(item: item) } }
                                .buttonStyle(PlainAnswerStyle())
                                .font(.footnote)
                        }
                    }
                    .modifier(RowActions(item: item, spoken: ChatCopy.spokenPrompt(user.text), host: self) { selecting = $0 })
                }
            }
        case .reply(let item):
            if case .assistant(let text) = item.body {
                ReplyRow(text: text.text)
                    .modifier(RowActions(item: item, spoken: ChatCopy.spokenReply(text.text), host: self) { selecting = $0 })
            }
        case .work(let id, let summary, let steps):
            let disclosure = expanded[id] ?? Disclosure()
            // Pictures the turn's tools read or made show without opening the steps.
            ChatWorkRow(
                line: WorkLine(
                    summary: summary, steps: steps, disclosure: disclosure,
                    onToggle: { toggle(id) }, onStep: open,
                    spoken: ChatCopy.spokenWork(summary, now: .now, expanded: disclosure.isOpen),
                    showInTerminal: steps.first.flatMap { first in
                        canShow(first) ? { Task { await store.showInTerminal(first) } } : nil
                    },
                    // The live line at the foot carries the clock while the turn runs.
                    counting: live == nil
                ),
                pictures: Self.pictures(steps)
            ) {
                if let first = steps.first, canShow(first) {
                    Button(ChatCopy.showInTerminal, systemImage: "terminal") { Task { await store.showInTerminal(first) } }
                }
            }
        case .needs(let item):
            needsView(item)
        case .notice(let item):
            if case .notice(let notice) = item.body, row.isContinued {
                TurnMarkerRow(notice: notice, gap: gap)
            } else if case .notice(let notice) = item.body {
                ChatNoticeRow(notice: notice)
                    .modifier(RowActions(item: item, spoken: notice.text, host: self) { selecting = $0 })
            }
        case .shell(let item):
            CommandBlockView(item: item, store: store)
        }
    }

    @ViewBuilder private func needsView(_ item: HarnessItem) -> some View {
        switch item.body {
        case .approval(let approval):
            ApprovalRow(
                item: item, approval: approval, card: actions.card(approval.requestId),
                failure: actions.failure(approval.requestId), showInTerminal: terminalAction(item)
            ) { decision in
                actions.answer(approval.requestId, decision)
            }
            .contextMenu { terminalButton(item) }
        case .question(let question):
            QuestionRow(item: item, question: question, showInTerminal: terminalAction(item)) { args in
                await store.answerReporting(item, args: args)
            }
            .contextMenu { terminalButton(item) }
        default:
            EmptyView()
        }
    }

    /// "Show in terminal" for an item that has a place there.
    private func terminalAction(_ item: HarnessItem) -> (() -> Void)? {
        guard canShow(item) else { return nil }
        return { Task { await store.showInTerminal(item) } }
    }

    @ViewBuilder fileprivate func terminalButton(_ item: HarnessItem) -> some View {
        if canShow(item) {
            Button(ChatCopy.showInTerminal, systemImage: "terminal") { Task { await store.showInTerminal(item) } }
        }
    }

    private func promptNote(_ user: HarnessItem.User) -> String? {
        if user.queued { return ChatCopy.queued }
        return user.source == "desk" ? ChatCopy.fromDesk : nil
    }

    fileprivate func canShow(_ item: HarnessItem) -> Bool {
        item.anchor != nil && store.has(HarnessCapability.anchors)
    }

    /// A subagent step opens its own conversation; any other step its details.
    private func open(_ step: HarnessItem) {
        if case .agent = step.body {
            store.openSubagent(step.id)
        } else {
            detail = step
        }
    }

    /// Opens or closes a work line's steps: one `resize` transaction moves the rows below and
    /// the scroll position with them; closing conceals the steps first (spec §2.1/§2.2).
    private func toggle(_ id: String) {
        let disclosure = Binding { expanded[id] ?? Disclosure() } set: { expanded[id] = $0 }
        Motion.toggle(disclosure, reduce: reduceMotion)
    }

    // MARK: Earlier and placeholder

    private static func showsEarlier(_ store: HarnessSessionStore) -> Bool {
        store.timeline.hasOlder && store.has(HarnessCapability.history)
    }

    /// Which placeholder shows, so changing it crossfades.
    private var placeholderKind: Int {
        guard rows.isEmpty, store.queue.isEmpty, store.liveWork == nil else { return 0 }
        return switch store.phase {
        case .describing, .opening: 1
        case .reconnecting: 2
        case .live, .terminalOnly: 3
        }
    }

    private var earlier: some View {
        Button(ChatCopy.earlier) { Task { await store.loadOlder() } }
            .font(.footnote)
            .foregroundStyle(Color.togoMute)
            .frame(maxWidth: .infinity, minHeight: 44)
            .disabled(store.loadingOlder)
    }

    @ViewBuilder private var placeholder: some View {
        if rows.isEmpty, store.queue.isEmpty, store.liveWork == nil {
            switch store.phase {
            case .describing, .opening:
                ProgressView(ChatCopy.reading)
                    .foregroundStyle(Color.togoMute)
            case .reconnecting:
                ProgressView(ChatCopy.reconnecting)
                    .foregroundStyle(Color.togoMute)
            case .live, .terminalOnly:
                ContentUnavailableView {
                    Label(ChatCopy.emptyTitle, systemImage: "bubble.left.and.text.bubble.right")
                } description: {
                    Text(ChatCopy.emptyDetail)
                }
                .foregroundStyle(Color.togoMute)
            }
        }
    }

    // MARK: Accessibility

    private func rotor(where keep: KeyPath<ChatRow, Bool>) -> some AccessibilityRotorContent {
        ForEach(rows.filter { $0[keyPath: keep] }) { row in
            AccessibilityRotorEntry(Text(row.rotorLabel), id: row.id, in: rotorSpace)
        }
    }
}

/// Copy, Show in terminal and Quote in reply on a row, as a context menu and as VoiceOver
/// actions; the row reads as one element.
private struct RowActions: ViewModifier {
    let item: HarnessItem
    let spoken: String
    let host: ChatView
    /// Opens the row's words in a sheet where any part can be selected.
    let select: (SelectedText) -> Void

    func body(content: Content) -> some View {
        content
            .accessibilityElement(children: .combine)
            .accessibilityLabel(spoken)
            .contextMenu { menu }
            .accessibilityActions { menu }
    }

    @ViewBuilder private var menu: some View {
        if let text = item.chatText {
            Button(ChatCopy.copy, systemImage: "doc.on.doc") { UIPasteboard.general.string = text }
            Button(ChatCopy.quote, systemImage: "text.quote") { host.actions.quote(ChatQuote.quoted(text)) }
            Button(ChatCopy.selectText, systemImage: "selection.pin.in.out") {
                select(SelectedText(id: item.id, text: text))
            }
        }
        host.terminalButton(item)
        if case .user(let user) = item.body, user.queued {
            Button(ComposerCopy.cancelQueued, systemImage: "xmark.circle", role: .destructive) {
                Task { await host.store.cancelQueued(item: item) }
            }
        }
    }
}

/// Speaks a finished reply once and a new needs row once, at high priority; never each
/// streamed piece. What was on screen when the chat opened is not spoken.
@MainActor final class ChatAnnouncer {
    private var seen: Set<String>?
    private var finished: Set<String> = []

    func update(_ rows: [ChatRow]) {
        defer { remember(rows) }
        guard let seen, UIAccessibility.isVoiceOverRunning else { return }
        for row in rows {
            switch row {
            case .needs(let item) where !seen.contains(item.id):
                NeedsAnnouncement.post(ChatCopy.spokenNeeds(item.chatText ?? ""))
            case .reply(let item):
                if case .assistant(let text) = item.body, !text.partial, !finished.contains(item.id) {
                    announce(ChatCopy.spokenReply(text.text), high: false)
                }
            default:
                break
            }
        }
    }

    private func remember(_ rows: [ChatRow]) {
        seen = Set(rows.map(\.id))
        for row in rows {
            if case .reply(let item) = row, case .assistant(let text) = item.body, !text.partial {
                finished.insert(item.id)
            }
        }
    }

    private func announce(_ text: String, high: Bool) {
        var line = AttributedString(text)
        line.accessibilitySpeechAnnouncementPriority = high ? .high : .default
        AccessibilityNotification.Announcement(line).post()
    }
}

extension ChatRow {
    var isPrompt: Bool { if case .prompt = self { true } else { false } }
    var isNeeds: Bool { if case .needs = self { true } else { false } }
    var isReply: Bool { if case .reply = self { true } else { false } }

    fileprivate var rotorLabel: String {
        switch self {
        case .prompt(let item), .reply(let item), .needs(let item), .notice(let item), .shell(let item):
            String((item.chatText ?? "").prefix(80))
        case .work: ""
        }
    }
}

extension HarnessSessionStore {
    /// Mission control has something to show for this session.
    var hasMission: Bool { mission.mission.map { !$0.sections.isEmpty } ?? false }
}

/// A row's words opened in the Select text sheet.
struct SelectedText: Identifiable {
    let id: String
    let text: String
}

/// Tells the scroll model whether a needs card is on screen; other rows pass through.
private struct NeedsVisibility: ViewModifier {
    let id: String?
    let scroll: ChatScrollModel

    func body(content: Content) -> some View {
        if let id {
            content.needsVisibility(id, in: scroll)
        } else {
            content
        }
    }
}

/// The chat's reading column, shared by everything that sits in it or under it (the composer,
/// a helper's thread, a background terminal), so on a wide window they line up.
enum ChatLayout {
    /// The column's widest, centred in wider panes.
    static let columnWidth: CGFloat = 720
    /// The column's own side padding; text starts this far inside it.
    static let inset: CGFloat = 16
    /// The composer's widest: the column's text width, so the field starts where replies do.
    static let composerWidth: CGFloat = columnWidth - 2 * inset
}
