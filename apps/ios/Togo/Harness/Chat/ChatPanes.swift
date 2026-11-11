import SwiftUI
import TogoKit
import UIKit

/// The chat layer over each pane whose tab shows it. The terminal stays attached underneath,
/// so switching layers never reconnects. Drawn in the panes' own layout pass, so it sits on the
/// pane from the first frame and keeps clear of a pane's header, the composer and the
/// keyboard (`insets.bottom`).
struct ChatPanes: View {
    /// Loops held from above (a snapshot, a UI test) stay held in a shown chat too.
    @Environment(\.loopsPaused) private var loopsPaused
    let store: AppStore
    let geometry: PaneGeometry
    let insets: EdgeInsets

    var body: some View {
        GeometryReader { proxy in
            ZStack(alignment: .topLeading) {
                pane(.primary, height: proxy.size.height)
                if geometry.secondary != nil {
                    pane(.secondary, height: proxy.size.height)
                }
            }
        }
    }

    @ViewBuilder private func pane(_ pane: Pane, height: CGFloat) -> some View {
        let id = pane == .primary ? store.workspace.selected : store.workspace.secondary
        if let id, let rect = geometry.frame(of: pane) {
            let top = geometry.showsTwoPanes ? PaneGeometry.headerHeight : 0
            let layer = store.layer(for: id)
            let harness = store.harness(for: id)
            // The chat ends at the composer's top edge, above the keyboard. `insets` come from
            // this same layout pass, so the chat's height follows the composer and the keyboard
            // in the frame they change. (A height measured after layout and reserved inside the
            // chat lagged a frame: every new line shrank the chat and grew it back, and the
            // scroll position fought the anchor each time.)
            let bottom = max(0, rect.maxY - (height - insets.bottom))
            // In book posture with one pane, the chat keeps to the leading page; the rest of the
            // pane stays ink so the terminal underneath does not show beside it.
            let page = geometry.chatFrame(of: pane) ?? rect
            let shown = layer == .chat
            ZStack {
                if layer == .pending {
                    // Not decided yet: the paws over the terminal, never the terminal itself.
                    SessionLoader(agent: agentName(id))
                        // motion: state change — a cut: availability never animates; fading the
                        // loader would show the terminal under it mid-fade (the flash on open).
                        .transition(.identity)
                } else if let harness, harness.chatAvailable {
                    // Kept alive under the terminal layer, only hidden, so its scroll position and
                    // folds are where they were when the chat comes back. Its loops hold still
                    // while it is away. Only the user's toggle fades it (its own transaction);
                    // the chat becoming available is a cut.
                    // One chat per tab: its scroll position, folds and live line belong to
                    // that session and never carry into another tab's conversation.
                    ChatView(
                        store: harness, actions: store.chatActions(for: id),
                        // ⌘↩ and ⌘. answer in the focused pane's chat only.
                        answersKeys: shown && (!geometry.showsTwoPanes || store.workspace.focused == pane)
                    )
                        .id(id)
                        // motion: state change — a tab switch is a cut: two half-faded chats
                        // would let the next tab's terminal show through.
                        .transition(.identity)
                        .padding(.bottom, bottom)
                        .padding(.leading, max(0, page.minX - rect.minX))
                        .padding(.trailing, max(0, rect.maxX - page.maxX))
                        .background(Color.togoInk)
                        .opacity(shown ? 1 : 0)
                        .allowsHitTesting(shown)
                        .accessibilityHidden(!shown)
                        .environment(\.loopsPaused, loopsPaused || !shown)
                        .simultaneousGesture(TapGesture().onEnded { store.focus(pane) })
                }
            }
            // A subagent's own conversation, from a work step or a mission row.
            .modifier(OptionalSubagentChat(harness: harness, actions: store.chatActions(for: id)))
            // Links in the chat and the sheets it opens go through the chat's router.
            .environment(\.openURL, OpenURLAction { [store] url in store.openChatLink(url, from: id) })
            .frame(width: rect.width, height: max(0, rect.height - top))
            .offset(x: rect.minX, y: rect.minY + top)
        }
    }
}

