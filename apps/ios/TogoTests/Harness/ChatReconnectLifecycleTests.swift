import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct ChatReconnectLifecycleTests {
    @Test(arguments: [false, true])
    func anotherEpochReopensFromZeroBeforeShowingItsTail(_ emptyTail: Bool) async throws {
        let services = FakeHarnessServices(descriptor: ChatTests.claude)
        let initial = try HarnessFixtures.open()
        var whole = initial
        whole.epoch = "restarted"
        var tail = whole
        tail.items = emptyTail ? [] : Array(whole.items.suffix(1))
        services.feeds = [
            .init(elements: [.opened(initial)], end: .fail),
            .init(elements: [.opened(tail)], end: .hold),
            .init(elements: [.opened(whole)], end: .hold),
        ]
        let chat = HarnessSessionStore(subject: ChatTests.subject, services: services,
                                       retry: (.milliseconds(5), .milliseconds(20)))
        chat.launch()
        defer { chat.stop() }
        try await until { services.opens.count == 3 && chat.phase == .live }
        #expect(services.opens == [0, initial.cursor, 0])
        #expect(chat.timeline.items.map(\.id) == whole.items.map(\.id))
    }

    @Test func aPreviouslyUnavailableAdapterCanBeDescribedOnLaunchAgain() async throws {
        let services = FakeHarnessServices(descriptor: HarnessDescriptor())
        let chat = HarnessSessionStore(subject: ChatTests.subject, services: services)
        chat.launch()
        defer { chat.stop() }
        try await until { chat.phase == .terminalOnly }
        try await until { !chat.isRunning }
        services.descriptor = ChatTests.claude
        services.feeds = [.init(elements: [.opened(try HarnessFixtures.open())], end: .hold)]
        chat.launch()
        try await until { chat.phase == .live }
        #expect(services.describes == 2)
        #expect(chat.chatAvailable)
    }
    @Test func sendingAMultilineDraftDoesNotRestartOrRereadTheConversation() async throws {
        let services = FakeHarnessServices(descriptor: ChatTests.claude)
        services.feeds = [.init(elements: [.opened(try HarnessFixtures.open())], end: .hold)]
        let store = HarnessSessionStore(subject: ChatTests.subject, services: services)
        store.launch()
        defer { store.stop() }
        try await until { store.phase == .live }
        let epoch = store.timeline.epoch
        try await store.send("first line\nsecond line\nthird line", mode: .now)
        #expect(store.phase == .live)
        #expect(services.opens == [0])
        #expect(services.describes == 1)
        #expect(store.timeline.epoch == epoch)
        #expect(ChatLoadingState(store) == .none)
    }

    @Test(arguments: [true, false])
    func helperRestartKeepsTheReadingRowOnlyWhenHistoryStillContainsIt(sharedRows: Bool) async throws {
        let services = FakeHarnessServices(descriptor: ChatTests.claude)
        let first = AsyncThrowingStream<HarnessFeed, any Error>.makeStream()
        let initial = try HarnessFixtures.open()
        var whole = initial
        whole.epoch = "helper-restarted"
        if !sharedRows {
            whole.items = whole.items.map { var item = $0; item.id = "other-" + item.id; return item }
        }
        services.feeds = [.init(stream: first.stream), .init(elements: [.opened(whole)], end: .hold),
                         .init(elements: [.opened(whole)], end: .hold)]
        first.continuation.yield(.opened(initial))
        let store = HarnessSessionStore(subject: ChatTests.subject, services: services, retry: (.milliseconds(5), .milliseconds(20)))
        store.launch()
        defer { store.stop(); first.continuation.finish() }
        try await until { store.phase == .live }
        let reading = try #require(store.rows.first?.id)
        store.chatScroll.pinned = false
        store.chatScroll.settle(at: reading)
        first.continuation.finish(throwing: HelperError.closed)
        try await until { services.opens.count == 3 && store.phase == .live }
        #expect(store.chatScroll.settledRow == (sharedRows ? reading : nil))
        #expect(store.chatScroll.pinned == !sharedRows)
    }

    private func until(_ condition: () -> Bool) async throws {
        let deadline = ContinuousClock.now + .seconds(3)
        while !condition() {
            guard ContinuousClock.now < deadline else { Issue.record("timed out"); return }
            try await Task.sleep(for: .milliseconds(5))
        }
    }

}
