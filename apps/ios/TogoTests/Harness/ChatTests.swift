import Foundation
import Testing
import TogoKit
@testable import Togo

/// The chat's store against scripted harness services, the phone's prompt queue and the work
/// line's words.
@MainActor
struct ChatTests {
    static let subject = HarnessSubject(hostID: UUID(), sessionID: "s_abcdefghij", capabilities: [Capability.harness])

    static let claude = HarnessDescriptor(
        adapter: .init(id: "claude", name: "Claude Code", harnessVersion: "2.1.319", revision: 1),
        capabilities: [HarnessCapability.chat, HarnessCapability.anchors, HarnessCapability.interrupt]
    )

    private func store(_ services: FakeHarnessServices, subject: HarnessSubject = ChatTests.subject) -> HarnessSessionStore {
        HarnessSessionStore(subject: subject, services: services, retry: (.milliseconds(5), .milliseconds(20)))
    }

    // MARK: Store

    @Test func openFixtureShowsPromptWorkAndReply() async throws {
        let services = FakeHarnessServices(descriptor: Self.claude)
        services.feeds = [.init(elements: [.opened(try HarnessFixtures.open())], end: .hold)]
        let chat = store(services)
        chat.launch()
        defer { chat.stop() }
        try await until { chat.rows.count == 3 }
        #expect(chat.phase == .live)
        #expect(chat.layer == .chat)
        #expect(chat.shownLayer == .chat)
        guard case .prompt = chat.rows[0], case .work(_, let summary, _) = chat.rows[1], case .reply = chat.rows[2] else {
            Issue.record("unexpected rows \(chat.rows.map(\.id))")
            return
        }
        #expect(ChatCopy.workLine(summary, now: .now) == "Worked 1m 16s  2 steps  1 file changed")
        #expect(ChatCopy.spokenWork(summary, now: .now, expanded: false).hasSuffix("2 steps, 1 file changed, collapsed"))
    }

    @Test func notificationExamplesApplyThenReopenFromTheStart() async throws {
        let services = FakeHarnessServices(descriptor: Self.claude)
        let notes = try HarnessFixtures.notes()
        var second = try HarnessFixtures.open()
        second.epoch = "e2"
        services.feeds = [
            .init(elements: [.opened(try HarnessFixtures.open())] + notes.map(HarnessFeed.note), end: .hold),
            .init(elements: [.opened(second)], end: .hold),
        ]
        let chat = store(services)
        chat.launch()
        defer { chat.stop() }
        try await until { services.opens.count == 2 && chat.timeline.epoch == "e2" }
        #expect(services.opens == [0, 0])
        #expect(chat.rows.count == 3)
    }

    @Test func dropReopensWithTheCursor() async throws {
        let services = FakeHarnessServices(descriptor: Self.claude)
        services.feeds = [
            .init(elements: [.opened(try HarnessFixtures.open())], end: .fail),
            .init(elements: [], end: .hold),
        ]
        let chat = store(services)
        chat.launch()
        defer { chat.stop() }
        try await until { services.opens.count == 2 }
        #expect(services.opens == [0, 7])
        #expect(chat.rows.count == 3)
    }

    @Test func sessionWithoutAdapterStaysTerminalOnly() async throws {
        let services = FakeHarnessServices(descriptor: HarnessDescriptor())
        let chat = store(services)
        await chat.start()
        #expect(chat.phase == .terminalOnly)
        #expect(chat.shownLayer == .terminal)
        #expect(!chat.chatAvailable)
        #expect(services.opens.isEmpty)
    }

    @Test func helperWithoutHarnessIsNeverAsked() async {
        let services = FakeHarnessServices(descriptor: Self.claude)
        let plain = HarnessSubject(hostID: UUID(), sessionID: "s1", capabilities: [])
        let chat = store(services, subject: plain)
        await chat.start()
        #expect(chat.phase == .terminalOnly)
        #expect(services.describes == 0)
    }

    @Test func refusedFeedFallsBackToTheTerminalWithALine() async throws {
        let services = FakeHarnessServices(descriptor: Self.claude)
        services.feeds = [.init(elements: [], end: .refuse)]
        let chat = store(services)
        await chat.start()
        #expect(chat.shownLayer == .terminal)
        #expect(chat.fallback == ChatCopy.fallback)
    }

