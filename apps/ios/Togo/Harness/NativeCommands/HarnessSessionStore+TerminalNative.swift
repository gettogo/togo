import Foundation
import TogoKit

extension HarnessSessionStore {
    func retireNativeInteractions(pumpQueue: Bool) {
        retireInteraction(pumpQueue: pumpQueue)
        retireTerminalInteraction(pumpQueue: pumpQueue)
    }

    var terminalInteraction: TerminalNativeInteraction? {
        get { nativeInteractions.terminal }
        set { nativeInteractions.terminal = newValue }
    }

    func runTerminalCommand(_ text: String) async throws {
        nativeInteractions.terminalWatch.cancel()
        let binding = conversation
        await retireWaitingInteraction()
        await nativeInteractions.stopTask?.value
        guard conversation == binding, phase == .live, interaction == nil, terminalInteraction == nil else {
            throw TerminalNativeInteraction.Failure(message: SurfaceCopy.finishCurrent)
        }
        guard !working else { throw TerminalNativeInteraction.Failure(message: SurfaceCopy.agentWorking) }
        let model = makeTerminalInteraction(binding: binding)
        terminalInteraction = model
        do {
            let result = try await model.start(text)
            guard conversation == binding else { throw ConversationChanged() }
            if result.result == .fallback {
                layer = .terminal
                note(result.message ?? SurfaceCopy.unsupported, code: "command")
            }
        } catch {
            if terminalInteraction === model { retireTerminalInteraction() }
            throw error
        }
    }

    /// Re-entering chat may find a chooser left open at the desk. This is a
    /// read and claim only; no command or key is sent when no menu is present.
    func attachTerminalMenu(request: UUID? = nil) async {
        let binding = conversation
        await nativeInteractions.stopTask?.value
        guard has(HarnessCapability.terminalNative), phase == .live, !working,
              conversation == binding, interaction == nil, terminalInteraction == nil else { return }
        if let request {
            guard shownLayer == .chat, nativeInteractions.terminalWatch.claim(requestId: request,
                conversation: binding, epoch: feed.timeline.epoch) else { return }
        } else if nativeInteractions.terminalWatch.observing(conversation: binding, epoch: feed.timeline.epoch) {
            return
        }
        let model = makeTerminalInteraction(binding: binding)
        terminalInteraction = model
        do { _ = try await model.attach() }
        catch { if terminalInteraction === model { retireTerminalInteraction() } }
    }

    /// A question is independent of any command watch. Reserve its interaction
    /// before starting the read, so changing view-task availability cannot cancel
    /// the connection. Existing handback and stop paths retire that same model.
    func attachTerminalQuestion(approvals: Set<String> = []) {
        guard canAttachTerminalQuestion, shownLayer == .chat,
              nativeInteractions.requests.next(in: feed.timeline.conversation(), available: true,
                  unanswerableApprovals: approvals) != nil else { return }
        nativeInteractions.terminalWatch.cancel()
        let binding = conversation
        let model = makeTerminalInteraction(binding: binding)
        terminalInteraction = model
        Task {
            guard terminalInteraction === model, conversation == binding, phase == .live, shownLayer == .chat else { return }
            do { _ = try await model.attach() }
            catch { if terminalInteraction === model { retireTerminalInteraction() } }
        }
    }

    private func makeTerminalInteraction(binding: UUID) -> TerminalNativeInteraction {
        let model = TerminalNativeInteraction(sessionID: subject.sessionID) { [services, subject] in
            try await services.terminalConnection(subject)
        }
        model.onDispatch = { [weak self, weak model] request in
            guard let self, self.terminalInteraction === model, self.conversation == binding,
                  let epoch = self.feed.timeline.epoch else { return }
            self.nativeInteractions.terminalWatch.begin(requestId: request, conversation: binding, epoch: epoch)
        }
        model.onReceipt = { [weak self, weak model] request, result in
            guard let self, self.terminalInteraction === model, self.conversation == binding else { return }
            let submitted = result?.result == .typed && result?.screen == nil
            self.nativeInteractions.terminalWatch.finish(requestId: request, submitted: submitted)
            if submitted { self.note(PrimitiveCopy.sent, code: "command") }
        }
        model.onEnd = { [weak self, weak model] in
            guard let self, let model, self.terminalInteraction === model else { return }
            // Keep an ended, unconfirmed interaction as a native error page.
            // Open claims the observed menu anew; it never replays the action.
            if model.screen != nil, model.failure != nil { return }
            self.terminalInteraction = nil
            if self.conversation == binding {
                if model.screen != nil, let failure = model.failure { self.note(failure, code: "command") }
                self.updateNativeQuestion()
                if model.confirmedEnd { self.pump() }
            }
        }
        return model
    }

    func dismissTerminalCommand(handbackOnFailure: Bool = false) {
        nativeInteractions.terminalWatch.cancel()
        guard let model = terminalInteraction else { return }
        if model.phase == .ended {
            retireTerminalInteraction()
            layer = .terminal
            return
        }
        Task { [weak self] in
            let closed = await model.close(cancel: true)
            if !closed, handbackOnFailure, let self, self.terminalInteraction === model {
                self.retireTerminalInteraction()
                self.layer = .terminal
            }
        }
    }

    func reopenTerminalMenu() async {
        guard let previous = terminalInteraction, previous.phase == .ended, !previous.closing else { return }
        terminalInteraction = nil
        await previous.close(cancel: false)
        await attachTerminalMenu()
    }

    /// Handback never sends Esc. A later prompt waits until this connection
    /// has actually closed, even after the presentation has gone away.
    func retireTerminalInteraction(pumpQueue: Bool = false) {
        nativeInteractions.terminalWatch.cancel()
        guard let previous = terminalInteraction else { return }
        terminalInteraction = nil
        nativeInteractions.stopping = true
        let binding = conversation
        let stopID = UUID()
        nativeInteractions.stopID = stopID
        let before = nativeInteractions.stopTask
        nativeInteractions.stopTask = Task { [weak self] in
            await before?.value
            await previous.close(cancel: false)
            guard let self, self.nativeInteractions.stopID == stopID else { return }
            self.nativeInteractions.stopping = false
            self.nativeInteractions.stopTask = nil
            if pumpQueue, self.conversation == binding { self.pump() }
        }
    }
}
