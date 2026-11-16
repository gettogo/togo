import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct StaleWorkTests {
    @Test(arguments: [false, true])
    func aLostOrStoppedFeedFreezesOnlyTheWorkingPresentation(_ stop: Bool) async throws {
        let first = AsyncThrowingStream<HarnessFeed, any Error>.makeStream()
        let second = AsyncThrowingStream<HarnessFeed, any Error>.makeStream()
        let services = FakeHarnessServices(descriptor: ChatTests.claude)
        services.feeds = [.init(stream: first.stream), .init(stream: second.stream)]
        var opened = try HarnessFixtures.open()
        opened.mission.summary.working = true
        opened.mission.summary.needsYou = 0
        let chat = HarnessSessionStore(subject: ChatTests.subject, services: services,
                                       retry: (.milliseconds(5), .milliseconds(20)))
        let routes = CommandLineModel.Routes.store(chat)
        let composer = CommandLineModel(store: chat)
        chat.launch()
        defer { chat.stop() }
        first.continuation.yield(.opened(opened))
        try await until { chat.phase == .live && chat.liveWork != nil }
        #expect(chat.working && composer.canInterrupt)
        #expect(!routes.describing())

        if stop { chat.stop() }
        else {
            first.continuation.finish(throwing: HelperError.closed)
            try await until { services.opens.count == 2 }
        }
        let now = Date.now
        let stale = try #require(chat.liveWork)
        let held = WorkingCopy.seconds(stale, now: now + 60)
        #expect(held == WorkingCopy.seconds(stale, now: now + 120))
        #expect(WorkingCopy.word(stale) == ChatCopy.reconnecting)
        #expect(chat.working, "A stale display must not mark the agent idle or drain queued prompts")
        #expect(!composer.canInterrupt)
        #expect(!routes.describing(), "Cached status must not blink back to its loading placeholder")
        chat.refreshWorking(now: now + 60)
        #expect(held == WorkingCopy.seconds(try #require(chat.liveWork), now: now + 120))

        if stop { chat.launch() }
        second.continuation.yield(.opened(opened))
        try await until { chat.phase == .live }
        let live = try #require(chat.liveWork)
        let earlier = try #require(WorkingCopy.seconds(live, now: now + 60))
        let later = try #require(WorkingCopy.seconds(live, now: now + 120))
        #expect(abs(later - earlier - 60) < 0.001)
        #expect(WorkingCopy.word(live) == WorkingCopy.working)
        #expect(composer.canInterrupt && !routes.describing())
    }

    @Test func aFirstOpenStillUsesTheStatePlaceholder() {
        let chat = HarnessSessionStore(subject: ChatTests.subject,
                                       services: FakeHarnessServices(descriptor: ChatTests.claude))
        #expect(CommandLineModel.Routes.store(chat).describing())
    }

    @Test func aPausedWorkLineDoesNotAnnounceTheStaleStep() {
        let start = Date(timeIntervalSince1970: 1_793_000_000)
        let state = WorkingState(clock: TurnClock(start: start), step: "Reading config.swift",
                                 pausedAt: start + 12)
        #expect(WorkingCopy.clock(state, now: start + 120) == "12s")
        #expect(WorkingCopy.spoken(state, now: start + 120) == "Reconnecting, 12 seconds")
        #expect(LiveLineLayout.segments(state, now: start + 120) == [
            .word(ChatCopy.reconnecting), .separator, .clock("12s"),
        ])
    }

    private func until(_ condition: () -> Bool) async throws {
        let deadline = ContinuousClock.now + .seconds(3)
        while !condition() {
            guard ContinuousClock.now < deadline else { Issue.record("timed out"); return }
            try await Task.sleep(for: .milliseconds(5))
        }
    }
}
