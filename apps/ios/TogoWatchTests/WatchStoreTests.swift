import Foundation
import Synchronization
import Testing
import TogoPush
@testable import TogoWatch

private final class FakeSender: AnswerSending {
    let sent = Mutex<[(requestId: String, decision: String, hostId: String)]>([])
    let fails = Mutex(false)

    func send(requestId: String, decision: String, pairing: Pairing) async throws {
        if fails.withLock({ $0 }) { throw AnswerError.offline }
        sent.withLock { $0.append((requestId, decision, pairing.hostId)) }
    }

    var decisions: [String] { sent.withLock { $0.map(\.decision) } }
}

private final class Clock: Sendable {
    let now = Mutex(Date(timeIntervalSinceReferenceDate: 815_000_000))
    func advance(_ seconds: TimeInterval) { now.withLock { $0 = $0.addingTimeInterval(seconds) } }
    var date: Date { now.withLock { $0 } }
}

@MainActor struct WatchStoreTests {
    private let directory = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
    private let sender = FakeSender()
    private let clock = Clock()

    private var pending: PendingStore { PendingStore(directory: directory, clock: { [clock] in clock.date }) }

    private func store() -> WatchStore {
        let pairings = PairingStore(directory: directory)
        pairings.put(Pairing(
            hostId: "h_studio", hostKey: "key", relayURL: URL(string: "https://relay.example")!, machineName: "Studio",
            appHostID: nil, pairedAt: clock.date, tokenHash: "", liveActivityCommand: false
        ))
        let clock = self.clock
        return WatchStore(dependencies: .init(
            pending: pending, pairings: pairings, sender: sender, clock: { clock.date }
        ))
    }

    private static let both = [
        PushOption(n: 1, label: "Yes", role: "approve"),
        PushOption(n: 2, label: "Yes, and don't ask again", role: "approve_always"),
        PushOption(n: 3, label: "No", role: "deny"),
    ]

    private func push(
        _ kind: PushPayload.Kind, _ requestId: String, options: [PushOption] = both, sentAgo: TimeInterval = 1
    ) -> PushPayload {
        PushPayload(
            kind: kind, sessionId: "s_1", bib: "04", agent: "claude", host: "studio", title: "Run the tests?",
            body: "", requestId: requestId, options: kind == .needsYou ? options : nil,
            subject: kind == .needsYou ? PushSubject(kind: "command", text: "npm test") : nil,
            ts: Int64((clock.date.timeIntervalSince1970 - sentAgo) * 1000)
        )
    }

    private func context(closed: [String], at: Date) -> WatchContext {
        WatchContext(sessions: [], closedRequestIds: closed, at: at)
    }

    @Test func ingestNeedsYouAddsRequest() {
        let store = store()
        store.ingest(push(.needsYou, "r_1"), hostId: "h_studio")
        #expect(store.requests.map(\.requestId) == ["r_1"])
        let request = store.requests[0]
        #expect(request.hostId == "h_studio")
        #expect(request.bib == "04")
        #expect(request.title == "Run the tests?")
        #expect(request.subject?.text == "npm test")
        // Kept where the complication and the notification actions read it.
        #expect(pending.request("r_1") != nil)

        // Too old to answer, or other kinds: nothing to add.
        store.ingest(push(.needsYou, "r_old", sentAgo: 700), hostId: "h_studio")
        store.ingest(push(.done, "r_done"), hostId: "h_studio")
        #expect(store.requests.map(\.requestId) == ["r_1"])
    }

    @Test func clearRemovesRequest() {
        let store = store()
        store.ingest(push(.needsYou, "r_1"), hostId: "h_studio")
        store.ingest(push(.clear, "r_1"), hostId: "h_studio")
        #expect(store.requests.isEmpty)
        // A replay of the request after its clear does not bring it back.
        store.ingest(push(.needsYou, "r_1"), hostId: "h_studio")
        #expect(store.requests.isEmpty)
    }

