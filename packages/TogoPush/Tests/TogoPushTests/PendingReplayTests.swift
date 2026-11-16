import Foundation
import Testing
@testable import TogoPush

extension NotificationServiceCoreTests {
    @Test func clearBeforeFirstAlertKeepsAClosedReplayBarrier() throws {
        pair(Self.hostID, name: "studio")
        pending.close("r_aaaaaaaaaa", hostId: Self.hostID)
        let result = core.process(try generic())
        expectQuiet(result.content, "clear arrived before its alert")
        #expect(!result.pendingChanged)
        #expect(pending.request("r_aaaaaaaaaa")?.answered == .elsewhere)
        #expect(!pending.claim("r_aaaaaaaaaa", hostId: Self.hostID))
        #expect(RequestLifetime.waiting(pending.all(), now: Self.now).isEmpty)
    }

    @Test func newPushPrunesWithoutOpeningTheApp() throws {
        pair(Self.hostID, name: "studio")
        var old = PendingRequest(
            requestId: "r_bbbbbbbbbb", hostId: Self.hostID, sessionId: nil, bib: nil, agent: nil, host: nil,
            title: "Old question", subject: nil, options: [],
            receivedAt: Self.now.addingTimeInterval(-PendingStore.retention - 1), answered: nil
        )
        pending.upsert(old)
        old.requestId = "r_cccccccccc"
        old.receivedAt = Self.now.addingTimeInterval(-RequestLifetime.seconds - 1)
        pending.upsert(old)
        let result = core.process(try generic())
        #expect(result.pendingChanged)
        #expect(pending.request("r_bbbbbbbbbb") == nil)
        #expect(pending.request("r_cccccccccc") != nil, "briefly retain an expired record for accurate feedback")
        #expect(RequestLifetime.waiting(pending.all(), now: Self.now).map(\.requestId) == ["r_aaaaaaaaaa"])
    }
}

extension StoresTests {
    @Test func clearRetainsTheOriginalAnswerAndExtendsOnlyClosedRetention() throws {
        let dir = directory()
        let store = PendingStore(directory: dir, clock: { Self.now })
        store.upsert(request("r_aaaaaaaaaa", receivedAt: Self.now.addingTimeInterval(-1190)))
        store.mark("r_aaaaaaaaaa", .denied)
        store.close("r_aaaaaaaaaa")
        let reopened = PendingStore(directory: dir, clock: { Self.now.addingTimeInterval(30) })
        reopened.prune()
        #expect(reopened.request("r_aaaaaaaaaa")?.answered == .denied)
        #expect(reopened.request("r_aaaaaaaaaa")?.receivedAt == Self.now)
        #expect(!reopened.claim("r_aaaaaaaaaa", hostId: Self.hostID))
    }

    @Test(arguments: [599.0, 600.0, 3600.0])
    func claimsRespectTheStoredRequestLifetime(_ age: TimeInterval) {
        let store = PendingStore(directory: directory(), clock: { Self.now })
        store.upsert(request("r_aaaaaaaaaa", receivedAt: Self.now.addingTimeInterval(-age)))
        #expect(store.claim("r_aaaaaaaaaa", hostId: Self.hostID) == (age < RequestLifetime.seconds))
    }

    @Test func expiredQuestionsGetNoChoiceCategories() {
        let options = [PushOption(n: 1, label: "One", role: "choice")]
        let stale = request("r_aaaaaaaaaa", receivedAt: Self.now.addingTimeInterval(-600), options: options)
        let fresh = request("r_bbbbbbbbbb", receivedAt: Self.now.addingTimeInterval(-599), options: options)
        let categories = NotificationCategorySet.choices([stale, fresh], approveWithoutUnlock: false, now: Self.now)
        #expect(categories.map(\.identifier) == [NotificationCategoryID.choice(fresh.requestId)])
    }
}