    @Test func chosenLayerIsKept() async throws {
        let services = FakeHarnessServices(descriptor: Self.claude)
        services.feeds = [.init(elements: [.opened(try HarnessFixtures.open())], end: .hold)]
        let chat = store(services)
        chat.layer = .terminal
        chat.launch()
        defer { chat.stop() }
        try await until { chat.phase == .live }
        #expect(chat.shownLayer == .terminal)
    }

    @Test func sendWhileIdleGoesAtOnceAndShowsTheItem() async throws {
        let services = FakeHarnessServices(descriptor: Self.claude)
        services.feeds = [.init(elements: [.opened(try HarnessFixtures.open())], end: .hold)]
        let chat = store(services)
        chat.launch()
        defer { chat.stop() }
        try await until { chat.phase == .live }
        try await chat.send("and the other test")
        #expect(services.sent.map(\.text) == ["and the other test"])
        #expect(chat.queue.isEmpty)
        #expect(chat.rows.count == 4)
    }

    @Test func sendWhileWorkingWaitsForTheTurnToEnd() async throws {
        let services = FakeHarnessServices(descriptor: Self.claude)
        var open = try HarnessFixtures.open()
        open.mission.summary.working = true
        let stream = AsyncThrowingStream<HarnessFeed, any Error>.makeStream()
        services.feeds = [.init(stream: stream.stream)]
        stream.continuation.yield(.opened(open))
        let chat = store(services)
        chat.launch()
        defer { chat.stop() }
        try await until { chat.phase == .live }
        try await chat.send("then update the docs")
        #expect(services.sent.isEmpty)
        #expect(chat.queue.prompts.map(\.state) == [.waiting])
        let idle = HarnessNotification(
            sessionId: Self.subject.sessionID, kind: .mission, ts: WireDate(.now), epoch: "e1",
            mission: HarnessDelta(rev: open.mission.rev + 1, ops: [.init(op: .summary, summary: MissionSummary())])
        )
        stream.continuation.yield(.note(idle))
        try await until { services.sent.count == 1 && chat.queue.isEmpty }
        #expect(services.sent.first?.mode == .queue)
    }

    @Test func harnessThatQueuesTakesThePromptWhileWorking() async throws {
        var descriptor = Self.claude
        descriptor.capabilities.append(HarnessCapability.queue)
        let services = FakeHarnessServices(descriptor: descriptor)
        var open = try HarnessFixtures.open()
        open.mission.summary.working = true
        services.feeds = [.init(elements: [.opened(open)], end: .hold)]
        let chat = store(services)
        chat.launch()
        defer { chat.stop() }
        try await until { chat.phase == .live }
        try await chat.send("queue this")
        #expect(services.sent.map(\.text) == ["queue this"])
    }

    @Test func failedSendStaysMarkedUntilRetried() async throws {
        let services = FakeHarnessServices(descriptor: Self.claude)
        services.feeds = [.init(elements: [.opened(try HarnessFixtures.open())], end: .hold)]
        services.sendFailures = 1
        let chat = store(services)
        chat.launch()
        defer { chat.stop() }
        try await until { chat.phase == .live }
        try await chat.send("hello")
        guard case .failed = chat.queue.prompts.first?.state else {
            Issue.record("not marked failed")
            return
        }
        chat.retryQueued(chat.queue.prompts[0].id)
        try await until { chat.queue.isEmpty }
        #expect(services.sent.map(\.text) == ["hello"])
    }

    @Test func cancellingAFailedPromptSendsTheOneItHeld() async throws {
        let services = FakeHarnessServices(descriptor: Self.claude)
        services.feeds = [.init(elements: [.opened(try HarnessFixtures.open())], end: .hold)]
        services.sendFailures = 1
        let chat = store(services)
        chat.launch()
        defer { chat.stop() }
        try await until { chat.phase == .live }
        try await chat.send("first")
        try await chat.send("second")
        #expect(chat.queue.prompts.count == 2)
        // The idle session sends no further change, so only the cancel can let "second" go.
        chat.cancelQueued(chat.queue.prompts[0].id)
        try await until { chat.queue.isEmpty }
        #expect(services.sent.map(\.text) == ["second"])
    }

