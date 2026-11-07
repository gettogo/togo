import SwiftUI
import TogoKit
import UIKit

/// The calm, full-window conversation built from the session's items: prompts, folded work
/// lines, replies, and inline approvals and questions. The composer sits at its foot, placed by
/// the workspace; the mission pill sits at its top.
struct ChatView: View {
    let store: HarnessSessionStore
    var actions: ChatActions = .inert

    @State private var expanded: Set<String> = []
    @State private var detail: HarnessItem?
    @State private var announcer = ChatAnnouncer()
    @Namespace private var rotorSpace

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 20) {
                if showsEarlier {
                    earlier
                }
                let gaps = ChatGaps.gaps(rows)
                ForEach(rows) { row in
                    gapped(row, gap: gaps[row.id])
                        .id(row.id)
                        .accessibilityRotorEntry(id: row.id, in: rotorSpace)
                }
                // While the agent works and its turn has no work line yet, the live line stands
                // alone below the newest words. Stop lives in the composer.
                if let live, liveSpot == .foot {
                    WorkLine(
                        summary: WorkingLogic.footSummary(live), steps: [], expanded: false,
                        onToggle: {}, onStep: { _ in },
                        spoken: WorkingCopy.spoken(live, now: .now), step: live.step,
                        waiting: live.waiting
                    )
                    .id("chat.working")
                    .accessibilityIdentifier("chat.working")
                    .transition(.opacity)
                }
                // Prompts the phone still holds, after the conversation: the one place they show.
                ForEach(store.queue.prompts) { prompt in
                    PendingPromptRow(
                        prompt: prompt,
                        onCancel: { store.cancelQueued(prompt.id) },
                        onRetry: { store.retryQueued(prompt.id) }
                    )
                }
            }
            .motion(.fade, value: liveSpot)
            .padding(.horizontal, 16)
            .padding(.vertical, 16)
            .frame(maxWidth: 720)
            .frame(maxWidth: .infinity)
        }
        .defaultScrollAnchor(.bottom)
        .defaultScrollAnchor(.bottom, for: .sizeChanges)
        .scrollDismissesKeyboard(.interactively)
        .overlay { placeholder }
        // A bar, not a plain inset: the chat starts below the pill, and anything scrolled up
        // under it fades behind the system's scroll edge effect instead of sitting cut in half.
        .safeAreaBar(edge: .top, spacing: 0) {
            if store.hasMission {
                MissionPill(summary: store.mission.summary) { store.showsMission = true }
            }
        }
        .scrollEdgeEffectStyle(.hard, for: .top)
        .background(Color.togoInk)
        .environment(\.chatImageFetch, imageFetch)
        .accessibilityRotor(ChatCopy.rotorPrompts) { rotor(where: \.isPrompt) }
        .accessibilityRotor(ChatCopy.rotorNeeds) { rotor(where: \.isNeeds) }
        .accessibilityRotor(ChatCopy.rotorReplies) { rotor(where: \.isReply) }
        .onChange(of: rows, initial: true) { _, rows in announcer.update(rows) }
        .modifier(WorkingAnnouncements(active: store.liveWork != nil))
        .sheet(item: $detail) { step in
            StepDetail(step: step, canShowInTerminal: canShow(step)) {
                Task { await store.showInTerminal(step) }
            }
        }
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

    private var live: WorkingState? { store.liveWork }

    /// Where the one live indicator sits: on the running turn's work line, or at the foot.
    private var liveSpot: LivePlacement? { WorkingLogic.placement(rows: rows, live: live) }

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
            let liveStep = liveSpot == .row(id) ? live?.step : nil
            let liveWaiting = liveSpot == .row(id) && live?.waiting == true
            VStack(alignment: .leading, spacing: 6) {
            WorkLine(
                summary: summary, steps: steps, expanded: expanded.contains(id),
                onToggle: { toggle(id) }, onStep: open,
                spoken: liveWaiting ? WorkingCopy.waiting
                    : [ChatCopy.spokenWork(summary, now: .now, expanded: expanded.contains(id)), liveStep]
                        .compactMap(\.self).joined(separator: ", "),
                showInTerminal: steps.first.flatMap { first in
                    canShow(first) ? { Task { await store.showInTerminal(first) } } : nil
                },
                step: liveStep,
                waiting: liveWaiting
            )
            .contextMenu {
                if let first = steps.first, canShow(first) {
                    Button(ChatCopy.showInTerminal, systemImage: "terminal") { Task { await store.showInTerminal(first) } }
                }
            }
                // Pictures the turn's tools read or made show without opening the steps.
                let pictures = Self.pictures(steps)
                if !pictures.isEmpty {
                    ChatImageGrid(images: pictures)
                        .padding(.leading, 22)
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

    private func toggle(_ id: String) {
        if expanded.contains(id) { expanded.remove(id) } else { expanded.insert(id) }
    }

    // MARK: Earlier and placeholder

    private var showsEarlier: Bool {
        store.timeline.hasOlder && store.has(HarnessCapability.history)
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
