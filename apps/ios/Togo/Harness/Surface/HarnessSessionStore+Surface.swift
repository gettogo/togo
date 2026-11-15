import Foundation
import Observation
import TogoKit

@MainActor @Observable final class NativeChatState {
    var current: ChatSurfaceInteraction?
    var terminal: TerminalNativeInteraction?
    var terminalWatch = TerminalCommandWatch()
    var stopping = false
    var requests = HarnessNativeRequests()
    @ObservationIgnored var stopTask: Task<Void, Never>?
    @ObservationIgnored var stopID = UUID()
    @ObservationIgnored var dismissing: UUID?
}

extension HarnessSessionStore {
    var interaction: ChatSurfaceInteraction? {
        get { nativeInteractions.current }
        set {
            if newValue != nil { nativeInteractions.terminalWatch.cancel() }
            nativeInteractions.current = newValue
        }
    }

    var surfaceAvailable: Bool {
        subject.capabilities.contains(Capability.surface) && has(HarnessCapability.surface)
    }

    func runNativeCommand(_ text: String, command: HarnessCompletion? = nil) async throws {
        nativeInteractions.terminalWatch.cancel()
        if !surfaceAvailable, has(HarnessCapability.terminalNative) {
            try await runTerminalCommand(text)
            return
        }
        let binding = conversation
        await retireWaitingInteraction()
        await nativeInteractions.stopTask?.value
        guard conversation == binding, phase == .live, surfaceAvailable, interaction == nil else {
            throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.finishCurrent)
        }
        guard !working else { throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.agentWorking) }
        let pending = makeInteraction(command: text, commandInfo: command)
        interaction = pending
        do { try await pending.submit() }
        catch {
            if interaction === pending, !pending.attachedExisting {
                retireInteraction()
                await nativeInteractions.stopTask?.value
            }
            throw error
        }
    }

    /// An explicit next submission moves on from a command that has shown no
    /// controls. It never dismisses or types over an actual original form.
    func retireWaitingInteraction() async {
        if let current = interaction, current.submitted, current.blocks.isEmpty {
            retireInteraction()
        }
        await nativeInteractions.stopTask?.value
    }

    /// Explicit chat intent hands a completed submission back before either
    /// a prompt or an action can type. Actual original controls stay protected.
    func prepareHarnessInput() async throws {
        nativeInteractions.terminalWatch.cancel()
        let binding = conversation
        await retireWaitingInteraction()
        guard conversation == binding else { throw ConversationChanged() }
        guard interaction == nil, terminalInteraction == nil else { throw ChatSurfaceInteraction.Failure(message: SurfaceCopy.finishCurrent) }
    }

    func canOpenNativeQuestion(_ item: HarnessItem) -> Bool {
        guard phase == .live, surfaceAvailable, interaction == nil, terminalInteraction == nil, !nativeInteractions.stopping,
              case .question(let question) = item.body else { return false }
        return !question.isAnswerable && (question.answered?.isEmpty ?? true)
    }

    func openNativeQuestion(_ id: String) {
        guard let item = feed.timeline.conversation().first(where: { $0.id == id }), canOpenNativeQuestion(item) else { return }
        interaction = makeInteraction(command: nil, requestID: id)
    }

    private func makeInteraction(command: String?, commandInfo: HarnessCompletion? = nil, requestID: String? = nil) -> ChatSurfaceInteraction {
        let binding = conversation
        let session = SurfaceSession(id: subject.sessionID) { [services, subject] in
            try await services.surfaceConnection(subject)
        }
        return ChatSurfaceInteraction(command: command, commandInfo: commandInfo, requestID: requestID,
            earlierRequests: Set(feed.timeline.conversation().map(\.id)), session: session) { [weak self] in
            guard let self, self.conversation == binding else { return }
            self.retireInteraction()
        }
    }

    func dismissNativeCommand() {
        if terminalInteraction != nil {
            dismissTerminalCommand()
            return
        }
        guard let current = interaction, nativeInteractions.dismissing != current.id else { return }
        nativeInteractions.dismissing = current.id
        Task { [weak self] in
            defer { if self?.nativeInteractions.dismissing == current.id { self?.nativeInteractions.dismissing = nil } }
            if let self, let block = current.blocks.last, current.session.canInteract {
                // Dismiss the original chooser too; otherwise another command
                // would find a hidden chooser still owning the desktop editor.
                do { try await NativeCommandContext(model: current, block: block, store: self).cancel() }
                catch { if self.interaction === current { self.note(SurfaceCopy.unconfirmed, code: "command") } }
            }
            if self?.interaction === current { self?.retireInteraction() }
        }
    }

    func retireInteraction(pumpQueue: Bool = true) {
        guard let previous = interaction else { return }
        interaction = nil
        previous.retire()
        nativeInteractions.stopping = true
        let binding = conversation
        let stopID = UUID()
        nativeInteractions.stopID = stopID
        let before = nativeInteractions.stopTask
        nativeInteractions.stopTask = Task { [weak self] in
            await before?.value
            await previous.stop()
            guard let self, self.nativeInteractions.stopID == stopID else { return }
            self.nativeInteractions.stopping = false
            self.nativeInteractions.stopTask = nil
            if pumpQueue, self.conversation == binding { self.pump() }
        }
    }

    func updateNativeQuestion() {
        if working { interaction?.turnStarted() }
        if let current = interaction, current.command != nil, current.requestID == nil, current.showingForm,
           let id = nativeInteractions.requests.next(in: feed.timeline.conversation(), available: true, skipping: current.earlierRequests) {
            current.attachRequest(id)
        }
        guard let id = nativeInteractions.requests.next(in: feed.timeline.conversation(),
            available: surfaceAvailable && interaction == nil && terminalInteraction == nil && !nativeInteractions.stopping) else { return }
        interaction = makeInteraction(command: nil, requestID: id)
    }
}
