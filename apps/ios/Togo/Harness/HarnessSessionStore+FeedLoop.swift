import Foundation

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

    /// Waits before trying again, doubling the wait up to the ceiling. False once cancelled.
    func pause(_ delay: inout Duration) async -> Bool {
        do {
            try await Task.sleep(for: delay)
        } catch {
            return false
        }
        delay = min(delay * 2, retryCeiling)
        return !Task.isCancelled
    }

}
