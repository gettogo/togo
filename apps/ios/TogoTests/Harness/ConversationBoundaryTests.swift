import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct ConversationBoundaryTests {
    @Test(arguments: ["reset", "sameEpochReset", "reconnect"])
    func heldWorkCannotFollowAnotherConversation(_ trigger: String) async throws {
        let (services, chat, first, second) = try fixture()
        chat.launch()
        defer { chat.stop() }
        try await wait { chat.phase == .live }
        try await chat.send("only for the original conversation")
        chat.note("old model changed")
        chat.runAfterTurn(action: "model", target: "old-model", note: "deferred model changed")
        #expect(chat.queue.prompts.count == 1)
        var next = try HarnessFixtures.open()
        next.epoch = trigger == "sameEpochReset" ? "e1" : "e2"
        next.items = []
        next.cursor = 0
        next.mission.summary.working = false
        second.yield(.opened(next))
        if trigger == "reconnect" {
            first.finish(throwing: HelperError.closed)
        } else {
            first.yield(.note(HarnessNotification(sessionId: ChatTests.subject.sessionID, kind: .reset,
                ts: WireDate(.now), epoch: next.epoch, reason: "epoch")))
        }
        try await wait { services.opens.count == 2 && chat.phase == .live }
        #expect(services.sent.isEmpty)
        #expect(services.actions.isEmpty)
        #expect(chat.queue.prompts.map(\.text) == ["only for the original conversation"])
        #expect(chat.queue.prompts.first?.state.isFailed == true)
        #expect(chat.deferred.isEmpty)
        #expect(!chat.notes.contains { $0.text == "old model changed" })
        #expect(chat.timeline.items.isEmpty)
    }

    @Test func aLateSendResultCannotEnterTheNewTimeline() async throws {
        let (services, chat, first, second) = try fixture()
        let (gate, release) = AsyncStream<Void>.makeStream()
        services.beforeSendResult = { for await _ in gate { break } }
        chat.launch()
        defer { chat.stop(); release.finish() }
        try await wait { chat.phase == .live }
        let send = Task { () -> Bool in
            do { try await chat.send("old prompt", mode: .now); return true }
            catch { return false }
        }
        try await wait { services.sent.count == 1 }
        var next = try HarnessFixtures.open()
        next.epoch = "e2"
        next.items = []
        next.cursor = 0
        next.mission.summary.working = false
        second.yield(.opened(next))
        first.yield(.note(HarnessNotification(sessionId: ChatTests.subject.sessionID, kind: .reset,
            ts: WireDate(.now), epoch: "e2", reason: "epoch")))
        try await wait { chat.timeline.epoch == "e2" && chat.phase == .live }
        release.yield(())
        release.finish()
        #expect(await send.value == false)
        #expect(chat.timeline.items.isEmpty)
        #expect(chat.awaitingSince == nil)
    }

    @Test(arguments: ["reconnect", "history", "overflow"])
    func recoveryOfTheSameConversationRetainsHeldWork(_ trigger: String) async throws {
        let (services, chat, first, second) = try fixture()
        chat.launch()
        defer { chat.stop() }
        try await wait { chat.phase == .live }
        try await chat.send("still the same conversation")
        chat.runAfterTurn(action: "model", target: "model", note: "model changed")
        var next = try HarnessFixtures.open()
        next.mission.summary.working = false
        second.yield(.opened(next))
        if trigger == "reconnect" {
            first.finish(throwing: HelperError.closed)
        } else {
            first.yield(.note(HarnessNotification(sessionId: ChatTests.subject.sessionID, kind: .reset,
                ts: WireDate(.now), epoch: next.epoch, reason: trigger)))
        }
        try await wait { services.sent.count == 1 && services.actions.count == 1 }
        #expect(chat.queue.isEmpty)
        #expect(services.sent[0].text == "still the same conversation")
        #expect(chat.timeline.epoch == "e1")
    }

    @Test func cancellationDoesNotReplayAnUnconfirmedSend() async throws {
        let services = FakeHarnessServices(descriptor: ChatTests.claude)
        let open = try HarnessFixtures.open()
        services.feeds = [.init(elements: [.opened(open)], end: .hold), .init(elements: [.opened(open)], end: .hold)]
        services.beforeSendResult = { try await Task.sleep(for: .seconds(10)) }
        let chat = HarnessSessionStore(subject: ChatTests.subject, services: services,
                                      retry: (.milliseconds(5), .milliseconds(20)))
        chat.launch()
        defer { chat.stop() }
        try await wait { chat.phase == .live }
        let send = Task { try await chat.send("check delivery first") }
        try await wait { services.sent.count == 1 }
        send.cancel()
        try await send.value
        #expect(chat.queue.prompts.first?.state == .unconfirmed)
        chat.stop()
        chat.launch()
        try await wait { services.opens.count == 2 && chat.phase == .live }
        #expect(services.sent.count == 1)
        #expect(chat.queue.prompts.map(\.text) == ["check delivery first"])
    }

    @Test func anInFlightQueuedPromptIsRetainedAsUnconfirmed() async throws {
        let (services, chat, first, second) = try fixture()
        let (gate, release) = AsyncStream<Void>.makeStream()
        services.beforeSendResult = { for await _ in gate { break } }
        chat.launch()
        defer { chat.stop(); release.finish() }
        try await wait { chat.phase == .live }
        try await chat.send("original queued prompt")
        let rev = try #require(chat.mission.mission?.rev)
        first.yield(.note(HarnessNotification(sessionId: ChatTests.subject.sessionID, kind: .mission,
            ts: WireDate(.now), epoch: "e1", mission: HarnessDelta(rev: rev + 1,
                ops: [.init(op: .summary, summary: MissionSummary())]))))
        try await wait { services.sent.count == 1 }
        var next = try HarnessFixtures.open()
        next.epoch = "e2"
        next.items = []
        next.cursor = 0
        next.mission.summary.working = false
        second.yield(.opened(next))
        first.yield(.note(HarnessNotification(sessionId: ChatTests.subject.sessionID, kind: .reset,
            ts: WireDate(.now), epoch: "e2", reason: "epoch")))
        try await wait { chat.timeline.epoch == "e2" && chat.phase == .live }
        release.yield(())
        release.finish()
        await Task.yield()
        #expect(chat.queue.prompts.map(\.text) == ["original queued prompt"])
        #expect(chat.queue.prompts.first?.state == .unconfirmed)
        #expect(chat.timeline.items.isEmpty)
        #expect(services.sent.count == 1)
    }

    @Test func aDeferredSequenceStopsWhenItsConversationChanges() async throws {
        let (services, chat, first, second) = try fixture()
        let (gate, release) = AsyncStream<Void>.makeStream()
        services.beforeActionResult = { for await _ in gate { break } }
        chat.launch()
        defer { chat.stop(); release.finish() }
        try await wait { chat.phase == .live }
        chat.runAfterTurn(action: "model", target: "old", note: "old model applied")
        chat.runAfterTurn(action: "mode", target: "old", note: "old mode applied")
        let rev = try #require(chat.mission.mission?.rev)
        first.yield(.note(HarnessNotification(sessionId: ChatTests.subject.sessionID, kind: .mission,
            ts: WireDate(.now), epoch: "e1", mission: HarnessDelta(rev: rev + 1,
                ops: [.init(op: .summary, summary: MissionSummary())]))))
        try await wait { services.actions.count == 1 }
        var next = try HarnessFixtures.open()
        next.epoch = "e2"
        next.items = []
        next.cursor = 0
        second.yield(.opened(next))
        first.yield(.note(HarnessNotification(sessionId: ChatTests.subject.sessionID, kind: .reset,
            ts: WireDate(.now), epoch: "e2", reason: "epoch")))
        try await wait { chat.timeline.epoch == "e2" && chat.phase == .live }
        release.yield(())
        release.finish()
        await Task.yield()
        #expect(services.actions == ["model"])
        #expect(!chat.notes.contains { $0.text == "old model applied" || $0.text == "old mode applied" })
    }

    private func fixture() throws -> (FakeHarnessServices, HarnessSessionStore,
        AsyncThrowingStream<HarnessFeed, any Error>.Continuation,
        AsyncThrowingStream<HarnessFeed, any Error>.Continuation) {
        let services = FakeHarnessServices(descriptor: ChatTests.claude)
        let first = AsyncThrowingStream<HarnessFeed, any Error>.makeStream()
        let second = AsyncThrowingStream<HarnessFeed, any Error>.makeStream()
        services.feeds = [.init(stream: first.stream), .init(stream: second.stream)]
        var open = try HarnessFixtures.open()
        open.mission.summary.working = true
        first.continuation.yield(.opened(open))
        let chat = HarnessSessionStore(subject: ChatTests.subject, services: services,
                                      retry: (.milliseconds(5), .milliseconds(20)))
        return (services, chat, first.continuation, second.continuation)
    }

    private func wait(_ condition: () -> Bool) async throws {
        let deadline = ContinuousClock.now + .seconds(3)
        while !condition() {
            guard ContinuousClock.now < deadline else { throw HarnessUnavailable() }
            try await Task.sleep(for: .milliseconds(5))
        }
    }
}