extension ChatPanes {
    /// The tab's agent as the app names it, for the loader's words.
    fileprivate func agentName(_ id: TabID) -> String {
        if let name = store.harness(for: id)?.agentName, !name.isEmpty { return name }
        return store.workspace.tabs.first { $0.id == id }.flatMap { store.session(for: $0) }?.agentName ?? ""
    }
}

/// The subagent sheet for a pane whose tab has a store.
private struct OptionalSubagentChat: ViewModifier {
    let harness: HarnessSessionStore?
    let actions: ChatActions

    func body(content: Content) -> some View {
        if let harness {
            content.subagentChat(store: harness, actions: actions)
        } else {
            content
        }
    }
}

extension AppStore {
    /// What the chat, mission control and subagent chats of `id`'s session do with approvals,
    /// questions and quotes: answer through the request cards, quote into that tab's composer.
    func chatActions(for id: TabID) -> ChatActions {
        let store = self
        return ChatActions(
            card: { requestID in store.controlCenter.requests.first { $0.id == requestID } },
            answer: { requestID, decision in Task { await store.answer(requestID, decision) } },
            quote: { text in
                guard let model = store.harnessTabs.composer(for: id) else { return }
                model.draft = model.draft.isEmpty ? text : model.draft + "\n" + text
            },
            failure: { requestID in store.answerFailures[requestID] },
            open: { url in store.openChatLink(url, from: id) }
        )
    }
}

/// The bar item that flips the focused tab between chat and terminal. A symbol and a title,
/// so it can stand in a vertical bar.
struct LayerToolbarItem: ToolbarContent {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let layer: SessionLayer
    let toggle: () -> Void

    var body: some ToolbarContent {
        ToolbarItem(placement: .primaryAction) {
            Button {
                // Neither layer keeps the keyboard across the switch: the terminal under the
                // chat may not have it, and the composer stays, hidden, under the terminal.
                Self.resignKeyboard()
                // Only opacity changes: both layers keep their frames (the composer keeps its
                // place under the terminal, the terminal its grid under the chat), so no text
                // moves. Under Reduce Motion the layers swap at once (`MotionTokens` .fade).
                // motion: cause → effect — the user's toggle crossfades the layers in place and
                // turns the symbol into the other layer's; nothing else ever fades them.
                withAnimation(Motion.animation(.fade, reduce: reduceMotion)) { toggle() }
            } label: {
                Label(title, systemImage: symbol)
                    .contentTransition(reduceMotion ? .identity : .symbolEffect(.replace))
            }
            .keyboardShortcut("l", modifiers: [.command, .shift])
            .accessibilityHint(layer == .chat ? ChatCopy.showTerminalHint : ChatCopy.showChatHint)
        }
    }

    /// The item names where it goes: "Terminal" from the chat, "Chat" from the terminal.
    private var title: String { layer == .chat ? ChatCopy.terminal : ChatCopy.chat }

    private var symbol: String { layer == .chat ? "terminal" : "bubble.left.and.text.bubble.right" }

    static func resignKeyboard() {
        UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
    }
}

/// Keeps the tabs' harness stores in step with the open tabs and their machines, and says so in
/// the title when the focused tab's chat fell back to the terminal. `WindowRoot` stops the
/// stores in the background, whichever screen is up.
struct HarnessSync: ViewModifier {
    let store: AppStore
    @Binding var status: String?

    func body(content: Content) -> some View {
        content
            .onChange(of: key, initial: true) { store.syncHarness() }
            .onChange(of: store.focusedTab.flatMap { store.harness(for: $0.id) }?.fallback) { _, line in
                if let line { status = line }
            }
    }

    /// What decides which stores run: each tab, and whether its machine is listed and serves
    /// harness.v1.
    private var key: [String] {
        store.workspace.tabs.map { tab in
            let caps = store.machine(for: tab.hostID)?.capabilities
            let state = caps.map { $0.contains(Capability.harness) ? "harness" : "listed" } ?? "unlisted"
            return "\(tab.hostID.uuidString)/\(tab.sessionID)/\(state)"
        }
    }
}

extension AppStore {
    /// The bar item for the focused tab, when its session has a chat to switch to.
    var layerItem: LayerToolbarItem? {
        guard let tab = focusedTab, let harness = harness(for: tab.id), harness.chatAvailable else { return nil }
        return LayerToolbarItem(layer: harness.shownLayer) { [weak self] in self?.toggleLayer() }
    }
}
