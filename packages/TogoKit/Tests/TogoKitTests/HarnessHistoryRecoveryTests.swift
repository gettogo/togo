import Testing
@testable import TogoKit

struct HarnessHistoryRecoveryTests {
    private typealias S = HarnessSamples

    private func items(_ range: ClosedRange<Int>) -> [HarnessItem] {
        range.map { S.user("u\($0)", seq: $0) }
    }

    private func window(_ range: ClosedRange<Int>, cursor: Int, covered: Int?, floor: Int? = 0) -> HarnessOpenResult {
        var result = S.open(cursor: cursor, items: items(range), more: true)
        result.coveredAfter = covered
        result.discardedThrough = floor
        return result
    }

    @Test func recoversAnOmittedMiddleBeforeMovingTheCursor() async throws {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 10, items: items(1...10)))
        let script = Pages([HarnessPage(items: items(1...50), more: false, epoch: "e1")])
        let recovered = try await HarnessHistoryRecovery.complete(window(51...250, cursor: 250, covered: 50), after: 10) {
            try await script.page($0)
        }
        timeline.load(recovered)
        #expect(timeline.items.map(\.seq) == Array(1...250))
        #expect(timeline.cursor == 250)
        #expect(await script.before == [51])
        #expect(!timeline.historyUnavailable)
    }

    @Test func byteWindowsCanNeedSeveralPages() async throws {
        let script = Pages([
            HarnessPage(items: items(4...7), more: true, epoch: "e1"),
            HarnessPage(items: items(1...3), more: false, epoch: "e1"),
        ])
        let recovered = try await HarnessHistoryRecovery.complete(window(8...9, cursor: 9, covered: 7), after: 2) {
            try await script.page($0)
        }
        #expect(recovered.items.map(\.seq) == Array(3...9))
        #expect(await script.before == [8, 4])
    }

    @Test func replacementGapsDoNotMeanLostMessages() async throws {
        let script = Pages([])
        let recovered = try await HarnessHistoryRecovery.complete(window(101...101, cursor: 101, covered: 10), after: 10) {
            try await script.page($0)
        }
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 10, items: items(1...10)))
        timeline.load(recovered)
        #expect(await script.before.isEmpty)
        #expect(!timeline.historyUnavailable)
        #expect(timeline.cursor == 101)
    }

    @Test func ringEvictionIsVisibleInsteadOfInventingACompleteInterval() async throws {
        let script = Pages([HarnessPage(items: items(31...50), more: false, epoch: "e1", discardedThrough: 30)])
        let recovered = try await HarnessHistoryRecovery.complete(window(51...250, cursor: 250, covered: 50), after: 10) {
            try await script.page($0)
        }
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 10, items: items(1...10)))
        timeline.load(recovered)
        #expect(timeline.historyUnavailable)
        #expect(timeline.items.map(\.seq) == Array(1...10) + Array(31...250))
    }

    @Test func olderHelpersCanStillRecoverThroughHistory() async throws {
        let script = Pages([HarnessPage(items: items(1...50), more: false)])
        let recovered = try await HarnessHistoryRecovery.complete(window(51...250, cursor: 250, covered: nil, floor: nil), after: 10) {
            try await script.page($0)
        }
        #expect(recovered.items.map(\.seq) == Array(11...250))
    }

    @Test func changedEpochAndNonprogressingPagesCannotBeAcknowledged() async {
        for page in [HarnessPage(items: items(1...50), more: false, epoch: "e2"), HarnessPage(items: [], more: true)] {
            let script = Pages([page])
            await #expect(throws: HelperError.malformedResponse) {
                try await HarnessHistoryRecovery.complete(window(51...250, cursor: 250, covered: 50), after: 10) {
                    try await script.page($0)
                }
            }
        }
    }

    @Test func initialAndEmptyOpensNeedNoCatchUpRequests() async throws {
        let script = Pages([])
        _ = try await HarnessHistoryRecovery.complete(window(51...250, cursor: 250, covered: 50), after: 0) {
            try await script.page($0)
        }
        _ = try await HarnessHistoryRecovery.complete(S.open(cursor: 10, items: []), after: 10) {
            try await script.page($0)
        }
        #expect(await script.before.isEmpty)
    }

    @Test func overlappingLoadsKeepANewerSendResponse() {
        var timeline = HarnessTimeline()
        timeline.load(S.open(cursor: 10, items: items(1...10)))
        timeline.insertSent(S.reply("ahead", seq: 100, turn: nil, text: "newer"))
        timeline.load(S.open(cursor: 20, items: [S.reply("ahead", seq: 20, turn: nil, text: "older")]))
        #expect(timeline.item("ahead")?.seq == 100)
        #expect(timeline.cursor == 20)
    }

    private actor Pages {
        var before: [Int] = []
        private var pages: [HarnessPage]
        init(_ pages: [HarnessPage]) { self.pages = pages }
        func page(_ before: Int) throws -> HarnessPage {
            self.before.append(before)
            guard !pages.isEmpty else { throw HelperError.malformedResponse }
            return pages.removeFirst()
        }
    }
}
