import TogoKit

extension HarnessSessionStore {
    func completeOpen(_ result: HarnessOpenResult) async throws -> HarnessOpenResult {
        let after = timeline.epoch == result.epoch ? feed.openAfter : 0
        let services = services, subject = subject
        return try await HarnessHistoryRecovery.complete(result, after: after) { before in
            try await services.history(subject, before: before)
        }
    }
}
