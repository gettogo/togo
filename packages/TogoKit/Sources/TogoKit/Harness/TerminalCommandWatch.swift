import Foundation

/// Correlates a late menu with one submitted operation. Expiry only ends
/// observation; it never turns an unconfirmed submission into success.
public struct TerminalCommandWatch: Sendable {
    private struct Pending: Sendable {
        let id: UUID
        let conversation: UUID
        let epoch: String
        var expires: ContinuousClock.Instant
        var submitted = false
        var ready = false
    }

    private var pending: Pending?

    public init() {}

    public mutating func begin(requestId: UUID, conversation: UUID, epoch: String,
                               now: ContinuousClock.Instant = .now) {
        pending = Pending(id: requestId, conversation: conversation, epoch: epoch, expires: now + .seconds(60))
    }

    /// A notification may precede the receipt. Keep it until the receipt
    /// confirms submission, or discard it when the operation resolves otherwise.
    public mutating func finish(requestId: UUID, submitted: Bool, now: ContinuousClock.Instant = .now) {
        guard pending?.id == requestId else { return }
        guard submitted else { pending = nil; return }
        pending?.submitted = true
        pending?.expires = now + .seconds(60)
    }

    @discardableResult public mutating func receive(_ note: HarnessNotification, sessionId: String,
        conversation: UUID, epoch: String?, now: ContinuousClock.Instant = .now) -> Bool {
        guard let held = pending, note.kind == .menu, note.sessionId == sessionId,
              note.epoch == epoch, held.epoch == epoch, held.conversation == conversation,
              note.requestId.flatMap(UUID.init(uuidString:)) == held.id, now < held.expires,
              !held.ready else { return false }
        pending?.ready = true
        return true
    }

    public func ready(conversation: UUID, epoch: String?, now: ContinuousClock.Instant = .now) -> UUID? {
        guard let held = pending, held.submitted, held.ready, held.conversation == conversation,
              held.epoch == epoch, now < held.expires else { return nil }
        return held.id
    }

    public func observing(conversation: UUID, epoch: String?, now: ContinuousClock.Instant = .now) -> Bool {
        guard let held = pending else { return false }
        return held.conversation == conversation && held.epoch == epoch && now < held.expires
    }

    @discardableResult public mutating func claim(requestId: UUID, conversation: UUID, epoch: String?,
                                                now: ContinuousClock.Instant = .now) -> Bool {
        guard ready(conversation: conversation, epoch: epoch, now: now) == requestId else { return false }
        pending = nil
        return true
    }

    public mutating func cancel() { pending = nil }
}
