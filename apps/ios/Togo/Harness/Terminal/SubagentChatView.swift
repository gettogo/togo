import SwiftUI
import TogoKit
import UIKit

/// A subagent's own conversation, opened from its row in mission control or its step in the
/// chat. It shows only that agent's items, folded like the main chat; its approvals and
/// questions are answerable here and also wait in the main chat. Push it on a navigation stack
/// for the system back button, or present it with `subagentChat(store:actions:)`, which adds a
/// way back to the agent that started it.
struct SubagentChatView: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let store: HarnessSessionStore
    let agentID: String
    var actions: ChatActions = .inert
    /// Shows a back item to the parent (an agent or the main chat). Off when a navigation stack
    /// already gives one.
    var showsBack = false

    @State private var expanded: [String: Disclosure] = [:]
    @State private var detail: HarnessItem?
    @State private var nested: String?
    @State private var scroll = ChatScrollModel()

    var body: some View {
        let thread = SubagentThread(id: agentID, timeline: store.timeline)
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 20) {
                header(thread)
                let waiting = WorkingLogic.hasOpenNeeds(rows: thread.rows)
                // Rows that arrive while open land in the store's arrival transaction.
                ForEach(thread.rows) { row in
                    rowView(row, waiting: waiting)
                        .id(row.id)
                        // motion: preventing a jump — the thread glides up to make room while the new row only fades in place; a needs card grows in place instead (attention).
                        .transition(row.isNeeds ? Motion.unfold(reduce: reduceMotion) : Motion.arrive(reduce: reduceMotion))
                }
            }
            .padding(16)
            .frame(maxWidth: 720)
            .frame(maxWidth: .infinity)
        }
        .defaultScrollAnchor(.bottom)
        .defaultScrollAnchor(.bottom, for: .sizeChanges)
        // A card that needs the user scrolls into view as it arrives, as in the main chat.
        .modifier(NeedsScrolling(
            marks: ChatScroll.marks(thread.rows) { approval in actions.card(approval.requestId) != nil },
            loaded: store.hasLoaded, scroll: scroll
        ))
        // The empty state is there or not: the first row arriving is not an event of its own.
        .overlay {
            if thread.rows.isEmpty {
                ContentUnavailableView {
                    Label(TerminalSyncCopy.subagentEmpty, systemImage: "person.2")
                } description: {
                    Text(TerminalSyncCopy.subagentEmptyDetail)
                }
                .foregroundStyle(Color.togoMute)
                .transaction { $0.animation = nil }
            }
        }
        .background(Color.togoInk)
        .navigationTitle(thread.title)
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if showsBack {
                ToolbarItem(placement: .cancellationAction) {
                    Button(backTitle(thread), systemImage: "chevron.backward") { store.focusedAgent = thread.parent }
                        .keyboardShortcut(.escape, modifiers: [])
                }
            }
        }
        .navigationDestination(item: $nested) { id in
            SubagentChatView(store: store, agentID: id, actions: actions)
        }
        .sheet(item: $detail) { step in
            StepDetail(step: step, canShowInTerminal: canShow(step)) {
                store.focusedAgent = nil
                Task { await store.showInTerminal(step) }
            }
        }
    }

    // MARK: Header

    private func header(_ thread: SubagentThread) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                PrimitiveStateGlyph(state: thread.state, font: .footnote)
                Text(verbatim: TerminalSyncCopy.agentLine(role: thread.agent?.role, state: ChatCopy.stepState(thread.state)))
                    .font(.mono(.footnote))
                    .foregroundStyle(Color.togoMute)
            }
            if let summary = thread.agent?.summary, !summary.isEmpty {
                // The summary is status: it is simply there, never carried by the arrival transaction.
                MarkdownText(text: summary)
                    .foregroundStyle(Color.togoSnow)
                    .transaction { $0.animation = nil }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }

    private func backTitle(_ thread: SubagentThread) -> String {
        guard let parent = thread.parent else { return TerminalSyncCopy.back(to: TerminalSyncCopy.mainChat) }
        return TerminalSyncCopy.back(to: SubagentThread(id: parent, timeline: store.timeline).title)
    }

    // MARK: Rows

    @ViewBuilder private func rowView(_ row: ChatRow, waiting: Bool) -> some View {
        switch row {
        case .prompt(let item):
            if case .user(let user) = item.body {
                PromptBubble(text: user.text)
                    .accessibilityElement(children: .combine)
                    .accessibilityLabel(ChatCopy.spokenPrompt(user.text))
                    .contextMenu { menu(item) }
            }
        case .reply(let item):
            if case .assistant(let text) = item.body {
                ReplyRow(text: text.text)
                    .accessibilityElement(children: .combine)
                    .accessibilityLabel(ChatCopy.spokenReply(text.text))
                    .contextMenu { menu(item) }
            }
        case .work(let id, let summary, let steps):
            let disclosure = expanded[id] ?? Disclosure()
            WorkLine(
                summary: summary, steps: steps, disclosure: disclosure,
                onToggle: { toggle(id) }, onStep: open,
                spoken: ChatCopy.spokenWork(summary, now: .now, expanded: disclosure.isOpen),
                waiting: waiting
            )
        case .needs(let item):
            needsView(item)
        case .notice(let item):
            if case .notice(let notice) = item.body {
                ChatNoticeRow(notice: notice)
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
            .contextMenu { menu(item) }
        case .question(let question):
            QuestionRow(item: item, question: question, showInTerminal: terminalAction(item)) { args in
                await store.answerReporting(item, args: args)
            }
            .contextMenu { menu(item) }
        default:
            EmptyView()
        }
    }

    @ViewBuilder private func menu(_ item: HarnessItem) -> some View {
        if let text = item.chatText {
            Button(ChatCopy.copy, systemImage: "doc.on.doc") { UIPasteboard.general.string = text }
        }
        if canShow(item) {
            Button(ChatCopy.showInTerminal, systemImage: "terminal") {
                store.focusedAgent = nil
                Task { await store.showInTerminal(item) }
            }
        }
    }

    /// A step that is itself an agent opens that agent's conversation; any other opens its
    /// details.
    private func open(_ step: HarnessItem) {
        if case .agent = step.body {
            nested = step.id
        } else {
            detail = step
        }
    }

    /// "Show in terminal" for an item that has a place there; it leaves this conversation.
    private func terminalAction(_ item: HarnessItem) -> (() -> Void)? {
        guard canShow(item) else { return nil }
        return {
            store.focusedAgent = nil
            Task { await store.showInTerminal(item) }
        }
    }

    private func canShow(_ item: HarnessItem) -> Bool {
        item.anchor != nil && store.has(HarnessCapability.anchors)
    }

    /// Opens or closes a work line's steps as a two-phase disclosure (spec §2.1/§2.2).
    private func toggle(_ id: String) {
        let disclosure = Binding { expanded[id] ?? Disclosure() } set: { expanded[id] = $0 }
        Motion.toggle(disclosure, reduce: reduceMotion)
    }
}

extension View {
    /// Presents the subagent conversation the store points at (`focusedAgent`) as a sheet, with a
    /// way back up. Mission control and the chat open one by setting `focusedAgent`.
    func subagentChat(store: HarnessSessionStore, actions: ChatActions = .inert) -> some View {
        modifier(SubagentChatPresenter(store: store, actions: actions))
    }
}

private struct SubagentChatPresenter: ViewModifier {
    let store: HarnessSessionStore
    let actions: ChatActions

    private struct Route: Identifiable {
        let id: String
    }

    func body(content: Content) -> some View {
        let route = Binding<Route?>(
            get: { store.focusedAgent.map(Route.init) },
            set: { store.focusedAgent = $0?.id }
        )
        content.sheet(item: route) { route in
            NavigationStack {
                SubagentChatView(store: store, agentID: route.id, actions: actions, showsBack: true)
            }
            .presentationDetents([.medium, .large])
            .presentationDragIndicator(.visible)
        }
    }
}
