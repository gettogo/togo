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
    func attachTerminalMenu() async {
        let binding = conversation
        await nativeInteractions.stopTask?.value
        guard has(HarnessCapability.terminalNative), phase == .live, !working,
              conversation == binding, interaction == nil, terminalInteraction == nil else { return }
        let model = makeTerminalInteraction(binding: binding)
        terminalInteraction = model
        do { _ = try await model.attach() }
        catch { if terminalInteraction === model { retireTerminalInteraction() } }
    }

    private func makeTerminalInteraction(binding: UUID) -> TerminalNativeInteraction {
        let model = TerminalNativeInteraction(sessionID: subject.sessionID) { [services, subject] in
            try await services.terminalConnection(subject)
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