    @Test func staleContextDoesNotResurrect() async {
        let store = store()
        store.ingest(push(.needsYou, "r_answered"), hostId: "h_studio")
        await store.answer("r_answered", "deny")
        // The phone had not heard of the answer yet: its context does not name the request.
        store.apply(context: context(closed: [], at: clock.date.addingTimeInterval(1)))
        #expect(store.requests.isEmpty)

        // Requests age out after ten minutes whatever the phone says.
        store.ingest(push(.needsYou, "r_late"), hostId: "h_studio")
        clock.advance(601)
        store.apply(context: context(closed: [], at: clock.date))
        #expect(store.requests.isEmpty)
    }

    /// The phone can miss a request: it was off, out of range, or locked before first unlock.
    /// Its later context must not close what it never saw.
    @Test func requestThePhoneNeverSawStaysOpen() {
        let store = store()
        store.ingest(push(.needsYou, "r_watch_only"), hostId: "h_studio")
        clock.advance(5)
        store.apply(context: context(closed: ["r_other"], at: clock.date))
        #expect(store.requests.map(\.requestId) == ["r_watch_only"])
        #expect(store.state(of: "r_watch_only") == .open)

        store.apply(context: context(closed: ["r_watch_only"], at: clock.date.addingTimeInterval(1)))
        #expect(store.requests.isEmpty)
        #expect(store.state(of: "r_watch_only") == .answeredElsewhere)
        // A replay of its push after the phone closed it stays closed.
        store.ingest(push(.needsYou, "r_watch_only"), hostId: "h_studio")
        #expect(store.requests.isEmpty)
    }

    @Test func answerMarksAndCollapses() async {
        let store = store()
        store.ingest(push(.needsYou, "r_1"), hostId: "h_studio")
        store.ingest(push(.needsYou, "r_2"), hostId: "h_studio")
        await store.answer("r_1", "approve")
        #expect(sender.decisions == ["approve"])
        #expect(store.outcome("r_1") == .approved)
        #expect(store.requests.map(\.requestId) == ["r_2"])
        #expect(pending.request("r_1")?.answered == .approved)

        // One answer per request, whatever is tapped next.
        await store.answer("r_1", "deny")
        #expect(sender.decisions == ["approve"])

        // A failed send can be tried again.
        sender.fails.withLock { $0 = true }
        await store.answer("r_2", "deny")
        #expect(store.outcome("r_2") == .failed)
        #expect(store.requests.map(\.requestId) == ["r_2"])
        sender.fails.withLock { $0 = false }
        await store.answer("r_2", "deny")
        #expect(store.outcome("r_2") == .denied)
        #expect(sender.decisions == ["approve", "deny"])
        #expect(store.requests.isEmpty)

        // Never an option, so never approve_always.
        store.ingest(push(.needsYou, "r_3"), hostId: "h_studio")
        await store.answer("r_3", "option:2")
        #expect(sender.decisions == ["approve", "deny"])
    }

    @Test func approveDisabledWithoutApproveRole() async {
        let store = store()
        store.ingest(push(.needsYou, "r_1", options: [PushOption(n: 1, label: "No", role: "deny")]), hostId: "h_studio")
        store.ingest(push(.needsYou, "r_2", options: []), hostId: "h_studio")
        store.ingest(push(.needsYou, "r_3"), hostId: "h_studio")
        let byId = Dictionary(uniqueKeysWithValues: store.requests.map { ($0.requestId, $0) })
        #expect(!WatchStore.canApprove(byId["r_1"]!))
        #expect(!WatchStore.canApprove(byId["r_2"]!))
        #expect(WatchStore.canApprove(byId["r_3"]!))

        await store.answer("r_1", "approve")
        #expect(sender.decisions.isEmpty)
        #expect(store.outcome("r_1") == nil)
        await store.answer("r_1", "deny")
        #expect(sender.decisions == ["deny"])
    }

