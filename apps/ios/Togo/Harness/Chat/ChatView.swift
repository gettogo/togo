import SwiftUI
import TogoKit
import UIKit

/// The calm, full-window conversation built from the session's items: prompts, folded work
/// lines, replies, and inline approvals and questions. The composer sits at its foot, placed by
/// the workspace; the mission pill sits at its top.
struct ChatView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let store: HarnessSessionStore
    var actions: ChatActions = .inert

    @State private var expanded: [String: Disclosure] = [:]
    @State private var detail: HarnessItem?
    @State private var announcer = ChatAnnouncer()
    @Namespace private var rotorSpace
    /// Mirrors of store state that put a view in or take it out, so each change runs in its
    /// own transaction (spec §2.3): the live line, the mission pill and "Earlier".
    @State private var live: WorkingState?
    @State private var showsPill: Bool
    @State private var showsEarlierButton: Bool
    /// A needs card's drift is moving, so the pill's beacon holds still.
    @State private var ambientTaken = false

    init(store: HarnessSessionStore, actions: ChatActions = .inert) {
        self.store = store
        self.actions = actions
        _live = State(initialValue: store.liveWork)
        _showsPill = State(initialValue: store.hasMission)
        _showsEarlierButton = State(initialValue: Self.showsEarlier(store))
    }

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 20) {
                if showsEarlierButton {
                    earlier
                        .transition(Motion.revealConceal(reduce: reduceMotion))
                }
                let gaps = ChatGaps.gaps(rows)
                // Rows arriving while the chat is open reveal in the store's `resize`
                // transaction; rows on first render, from Earlier or by scrolling land at once.
                ForEach(rows) { row in
                    gapped(row, gap: gaps[row.id])
                        .id(row.id)
                        .accessibilityRotorEntry(id: row.id, in: rotorSpace)
                        .transition(Motion.revealConceal(reduce: reduceMotion))
                }
                // The chat's one live indicator stays at the foot for the whole turn: one view,
                // so waiting and working morph in place. Stop lives in the composer.
                if let live {
                    LiveLine(state: live)
                        .id("chat.working")
                        .accessibilityIdentifier("chat.working")
                        .transition(Motion.revealConceal(reduce: reduceMotion))
                }
                // Prompts the phone still holds, after the conversation: the one place they show.
                ForEach(store.queue.prompts) { prompt in
                    PendingPromptRow(
                        prompt: prompt,
                        onCancel: { store.cancelQueued(prompt.id) },
                        onRetry: { store.retryQueued(prompt.id) }
                    )
                    .transition(Motion.revealConceal(reduce: reduceMotion))
                }
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 16)
            .frame(maxWidth: 720)
            .frame(maxWidth: .infinity)
        }
        .defaultScrollAnchor(.bottom)
        .defaultScrollAnchor(.bottom, for: .sizeChanges)
        .scrollDismissesKeyboard(.interactively)
        .overlay {
            placeholder
                .motion(.fade, value: placeholderKind)
        }
        // A bar, not a plain inset: the chat starts below the pill, and anything scrolled up
        // under it fades behind the system's scroll edge effect instead of sitting cut in half.
        .safeAreaBar(edge: .top, spacing: 0) {
            if showsPill {
                MissionPill(summary: store.mission.summary) { store.showsMission = true }
                    .environment(\.ambientTaken, ambientTaken)
                    .transition(.asymmetric(
                        insertion: Motion.present(from: .top, reduce: reduceMotion),
                        removal: Motion.conceal(reduce: reduceMotion)
                    ))
            }
        }
        .onPreferenceChange(AmbientClaim.self) { ambientTaken = $0 }
        .scrollEdgeEffectStyle(.hard, for: .top)
        .background(Color.togoInk)
        .environment(\.chatImageFetch, imageFetch)
        .accessibilityRotor(ChatCopy.rotorPrompts) { rotor(where: \.isPrompt) }
        .accessibilityRotor(ChatCopy.rotorNeeds) { rotor(where: \.isNeeds) }
        .accessibilityRotor(ChatCopy.rotorReplies) { rotor(where: \.isReply) }
        .onChange(of: rows, initial: true) { _, rows in announcer.update(rows) }
        .onChange(of: store.liveWork) { _, work in follow(work) }
        .onChange(of: store.hasMission) { _, has in
            // The pill presents on dropIn with the chat's inset; it leaves by concealing while
            // the inset closes on resize.
            withAnimation(Motion.animation(has ? .dropIn : .resize, reduce: reduceMotion)) { showsPill = has }
        }
        .onChange(of: Self.showsEarlier(store)) { _, shows in
            withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { showsEarlierButton = shows }
        }
        .modifier(WorkingAnnouncements(active: store.liveWork != nil))
        .sheet(item: $detail) { step in
            StepDetail(step: step, canShowInTerminal: canShow(step)) {
                Task { await store.showInTerminal(step) }
            }
        }
    }

    /// The live line arrives and leaves in a `resize` transaction; while it stays, its clock
    /// and words change in place.
    private func follow(_ work: WorkingState?) {
        guard (live == nil) != (work == nil) else {
            live = work
            return
        }
        withAnimation(Motion.animation(.resize, reduce: reduceMotion)) { live = work }
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
                        PromptBubble(text: user.text, note: promptNote(user), dimmed: user.queued)
                        // Queued prompts show only here, so their Take back is on the row itself.
                        if user.queued {
                            Button(ComposerCopy.cancelQueued) { Task { await store.cancelQueued(item: item) } }
                                .buttonStyle(PlainAnswerStyle())
                                .font(.footnote)
                        }
                    }
                    .modifier(RowActions(item: item, spoken: ChatCopy.spokenPrompt(user.text), host: self))
                }
            }
        case .reply(let item):
            if case .assistant(let text) = item.body {
                ReplyRow(text: text.text)
                    .modifier(RowActions(item: item, spoken: ChatCopy.spokenReply(text.text), host: self))
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
                    .modifier(RowActions(item: item, spoken: notice.text, host: self))
            }
        case .shell(let item):
            CommandBlockView(item: item, store: store)
        }
    }

    @ViewBuilder private func needsView(_ item: HarnessItem) -> some View {
        switch item.body {
        case .approval(let approval):
            ApprovalRow(item: item, approval: approval, card: actions.card(approval.requestId)) { decision in
                actions.answer(approval.requestId, decision)
            }
            .contextMenu { terminalButton(item) }
        case .question(let question):
            QuestionRow(item: item, question: question) { answers in
                guard let result = try? await store.answer(item, answers: answers) else { return false }
                return result.result != .failed
            }
            .contextMenu { terminalButton(item) }
        default:
            EmptyView()
        }
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
                    .transition(.opacity)
            case .reconnecting:
                ProgressView(ChatCopy.reconnecting)
                    .foregroundStyle(Color.togoMute)
                    .transition(.opacity)
            case .live, .terminalOnly:
                ContentUnavailableView {
                    Label(ChatCopy.emptyTitle, systemImage: "bubble.left.and.text.bubble.right")
                } description: {
                    Text(ChatCopy.emptyDetail)
                }
                .foregroundStyle(Color.togoMute)
                .transition(.opacity)
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
