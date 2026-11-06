import Foundation

/// Hands every machine snapshot to each window's store. A store that subscribes late, such
/// as a window opened after launch, first gets the latest snapshot of each machine, so it
/// never waits for the next change to show what the others already show.
@MainActor final class SnapshotHub {
    private var latest: [HostID: HostViewSnapshot] = [:]
    /// Machines in the order they first reported, so a late subscriber sees them in that order.
    private var order: [HostID] = []
    private var subscribers: [UUID: AsyncStream<HostViewSnapshot>.Continuation] = [:]
    private var finished = false

    /// A new stream on every call. It ends when the subscriber stops iterating or the hub finishes.
    func stream() -> AsyncStream<HostViewSnapshot> {
        let (stream, continuation) = AsyncStream.makeStream(of: HostViewSnapshot.self)
        for id in order {
            if let snapshot = latest[id] { continuation.yield(snapshot) }
        }
        guard !finished else {
            continuation.finish()
            return stream
        }
        let key = UUID()
        subscribers[key] = continuation
        continuation.onTermination = { [weak self] _ in
            Task { @MainActor in self?.subscribers[key] = nil }
        }
        return stream
    }

    func publish(_ snapshot: HostViewSnapshot) {
        if latest.updateValue(snapshot, forKey: snapshot.hostID) == nil {
            order.append(snapshot.hostID)
        }
        for continuation in subscribers.values {
            continuation.yield(snapshot)
        }
    }

    /// A machine that is gone, or starting over with new details, is not replayed to new subscribers.
    func forget(_ id: HostID) {
        latest[id] = nil
        order.removeAll { $0 == id }
    }

    func finish() {
        finished = true
        for continuation in subscribers.values {
            continuation.finish()
        }
        subscribers = [:]
    }
}