    @Test func sessionsAsOfShownWhenOld() {
        let store = store()
        #expect(store.asOf == nil)
        store.apply(context: context(closed: [], at: clock.date))
        #expect(store.sessionsAsOf == clock.date)
        #expect(store.asOf == nil)
        clock.advance(299)
        #expect(store.asOf == nil)
        clock.advance(2)
        #expect(store.asOf == clock.date.addingTimeInterval(-301))

        // An older context arriving late changes nothing.
        let newest = store.sessionsAsOf
        store.apply(context: WatchContext(
            sessions: [WatchSession(hostName: "Studio", bib: "04", agentName: "Claude", status: "working", activity: nil)],
            closedRequestIds: [], at: clock.date.addingTimeInterval(-600)
        ))
        #expect(store.sessionsAsOf == newest)
        #expect(store.sessions.isEmpty)

        let midnight: TimeInterval = 1_800_000_000 - 1_800_000_000.truncatingRemainder(dividingBy: 86_400)
        let noon = Date(timeIntervalSince1970: midnight + 12 * 3600 + 4 * 60)
        let text = WatchCopy.asOf(noon, locale: Locale(identifier: "en_GB"), timeZone: TimeZone(identifier: "UTC")!)
        #expect(text == "As of 12:04")
    }

    @Test func notificationActionAnswersOnce() async {
        let store = store()
        // A mirrored phone notification can arrive before, or without, the Watch's own push.
        await store.answerFromNotification(hostId: "h_studio", requestId: "r_9", decision: "approve")
        await store.answerFromNotification(hostId: "h_studio", requestId: "r_9", decision: "approve")
        await store.answerFromNotification(hostId: "h_unknown", requestId: "r_10", decision: "deny")
        await store.answerFromNotification(hostId: "h_studio", requestId: "r_11", decision: "option:2")
        #expect(sender.decisions == ["approve"])
        #expect(pending.request("r_9")?.answered == .approved)
        #expect(pending.request("r_10")?.answered == nil)
    }

    @Test func detailNotActionableAfterClearOrExpiry() async {
        let store = store()
        store.ingest(push(.needsYou, "r_cleared"), hostId: "h_studio")
        store.ingest(push(.needsYou, "r_late"), hostId: "h_studio")
        store.ingest(push(.needsYou, "r_other"), hostId: "h_studio")
        #expect(store.state(of: "r_cleared") == .open)

        // Closed at the desk or on the phone while the screen was open.
        store.ingest(push(.clear, "r_cleared"), hostId: "h_studio")
        #expect(store.state(of: "r_cleared") == .answeredElsewhere)
        await store.answer("r_cleared", "deny")
        #expect(sender.decisions.isEmpty)

        // Answered by another process on this Watch, such as a notification action.
        _ = pending.claim("r_other", hostId: "h_studio")
        pending.mark("r_other", .approved)
        #expect(store.state(of: "r_other") == .answered)
        await store.answer("r_other", "deny")
        #expect(sender.decisions.isEmpty)

        // Ten minutes on, nothing has pruned it yet: the screen says so, and a tap sends nothing.
        clock.advance(599)
        #expect(store.state(of: "r_late") == .open)
        clock.advance(2)
        #expect(store.state(of: "r_late") == .expired)
        await store.answer("r_late", "approve")
        await store.answerFromNotification(hostId: "h_studio", requestId: "r_late", decision: "deny")
        #expect(sender.decisions.isEmpty)
        #expect(store.state(of: "r_late") == .expired)
        #expect(pending.request("r_late")?.answered == nil)
    }

    @Test func approvedOnlyAfterTheRelayAccepts() async {
        let store = store()
        store.ingest(push(.needsYou, "r_1"), hostId: "h_studio")
        sender.fails.withLock { $0 = true }
        await store.answer("r_1", "approve")
        #expect(store.state(of: "r_1") == .failed)
        #expect(pending.request("r_1")?.answered == nil)
        sender.fails.withLock { $0 = false }
        await store.answer("r_1", "approve")
        #expect(store.state(of: "r_1") == .approved)
    }

    /// The app stopped mid-send (watchOS ended it in the background, or it crashed): its claim
    /// must not leave the request on "Sending", refusing every tap, until it ages out.
    @Test func claimLeftByAStoppedAppIsGivenBackAtLaunch() async {
        let first = store()
        first.ingest(push(.needsYou, "r_1"), hostId: "h_studio")
        #expect(pending.claim("r_1", hostId: "h_studio"))
        // A notification action for a request this Watch never got a push for.
        #expect(pending.claim("r_mirrored", hostId: "h_studio"))

        let relaunched = store()
        #expect(relaunched.state(of: "r_1") == .open)
        #expect(relaunched.requests.map(\.requestId) == ["r_1"])
        // The record the claim made holds nothing to show: it goes rather than open blank.
        #expect(pending.request("r_mirrored") == nil)
        await relaunched.answer("r_1", "approve")
        #expect(sender.decisions == ["approve"])
        #expect(relaunched.state(of: "r_1") == .approved)
    }

