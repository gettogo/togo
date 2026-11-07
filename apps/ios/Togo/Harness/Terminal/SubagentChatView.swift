import SwiftUI
import TogoKit
import UIKit

/// A subagent's own conversation, opened from its row in mission control or its step in the
/// chat. It shows only that agent's items, folded like the main chat; its approvals and
/// questions are answerable here and also wait in the main chat. Push it on a navigation stack
/// for the system back button, or present it with `subagentChat(store:actions:)`, which adds a
/// way back to the agent that started it.
struct SubagentChatView: View {
    let store: HarnessSessionStore
    let agentID: String
    var actions: ChatActions = .inert
    /// Shows a back item to the parent (an agent or the main chat). Off when a navigation stack
    /// already gives one.
    var showsBack = false

    @State private var expanded: Set<String> = []
    @State private var detail: HarnessItem?
    @State private var nested: String?

    var body: some View {
        let thread = SubagentThread(id: agentID, timeline: store.timeline)
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 20) {
                header(thread)
                ForEach(thread.rows) { row in
                    rowView(row)
                        .id(row.id)
                }
            }
            .padding(16)
            .frame(maxWidth: 720)
            .frame(maxWidth: .infinity)
        }
        .defaultScrollAnchor(.bottom)
        .defaultScrollAnchor(.bottom, for: .sizeChanges)
        .overlay {
            if thread.rows.isEmpty {
                ContentUnavailableView {
                    Label(TerminalSyncCopy.subagentEmpty, systemImage: "person.2")
                } description: {
                    Text(TerminalSyncCopy.subagentEmptyDetail)
                }
                .foregroundStyle(Color.togoMute)
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
                MarkdownText(text: summary)
                    .foregroundStyle(Color.togoSnow)
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

    @ViewBuilder private func rowView(_ row: ChatRow) -> some View {
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
            WorkLine(
                summary: summary, steps: steps, expanded: expanded.contains(id),
                onToggle: { toggle(id) }, onStep: open
            )
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(ChatCopy.spokenWork(summary, now: .now, expanded: expanded.contains(id)))
            .accessibilityAddTraits(.isButton)
            .accessibilityAction { toggle(id) }
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
            ApprovalRow(item: item, approval: approval, card: actions.card(approval.requestId)) { decision in
                actions.answer(approval.requestId, decision)
            }
            .contextMenu { menu(item) }
        case .question(let question):
            QuestionRow(item: item, question: question) { answers in
                guard let result = try? await store.answer(item, answers: answers) else { return false }
                return result.result != .failed
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

    private func canShow(_ item: HarnessItem) -> Bool {
        item.anchor != nil && store.has(HarnessCapability.anchors)
    }

    private func toggle(_ id: String) {
        if expanded.contains(id) { expanded.remove(id) } else { expanded.insert(id) }
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
