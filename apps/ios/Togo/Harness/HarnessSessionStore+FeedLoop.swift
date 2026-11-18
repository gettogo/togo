import Foundation
import TogoKit

extension HarnessSessionStore {
    /// One store's loop. Each pass takes the store afresh and ends once it is gone, so the loop
    /// never keeps a closed tab's store, and its connections, alive.
    static func run(weak store: HarnessSessionStore?) async {
        weak let weakStore = store
        var delay = weakStore?.retryFloor ?? .zero
        var reopenDelay = delay
        while !Task.isCancelled {
            guard let store = weakStore, await store.step(delay: &delay, reopenDelay: &reopenDelay) else { return }
        }
    }

    /// Before the first successful helper request, wait a short flat interval. A connection
    /// that had worked retains exponential backoff. Neither path replays a mutation.
    func pause(_ delay: inout Duration) async -> Bool {
        do {
            let pause = (hasConnected || feed.isLoaded) ? delay : min(ConnectionTiming.retryPause, retryCeiling)
            try await Task.sleep(for: pause)
        } catch {
            return false
        }
        if hasConnected || feed.isLoaded { delay = min(delay * 2, retryCeiling) }
        return !Task.isCancelled
    }

}