    @Test func olderPageFromAReplacedEpochIsDropped() async throws {
        var descriptor = Self.claude
        descriptor.capabilities.append(HarnessCapability.history)
        let services = FakeHarnessServices(descriptor: descriptor)
        var first = try HarnessFixtures.open()
        first.more = true
        var second = first
        second.epoch = first.epoch + "-next"
        let stream = AsyncThrowingStream<HarnessFeed, any Error>.makeStream()
        stream.continuation.yield(.opened(first))
        services.feeds = [.init(stream: stream.stream)]
        let chat = store(services)
        chat.launch()
        defer { chat.stop() }
        try await until { chat.phase == .live }
        let held = chat.timeline.items.map(\.id)
        let old = HarnessItem(id: "old-epoch", seq: 1, at: WireDate(.now), body: .notice(.init(level: "info", code: "x", text: "old")))
        services.historyPage = {
            // The session opens again under a new epoch while the page is on its way.
            stream.continuation.yield(.opened(second))
            while chat.timeline.epoch != second.epoch { try? await Task.sleep(for: .milliseconds(5)) }
            return HarnessPage(items: [old], more: false)
        }
        await chat.loadOlder()
        #expect(chat.timeline.epoch == second.epoch)
        #expect(chat.timeline.item("old-epoch") == nil)
        #expect(chat.timeline.items.map(\.id) == held)
    }

    @Test func showInTerminalSetsTheJumpAndSwitchesLayer() async throws {
        let services = FakeHarnessServices(descriptor: Self.claude)
        services.feeds = [.init(elements: [.opened(try HarnessFixtures.open())], end: .hold)]
        let chat = store(services)
        chat.launch()
        defer { chat.stop() }
        try await until { chat.rows.count == 3 }
        guard case .prompt(let item) = chat.rows[0] else { Issue.record("no prompt"); return }
        await chat.showInTerminal(item)
        #expect(chat.layer == .terminal)
        #expect(chat.pendingJump == TerminalJump(sessionID: Self.subject.sessionID, linesFromBottom: 12, needle: "> fix the failing date test"))
    }

    // MARK: Lifecycle

    @Test func closedWindowStopsItsStoresAndClosesTheFeed() async throws {
        let services = FakeHarnessServices(descriptor: Self.claude)
        let stream = AsyncThrowingStream<HarnessFeed, any Error>.makeStream()
        let closed = Flag()
        stream.continuation.onTermination = { _ in closed.on = true }
        stream.continuation.yield(.opened(try HarnessFixtures.open()))
        services.feeds = [.init(stream: stream.stream)]
        let tabs = HarnessTabs(services: services)
        let tab = TabID(hostID: Self.subject.hostID, sessionID: Self.subject.sessionID)
        tabs.sync(tabs: [tab]) { _ in [Capability.harness] }
        let chat = try #require(tabs.store(for: tab))
        try await until { chat.phase == .live }
        tabs.stopAll()
        #expect(tabs.stores.isEmpty)
        try await until { closed.on }
    }

    @Test func reopenStormBacksOff() async throws {
        let services = FakeHarnessServices(descriptor: Self.claude)
        let reset = HarnessNotification(sessionId: Self.subject.sessionID, kind: .reset, ts: WireDate(.now), epoch: "e1")
        services.feeds = try (0..<6).map { _ in .init(elements: [.opened(try HarnessFixtures.open()), .note(reset)], end: .hold) }
        let chat = store(services)
        let began = ContinuousClock.now
        chat.launch()
        defer { chat.stop() }
        try await until { services.opens.count >= 6 }
        // Floor 5 ms doubling to 20 ms: 5 + 10 + 20 + 20 + 20 before the sixth open.
        #expect(ContinuousClock.now - began >= .milliseconds(60))
    }

    @Test func stoppedStoreSendsNothingFromItsQueue() async throws {
        let services = FakeHarnessServices(descriptor: Self.claude)
        services.feeds = [.init(elements: [.opened(try HarnessFixtures.open())], end: .hold)]
        services.sendFailures = 1
        let chat = store(services)
        chat.launch()
        try await until { chat.phase == .live }
        try await chat.send("hello")
        chat.stop()
        chat.retryQueued(chat.queue.prompts[0].id)
        try await Task.sleep(for: .milliseconds(50))
        #expect(services.sent.isEmpty)
        #expect(chat.queue.prompts.map(\.state) == [.waiting])
    }

