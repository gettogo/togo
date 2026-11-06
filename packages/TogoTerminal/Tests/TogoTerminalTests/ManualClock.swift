import Foundation

/// A clock that only moves when the test says so.
final class ManualClock: Clock, @unchecked Sendable {
    struct Instant: InstantProtocol {
        var offset: Duration
        func advanced(by duration: Duration) -> Instant { Instant(offset: offset + duration) }
        func duration(to other: Instant) -> Duration { other.offset - offset }
        static func < (a: Instant, b: Instant) -> Bool { a.offset < b.offset }
    }

    private struct Waiter {
        let id: UInt64
        let deadline: Instant
        let continuation: CheckedContinuation<Void, any Error>
    }

    private let lock = NSLock()
    private var current = Instant(offset: .zero)
    private var waiters: [Waiter] = []
    private var nextID: UInt64 = 0

    var now: Instant { lock.withLock { current } }
    var minimumResolution: Duration { .nanoseconds(1) }

    func sleep(until deadline: Instant, tolerance: Duration?) async throws {
        let id = lock.withLock { () -> UInt64 in nextID += 1; return nextID }
        try await withTaskCancellationHandler {
            try await withCheckedThrowingContinuation { (c: CheckedContinuation<Void, any Error>) in
                let ready = lock.withLock { () -> Bool in
                    if deadline <= current { return true }
                    if Task.isCancelled { return true }
                    waiters.append(Waiter(id: id, deadline: deadline, continuation: c))
                    return false
                }
                if ready {
                    if Task.isCancelled { c.resume(throwing: CancellationError()) } else { c.resume() }
                }
            }
        } onCancel: {
            let w = lock.withLock { () -> Waiter? in
                guard let i = waiters.firstIndex(where: { $0.id == id }) else { return nil }
                return waiters.remove(at: i)
            }
            w?.continuation.resume(throwing: CancellationError())
        }
    }

    /// Waits until exactly `n` sleepers are registered, so a deadline is never computed after an advance.
    func waitForSleepers(_ n: Int) async {
        let end = ContinuousClock.now.advanced(by: .seconds(60))
        while lock.withLock({ waiters.count }) != n && ContinuousClock.now < end { await Task.yield() }
    }

    func advance(by d: Duration) async {
        let due = lock.withLock { () -> [Waiter] in
            current = current.advanced(by: d)
            let due = waiters.filter { $0.deadline <= current }
            waiters.removeAll { $0.deadline <= current }
            return due
        }
        for w in due { w.continuation.resume() }
        for _ in 0..<50 { await Task.yield() }
    }
}

@MainActor
func settles(_ condition: () -> Bool) async -> Bool {
    let end = ContinuousClock.now.advanced(by: .seconds(60))
    while !condition() && ContinuousClock.now < end { await Task.yield() }
    return condition()
}
