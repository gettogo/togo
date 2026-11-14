import TogoKit

extension HarnessSessionStore {
    func completeOpen(_ result: HarnessOpenResult) async throws -> HarnessOpenResult {
        let after = timeline.epoch == result.epoch ? feed.openAfter : 0
        let services = services, subject = subject
        return try await HarnessHistoryRecovery.complete(result, after: after) { before in
            try await services.history(subject, before: before)
        }
    }

    // MARK: Older items

    func loadOlder() async {
        guard has(HarnessCapability.history), feed.timeline.hasOlder, !loadingOlder,
              let before = feed.timeline.oldestSeq else { return }
        let epoch = feed.timeline.epoch
        let binding = conversation
        loadingOlder = true
        defer { if conversation == binding { loadingOlder = false } }
        guard let page = try? await services.history(subject, before: before) else { return }
        // The session was opened again meanwhile (a reset, a new epoch): the page belongs to a
        // conversation that is gone, and its items must not land above the new one.
        guard conversation == binding, feed.timeline.epoch == epoch, page.epoch == nil || page.epoch == epoch else { return }
        feed.prepend(page)
        // Rows from "Earlier" land without animation; the scroll keeps its place.
        setRows(SubagentThread.mainRows(feed.timeline, resolvedAt: resolvedAt), arrivals: false)
    }
}