    /// A failed send for a request the Watch never got a push for must not leave a blank
    /// request on the Waiting page.
    @Test func failedNotificationAnswerLeavesNoBlankRequest() async {
        let store = store()
        sender.fails.withLock { $0 = true }
        let notice = await store.answerFromNotification(
            hostId: "h_studio", requestId: "r_mirrored", decision: "deny", deliveredAt: clock.date
        )
        #expect(notice == store.failureNotice("r_mirrored"))
        #expect(notice != nil)
        #expect(pending.request("r_mirrored") == nil)
        #expect(store.requests.isEmpty)

        // It can be answered again.
        sender.fails.withLock { $0 = false }
        let again = await store.answerFromNotification(
            hostId: "h_studio", requestId: "r_mirrored", decision: "deny", deliveredAt: clock.date
        )
        #expect(again == nil)
        #expect(sender.decisions == ["deny"])
    }

    /// Past ten minutes the store has pruned the request, so only the notification's own date
    /// tells that it ran out. The tap sends nothing and says it expired, not that it went.
    @Test func notificationAnswerAfterPruneSaysExpired() async {
        let store = store()
        let delivered = clock.date
        store.ingest(push(.needsYou, "r_1"), hostId: "h_studio")
        clock.advance(601)
        store.apply(context: context(closed: [], at: clock.date))
        #expect(pending.request("r_1") == nil)

        let notice = await store.answerFromNotification(
            hostId: "h_studio", requestId: "r_1", decision: "approve", deliveredAt: delivered
        )
        #expect(notice == WatchCopy.expired)
        #expect(sender.decisions.isEmpty)
        #expect(pending.request("r_1") == nil)

        // A recent notification for a request the Watch never got still goes.
        let fresh = await store.answerFromNotification(
            hostId: "h_studio", requestId: "r_2", decision: "approve", deliveredAt: clock.date.addingTimeInterval(-30)
        )
        #expect(fresh == nil)
        #expect(sender.decisions == ["approve"])
    }

    /// A send that failed earlier, then the request ran out: the notice says it expired, not
    /// that it could not send.
    @Test func expiredAfterAFailureSaysExpired() async {
        let store = store()
        store.ingest(push(.needsYou, "r_1"), hostId: "h_studio")
        sender.fails.withLock { $0 = true }
        await store.answer("r_1", "approve")
        #expect(store.state(of: "r_1") == .failed)
        sender.fails.withLock { $0 = false }
        clock.advance(601)
        let notice = await store.answerFromNotification(hostId: "h_studio", requestId: "r_1", decision: "approve")
        #expect(notice == WatchCopy.expired)
        #expect(store.failureNotice("r_1") == nil)
        #expect(sender.decisions.isEmpty)
    }

    /// Before the phone sends this Watch its pairings, or for a machine added since, an answer
    /// cannot go anywhere. It says so instead of doing nothing.
    @Test func answerWithoutPairingSaysWhy() async {
        let store = store()
        store.ingest(push(.needsYou, "r_new_host"), hostId: "h_new")
        await store.answer("r_new_host", "approve")
        #expect(sender.decisions.isEmpty)
        #expect(store.outcome("r_new_host") == .notSetUp)
        #expect(store.state(of: "r_new_host") == .notSetUp)
        #expect(store.state(of: "r_new_host").isActionable, "it can go once the pairing arrives")
        #expect(pending.request("r_new_host")?.answered == nil)
        #expect(store.failureNotice("r_new_host") == WatchCopy.notSetUp(machine: "studio"))

        await store.answerFromNotification(hostId: "h_unknown", requestId: "r_mirrored", decision: "approve")
        #expect(store.outcome("r_mirrored") == .notSetUp)
        #expect(store.failureNotice("r_mirrored") == WatchCopy.notSetUp(machine: nil))
        #expect(WatchCopy.notSetUp(machine: "Studio") == "Not set up for Studio. Open Togo on iPhone.")
    }
}