    @Test func singlePaneChatKeepsToTheLeadingPageOfABook() {
        let size = CGSize(width: 800, height: 600)
        let fold = CGRect(x: 396, y: 0, width: 8, height: 600)
        let one = PaneGeometry(.single, size: size, fold: fold)
        #expect(one.chatFrame(of: .primary) == CGRect(x: 0, y: 0, width: 396, height: 600))
        let two = PaneGeometry(.sideBySide(primaryWidth: 396, gap: fold), size: size, fold: fold)
        #expect(two.chatFrame(of: .secondary) == two.secondary)
        #expect(PaneGeometry(.single, size: size).chatFrame(of: .primary) == CGRect(origin: .zero, size: size))
    }

    // MARK: Prompt queue

    @Test func queueSendsOneAtATimeInOrder() {
        var queue = PromptQueue()
        let a = queue.add("first")
        let b = queue.add("second")
        let next1 = queue.next(canSend: false)
        #expect(next1 == nil)
        let next2 = queue.next(canSend: true)
        #expect(next2?.id == a)
        let next3 = queue.next(canSend: true)
        #expect(next3 == nil)
        queue.sent(a!)
        let next4 = queue.next(canSend: true)
        #expect(next4?.id == b)
    }

    @Test func queueRefusesBlankAndOverflow() {
        var queue = PromptQueue()
        let blank = queue.add("   \n")
        #expect(blank == nil)
        for n in 0..<PromptQueue.limit {
            let added = queue.add("p\(n)")
            #expect(added != nil)
        }
        let overflow = queue.add("one more")
        #expect(overflow == nil)
        #expect(queue.isFull)
    }

    @Test func failedPromptHoldsTheRestUntilRetriedOrCancelled() {
        var queue = PromptQueue()
        let a = queue.add("a")!
        queue.add("b")
        _ = queue.next(canSend: true)
        queue.failed(a, message: "offline")
        let next5 = queue.next(canSend: true)
        #expect(next5 == nil)
        let cancelled = queue.cancel(a)
        #expect(cancelled)
        let next6 = queue.next(canSend: true)
        #expect(next6?.text == "b")
    }

    @Test func promptOnItsWayCannotBeCancelled() {
        var queue = PromptQueue()
        let a = queue.add("a")!
        _ = queue.next(canSend: true)
        let refused = queue.cancel(a)
        #expect(!refused)
        queue.requeue(a)
        let taken = queue.cancel(a)
        #expect(taken)
        #expect(queue.isEmpty)
    }

    // MARK: Words

    @Test func workLineWords() {
        let start = Date(timeIntervalSince1970: 0)
        let done = WorkSummary(running: false, startedAt: start, endedAt: start + 72, steps: 6, filesChanged: 2, failed: false)
        #expect(ChatCopy.workLine(done, now: .now) == "Worked 1m 12s  6 steps  2 files changed")
        let running = WorkSummary(running: true, startedAt: start, endedAt: nil, steps: 1, filesChanged: 0, failed: false)
        #expect(ChatCopy.workLine(running, now: start + 9) == "Working 9s  1 step")
        let stopped = WorkSummary(running: false, startedAt: nil, endedAt: nil, steps: 0, filesChanged: 0, failed: true)
        #expect(ChatCopy.workLine(stopped, now: .now) == "Stopped")
        #expect(ChatCopy.clock(3_840) == "1h 4m")
    }

    @Test func quotePrefixesEveryLine() {
        #expect(ChatQuote.quoted("a\nb") == "> a\n> b\n")
    }

    @Test func markdownSplitsCodeFences() {
        let blocks = ChatMarkdown.blocks("Fixed it.\n```go\nfunc a() {}\n```\n- one\n## Next")
        #expect(blocks == [
            .prose("Fixed it."), .code(language: "go", text: "func a() {}"), .prose("\u{2022} one"), .heading("Next"),
        ])
    }

    // MARK: Helpers

    private func until(_ condition: () -> Bool, timeout: Duration = .seconds(3)) async throws {
        let clock = ContinuousClock()
        let deadline = clock.now + timeout
        while !condition() {
            guard clock.now < deadline else {
                Issue.record("timed out")
                return
            }
            try await Task.sleep(for: .milliseconds(5))
        }
    }
}

