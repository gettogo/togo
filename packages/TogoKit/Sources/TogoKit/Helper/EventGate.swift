import Synchronization

/// Filters events against the newest `events.subscribe` snapshot when the consumer
/// reads them, not when they are queued.
///
/// The reader tags each event with the subscription generation it arrived under. A
/// subscribe response starts a new generation; the helper writes that response before
/// any event it does not contain, so every event queued under an older generation is
/// already in the new snapshot, as is any event at or below the snapshot seq. Both are
/// skipped, however long they sat in the buffer.
final class EventGate: Sendable {
    struct Tagged: Sendable {
        let generation: Int
        let envelope: EventEnvelope
    }

    private struct Cursor {
        var generation = 0
        var seq = 0
    }

    private let cursor = Mutex(Cursor())

    /// Called while the subscribe response is routed, before the next frame is read.
    func startGeneration(snapshotSeq: Int) {
        cursor.withLock { $0 = Cursor(generation: $0.generation + 1, seq: snapshotSeq) }
    }

    /// The tagged event, or nil when the current snapshot already contains it.
    func tag(_ envelope: EventEnvelope) -> Tagged? {
        cursor.withLock { cursor in
            envelope.seq > cursor.seq ? Tagged(generation: cursor.generation, envelope: envelope) : nil
        }
    }

    func isCurrent(_ event: Tagged) -> Bool {
        cursor.withLock { $0.generation == event.generation && event.envelope.seq > $0.seq }
    }

    /// The consumer-facing stream: pulls tagged events and skips the stale ones.
    func filtered(_ events: AsyncStream<Tagged>) -> AsyncStream<EventEnvelope> {
        let source = TaggedSource(events.makeAsyncIterator())
        return AsyncStream { [self] in
            while let event = await source.next() {
                if isCurrent(event) { return event.envelope }
            }
            return nil
        }
    }
}

/// Holds the iterator for the unfolding closure. `notifications` has a single
/// consumer, so `next()` is never called concurrently.
private final class TaggedSource: @unchecked Sendable {
    private var iterator: AsyncStream<EventGate.Tagged>.Iterator

    init(_ iterator: AsyncStream<EventGate.Tagged>.Iterator) {
        self.iterator = iterator
    }

    func next() async -> EventGate.Tagged? {
        await iterator.next()
    }
}
