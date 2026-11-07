import Foundation
import TogoKit
import TogoTerminal

// The ways into subagent chats, background terminals and "Show in terminal", for mission
// control, the roster and the chat:
//
// - Subagent chat by item id: `HarnessSessionStore.openSubagent(_:)`, presented by the
//   `subagentChat(store:actions:)` modifier, or `SubagentChatView(store:agentID:)` pushed on a
//   navigation stack.
// - Background terminal by item id: `BackgroundTerminalView(store:itemID:openTerminal:)`, or
//   straight to the live terminal with `AppStore.openBackgroundTerminal(itemID:from:)`.
// - Show in terminal by anchor: `HarnessSessionStore.showInTerminal(anchor:)`.
// - Any mission row's open target: `AppStore.openHarnessTarget(_:from:)`.

extension HarnessSessionStore {
    /// Shows the subagent's own conversation: the agent item's id.
    func openSubagent(_ itemID: String) {
        focusedAgent = itemID
    }

    /// The background process with this row or item id, as its card shows it.
    func backgroundJob(_ id: String) -> BackgroundJob? {
        BackgroundJob.find(id, mission: mission, timeline: timeline)
    }

    /// Switches to the terminal and, when the helper can place the anchor in the pane now, asks
    /// the terminal to scroll there and mark it. Over a link without scrollback the terminal
    /// shows the capture sheet at the needle instead. With no anchor, or one the pane no longer
    /// holds, it only switches.
    func showInTerminal(anchor: HarnessAnchor?) async {
        var jump: TerminalJump?
        if let anchor, has(HarnessCapability.anchors),
           let location = try? await services.locate(subject, anchor: anchor), location.found {
            jump = TerminalJump(
                sessionID: subject.sessionID, linesFromBottom: max(0, location.linesFromBottom),
                needle: location.needle ?? anchor.needle
            )
        }
        pendingJump = jump
        layer = .terminal
    }
}

extension AppStore {
    /// Opens a live terminal on a background process: its session as a tab, beside the one on
    /// screen when two panes fit, else in place of it. The tab attaches like any other, so the
    /// output keeps coming while it shows.
    @discardableResult
    func openBackgroundTerminal(_ target: TerminalTarget, on hostID: HostID) -> Bool {
        let id = TabID(hostID: hostID, sessionID: target.sessionID)
        if panesVisible, workspace.selected != id {
            openBeside(id)
        } else {
            open(hostID: hostID, sessionID: target.sessionID)
        }
        return true
    }

    /// The live terminal of the background process with this row or item id, in `tab`'s
    /// session. False when the process has no pane of its own.
    @discardableResult
    func openBackgroundTerminal(itemID: String, from tab: TabID) -> Bool {
        guard let job = harness(for: tab)?.backgroundJob(itemID), let target = job.terminal else { return false }
        return openBackgroundTerminal(target, on: tab.hostID)
    }

    /// Follows a mission control row's open target for the session in `tab`: a subagent's chat,
    /// a live terminal, or the session's terminal at an approval. Screens, diffs and links are
    /// the caller's. False when this does not handle the target.
    @discardableResult
    func openHarnessTarget(_ target: HarnessOpen, from tab: TabID) -> Bool {
        guard let harness = harness(for: tab) else { return false }
        switch target.kind {
        case .chat:
            harness.openSubagent(target.ref)
            return true
        case .terminal:
            guard let terminal = TerminalTarget(target.ref) else {
                harness.layer = .terminal
                return true
            }
            return openBackgroundTerminal(terminal, on: tab.hostID)
        default:
            return false
        }
    }
}

extension TerminalHandle {
    /// The engine behind the handle's view, for scrolling to and marking a line.
    var engine: SwiftTermEngine? {
        (engineView as? ThemedTerminalController)?.terminal.engine
    }
}