/// The protocol fixtures the chat is built against: the served exchanges, and the notification
/// examples still kept with the pending ones.
enum HarnessFixtures {
    private static let root: URL = {
        var url = URL(filePath: #filePath)
        for _ in 0..<5 { url.deleteLastPathComponent() }
        return url.appending(path: "protocol/v1/fixtures", directoryHint: .isDirectory)
    }()

    static func open() throws -> HarnessOpenResult {
        struct Exchange: Decodable {
            struct Response: Decodable { var result: HarnessOpenResult }
            var response: Response
        }
        let data = try Data(contentsOf: root.appending(path: "harness.open.json"))
        return try JSONDecoder().decode(Exchange.self, from: data).response.result
    }

    static func notes() throws -> [HarnessNotification] {
        struct File: Decodable {
            struct Note: Decodable { var params: HarnessNotification }
            var notifications: [Note]
        }
        let data = try Data(contentsOf: root.appending(path: "pending/notifications/harness.json"))
        return try JSONDecoder().decode(File.self, from: data).notifications.map(\.params)
    }
}

/// Harness services that replay scripted feeds, one per open, and record what was asked.
final class Flag: @unchecked Sendable {
    var on = false
}

@MainActor final class FakeHarnessServices: HarnessServices {
    var sentImages: [[String]] = []
    var uploads: [String] = []

    func upload(_ s: HarnessSubject, name: String, data: Data) async throws -> String {
        uploads.append(name)
        return "/u/\(name)"
    }

    func image(_ s: HarnessSubject, path: String) async throws -> Data { Data() }

    struct FeedScript {
        enum End { case hold, fail, refuse }
        var stream: AsyncThrowingStream<HarnessFeed, any Error>

        init(stream: AsyncThrowingStream<HarnessFeed, any Error>) {
            self.stream = stream
        }

        init(elements: [HarnessFeed], end: End) {
            stream = AsyncThrowingStream { continuation in
                for element in elements { continuation.yield(element) }
                switch end {
                case .hold: break
                case .fail: continuation.finish(throwing: HelperError.closed)
                case .refuse: continuation.finish(throwing: HelperError.refused("fallback"))
                }
            }
        }
    }

    var descriptor: HarnessDescriptor
    var feeds: [FeedScript] = []
    var sendFailures = 0
    private(set) var describes = 0
    private(set) var opens: [Int] = []
    private(set) var sent: [(text: String, mode: HarnessSendParams.Mode)] = []

    init(descriptor: HarnessDescriptor) {
        self.descriptor = descriptor
    }

    func describe(_ s: HarnessSubject) async throws -> HarnessDescriptor {
        describes += 1
        return descriptor
    }

    func feed(_ s: HarnessSubject, after: Int) -> AsyncThrowingStream<HarnessFeed, any Error> {
        opens.append(after)
        guard !feeds.isEmpty else { return AsyncThrowingStream { _ in } }
        return feeds.removeFirst().stream
    }

    /// Answers `history`; the default is an empty last page.
    var historyPage: @MainActor () async -> HarnessPage = { HarnessPage(items: [], more: false) }

    func history(_ s: HarnessSubject, before: Int) async throws -> HarnessPage { await historyPage() }

    func screen(_ s: HarnessSubject, id: String, query: [String: String]?) async throws -> HarnessScreen {
        throw HarnessUnavailable()
    }

    func act(_ s: HarnessSubject, action: String, target: String?, args: [String: JSONValue]?) async throws -> HarnessActionResult {
        HarnessActionResult(result: .applied)
    }

    func send(_ s: HarnessSubject, text: String, mode: HarnessSendParams.Mode, images: [String]) async throws -> HarnessItem {
        if sendFailures > 0 {
            sendFailures -= 1
            throw HelperError.closed
        }
        sent.append((text, mode))
        sentImages.append(images)
        let id = "sent-\(sent.count)"
        return HarnessItem(id: id, seq: 100 + sent.count, turnId: id, at: WireDate(.now),
                           body: .user(.init(text: text, queued: false, source: "phone")))
    }

    func interrupt(_ s: HarnessSubject) async throws -> HarnessActionResult { HarnessActionResult(result: .applied) }

    func complete(_ s: HarnessSubject, kind: HarnessCompleteParams.Kind, query: String) async throws -> [HarnessCompletion] {
        []
    }

    func locate(_ s: HarnessSubject, anchor: HarnessAnchor) async throws -> HarnessLocation {
        HarnessLocation(found: true, linesFromBottom: Int(26 - anchor.line), needle: anchor.needle)
    }
}
