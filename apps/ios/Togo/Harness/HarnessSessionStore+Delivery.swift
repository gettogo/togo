import Foundation
import SwiftUI
import TogoKit

extension HarnessSessionStore {
    // MARK: Composer

    struct QueueFull: Error {}

    struct ConversationChanged: LocalizedError {
        var errorDescription: String? { ChatCopy.deliveryUnconfirmed }
    }

    /// Sends a prompt. `.queue` (the default) holds it on the phone while the agent works on a
    /// harness that cannot queue, and sends it when the turn ends; `.now` goes at once and
    /// throws when the helper refuses it.
    func send(_ text: String, mode: HarnessSendParams.Mode = .queue, images: [String] = []) async throws {
        let binding = conversation
        try await prepareHarnessInput()
        if mode == .now {
            guard phase == .live else { throw HarnessUnavailable() }
            let item = try await services.send(subject, text: text, mode: .now, images: images)
            guard conversation == binding else { throw ConversationChanged() }
            feed.insertSent(item)
            markSent()
            changed()
            return
        }
        var added: UUID?
        changeQueue { added = $0.add(text, images: images) }
        guard added != nil else {
            if queue.isFull { throw QueueFull() }
            return
        }
        await deliverNext()
    }

    /// Stops the whole turn; the helper presses until the session is idle and says whether it got there.
    @discardableResult
    func interrupt() async throws -> HarnessActionResult {
        try await services.interrupt(subject)
    }

    func cancelQueued(_ id: UUID) {
        changeQueue { _ = $0.cancel(id) }
        // A failed prompt holds the ones after it; taking it away lets them go now, rather
        // than wait for a change an idle session may never send.
        pump()
    }

    /// Takes back a prompt the harness holds in its own queue: `stop` aimed at the queued user
    /// item, which the engine drops from the send queue. False when the helper refused.
    @discardableResult
    func cancelQueued(item: HarnessItem) async -> Bool {
        guard let result = try? await run(action: "stop", target: item.id) else { return false }
        return result.result != .failed
    }

    func retryQueued(_ id: UUID) {
        changeQueue { $0.retry(id) }
        pump()
    }

    /// True when a prompt sent now reaches the session: the agent is idle, or the harness holds
    /// prompts itself while it works.
    var canSendNow: Bool {
        phase == .live && feed.isLoaded && interaction == nil && terminalInteraction == nil && !nativeInteractions.stopping && (!working || has(HarnessCapability.queue))
    }

    /// Sends what the queue holds once the session can take it. Only while the feed runs: a
    /// stopped store sends nothing until it is launched again.
    func pump() {
        guard isRunning, pumpTask == nil, !queue.isEmpty else { return }
        let token = UUID()
        pumpToken = token
        pumpTask = Task { [weak self] in
            await self?.deliverNext()
            if self?.pumpToken == token { self?.pumpTask = nil }
        }
    }

    private func deliverNext() async {
        guard let prompt = queue.next(canSend: canSendNow) else { return }
        let binding = conversation
        do {
            let item = try await services.send(subject, text: prompt.text, mode: .queue, images: prompt.images)
            guard conversation == binding else { return }
            // motion: continuity — one transaction: the held row leaves as the delivered prompt arrives in its place.
            withAnimation(Motion.animation(.resize, reduce: Self.reduceMotion)) {
                queue.sent(prompt.id)
                feed.insertSent(item)
                setRows(SubagentThread.mainRows(feed.timeline, resolvedAt: resolvedAt), arrivals: false)
            }
            markSent()
        } catch let error as HelperError where error.conflictReason == "busy" {
            guard conversation == binding else { return }
            // The turn is still going; the next change of the mission tries again.
            queue.requeue(prompt.id)
            return
        } catch is CancellationError {
            guard conversation == binding else { return }
            // Cancellation can race delivery. Preserve the text without replaying it.
            queue.unconfirmed(prompt.id)
            return
        } catch {
            guard conversation == binding else { return }
            changeQueue { $0.failed(prompt.id, message: ServiceErrors.message(error)) }
            return
        }
        await deliverNext()
    }

    /// Changes the phone's own queue in the arrival transaction, so a held prompt's row
    /// reveals and leaves like any other row.
    func changeQueue(_ change: (inout PromptQueue) -> Void) {
        // motion: preventing a jump — a held prompt's row comes and goes while the rows around it make room.
        withAnimation(Motion.animation(.resize, reduce: Self.reduceMotion)) { change(&queue) }
    }
}
