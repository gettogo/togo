import Foundation
import Synchronization

/// A single-consumer byte queue with backpressure.
///
/// `AsyncStream` buffers without limit or drops data; neither is acceptable for terminal
/// output, where a flood (`cat /dev/urandom`) must slow the producer instead of growing
/// memory or losing bytes. A `write` that starts while `bufferedBytes >= limit` suspends
/// until the consumer drains below the limit, so the buffer holds at most
/// `limit - 1 + ` the largest single write.
///
/// Suspended writers resume in the order they arrived. `finish()` resumes them with
/// `false` and drops their data; bytes already accepted stay readable until drained.
/// A cancelled writer returns `false` without writing; a cancelled consumer gets `nil`.
public final class ByteStream: AsyncSequence, Sendable {
    public typealias Element = Data
    public typealias Failure = Never

    private let limit: Int
    private let state = Mutex(State())

    public init(limit: Int = 4 << 20) {
        precondition(limit > 0, "a zero limit would block every writer forever")
        self.limit = limit
    }

    public var bufferedBytes: Int {
        state.withLock { $0.bufferedBytes }
    }

    /// Returns false once the stream is finished, or if the calling task is cancelled
    /// while waiting for room; the data is not delivered in either case.
    public func write(_ data: Data) async -> Bool {
        let immediate: Bool? = state.withLock { state in
            if state.finished { return false }
            if data.isEmpty { return true }
            guard state.bufferedBytes < limit, state.pendingWrites.isEmpty else { return nil }
            state.deliver(data)
            return true
        }
        if let immediate { return immediate }
        return await waitForRoom(data)
    }

    public func finish() {
        let (writers, consumer) = state.withLock { state in
            state.finished = true
            defer { state.pendingWrites.removeAll() }
            return (state.pendingWrites.map(\.continuation), state.takeConsumer())
        }
        for writer in writers { writer.resume(returning: false) }
        consumer?.resume(returning: nil)
    }

    public func makeAsyncIterator() -> Iterator {
        Iterator(stream: self)
    }

    public struct Iterator: AsyncIteratorProtocol {
        let stream: ByteStream

        public mutating func next() async -> Data? {
            await stream.next()
        }
    }

    private func waitForRoom(_ data: Data) async -> Bool {
        let id = state.withLock { $0.makeID() }
        return await withTaskCancellationHandler {
            await withCheckedContinuation { (continuation: CheckedContinuation<Bool, Never>) in
                let outcome: Bool? = state.withLock { state in
                    if state.finished || Task.isCancelled { return false }
                    // The consumer may have drained between the fast path and this lock.
                    if state.bufferedBytes < limit, state.pendingWrites.isEmpty {
                        state.deliver(data)
                        return true
                    }
                    state.pendingWrites.append(PendingWrite(id: id, data: data, continuation: continuation))
                    return nil
                }
                if let outcome { continuation.resume(returning: outcome) }
            }
        } onCancel: {
            let cancelled = state.withLock { state in
                state.pendingWrites.firstIndex { $0.id == id }.map { state.pendingWrites.remove(at: $0) }
            }
            cancelled?.continuation.resume(returning: false)
        }
    }

    private func next() async -> Data? {
        let (chunk, admitted) = state.withLock { $0.take(below: limit) }
        for writer in admitted { writer.resume(returning: true) }
        if let chunk { return chunk }
        return await waitForData()
    }

    private func waitForData() async -> Data? {
        let id = state.withLock { $0.makeID() }
        return await withTaskCancellationHandler {
            await withCheckedContinuation { (continuation: CheckedContinuation<Data?, Never>) in
                let (outcome, admitted): (Data??, [CheckedContinuation<Bool, Never>]) = state.withLock { state in
                    // Writers may have filled the buffer and queued behind it since next() looked,
                    // so this path must admit them too or they would wait forever.
                    let (chunk, admitted) = state.take(below: limit)
                    if let chunk { return (.some(chunk), admitted) }
                    if state.finished || Task.isCancelled { return (.some(nil), admitted) }
                    precondition(state.consumer == nil, "ByteStream supports exactly one consumer")
                    state.consumer = Consumer(id: id, continuation: continuation)
                    return (.none, admitted)
                }
                for writer in admitted { writer.resume(returning: true) }
                if case .some(let result) = outcome { continuation.resume(returning: result) }
            }
        } onCancel: {
            let cancelled = state.withLock { state in
                state.consumer?.id == id ? state.takeConsumer() : nil
            }
            cancelled?.resume(returning: nil)
        }
    }
}

private struct PendingWrite {
    let id: UInt64
    let data: Data
    let continuation: CheckedContinuation<Bool, Never>
}

private struct Consumer {
    let id: UInt64
    let continuation: CheckedContinuation<Data?, Never>
}

private struct State {
    var chunks: [Data] = []
    var head = 0
    var bufferedBytes = 0
    var finished = false
    var pendingWrites: [PendingWrite] = []
    var consumer: Consumer?
    var nextID: UInt64 = 0

    mutating func makeID() -> UInt64 {
        nextID += 1
        return nextID
    }

    /// Hands data straight to a waiting consumer (whose buffer is then empty) or queues it.
    mutating func deliver(_ data: Data) {
        if let consumer = takeConsumer() {
            consumer.resume(returning: data)
        } else {
            enqueue(data)
        }
    }

    mutating func enqueue(_ data: Data) {
        chunks.append(data)
        bufferedBytes += data.count
    }

    mutating func dequeue() -> Data? {
        guard head < chunks.count else { return nil }
        let chunk = chunks[head]
        chunks[head] = Data()
        head += 1
        bufferedBytes -= chunk.count
        // Reclaim consumed slots once they outnumber live ones, keeping dequeue amortised O(1).
        if head * 2 >= chunks.count {
            chunks.removeFirst(head)
            head = 0
        }
        return chunk
    }

    mutating func takeConsumer() -> CheckedContinuation<Data?, Never>? {
        defer { consumer = nil }
        return consumer?.continuation
    }

    /// Dequeues the next chunk and admits every queued writer that now fits. Both steps
    /// happen under one lock so no drain can leave a writer queued below the limit.
    mutating func take(below limit: Int) -> (Data?, [CheckedContinuation<Bool, Never>]) {
        var chunk = dequeue()
        let admitted = admitWriters(below: limit)
        if chunk == nil { chunk = dequeue() }
        return (chunk, admitted)
    }

    /// Moves queued writes into the buffer, oldest first, while there is room.
    mutating func admitWriters(below limit: Int) -> [CheckedContinuation<Bool, Never>] {
        var admitted: [CheckedContinuation<Bool, Never>] = []
        while bufferedBytes < limit, !pendingWrites.isEmpty {
            let write = pendingWrites.removeFirst()
            enqueue(write.data)
            admitted.append(write.continuation)
        }
        return admitted
    }
}
