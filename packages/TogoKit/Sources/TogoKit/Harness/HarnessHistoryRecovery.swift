import Foundation

/// Completes a retained reconnect interval before its cursor is acknowledged.
/// Sequence holes from replaced item revisions are never evidence of eviction.
public enum HarnessHistoryRecovery {
    public static func complete(
        _ opened: HarnessOpenResult, after: Int,
        history: @Sendable (Int) async throws -> HarnessPage
    ) async throws -> HarnessOpenResult {
        guard after > 0, let first = opened.items.map(\.seq).min() else { return opened }
        let truncated = opened.coveredAfter.map { $0 > after } ?? (opened.more && first > after)
        guard truncated else { return opened }
        var result = opened
        var byID = Dictionary(opened.items.map { ($0.id, $0) }, uniquingKeysWith: { $0.seq > $1.seq ? $0 : $1 })
        var before = first
        for _ in 0..<HarnessTimeline.limit {
            try Task.checkCancellation()
            let page = try await history(before)
            if let epoch = page.epoch, epoch != opened.epoch { throw HelperError.malformedResponse }
            if let floor = page.discardedThrough {
                result.discardedThrough = max(result.discardedThrough ?? 0, floor)
            }
            let earlier = page.items.filter { $0.seq < before }
            guard let oldest = earlier.map(\.seq).min() else {
                if page.more { throw HelperError.malformedResponse }
                result.coveredAfter = after
                result.items = byID.values.sorted { $0.seq < $1.seq }
                return result
            }
            for item in earlier where item.seq > after && item.seq <= opened.cursor {
                if byID[item.id].map({ $0.seq >= item.seq }) != true { byID[item.id] = item }
            }
            guard byID.count <= HarnessTimeline.limit else { throw HelperError.malformedResponse }
            if oldest <= after || !page.more {
                result.coveredAfter = after
                result.items = byID.values.sorted { $0.seq < $1.seq }
                return result
            }
            before = oldest
        }
        throw HelperError.malformedResponse
    }
}
