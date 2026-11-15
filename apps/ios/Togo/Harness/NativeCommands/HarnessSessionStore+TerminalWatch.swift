import Foundation
import TogoKit

extension HarnessSessionStore {
    func applyNativeNotification(_ note: HarnessNotification) -> HarnessApply? {
        if note.kind == .commands { return refreshCommands(note) }
        guard note.kind == .menu else { return nil }
        guard phase == .live, feed.isLoaded, has(HarnessCapability.terminalNative) else { return .ignored }
        return nativeInteractions.terminalWatch.receive(note, sessionId: subject.sessionID,
            conversation: conversation, epoch: feed.timeline.epoch) ? .applied : .ignored
    }

    var readyTerminalMenu: UUID? {
        nativeInteractions.terminalWatch.ready(conversation: conversation, epoch: feed.timeline.epoch)
    }

    var canAttachTerminalMenu: Bool {
        phase == .live && has(HarnessCapability.terminalNative) && !working && interaction == nil
            && terminalInteraction == nil && !nativeInteractions.stopping
    }

    /// The visible chat consumes this signal once. A hidden pane, another
    /// command or another conversation cannot claim a late menu.
    func attachReadyTerminalMenu(_ request: UUID) async {
        guard canAttachTerminalMenu, shownLayer == .chat else { return }
        await attachTerminalMenu(request: request)
    }
}

extension AppStore {
    /// Navigation, rather than view reconstruction during a fold, decides
    /// which tabs may still receive an automatic late-menu presentation.
    func cancelHiddenTerminalWatches() {
        let visible = workspaceOpen ? Set([workspace.selected, panesVisible ? workspace.secondary : nil].compactMap { $0 }) : []
        for (id, store) in harnessTabs.stores where !visible.contains(id) {
            store.nativeInteractions.terminalWatch.cancel()
        }
    }
}
