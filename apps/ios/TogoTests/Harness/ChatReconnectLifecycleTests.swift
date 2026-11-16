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
    private func until(_ condition: () -> Bool) async throws {
        let deadline = ContinuousClock.now + .seconds(3)
        while !condition() {
            guard ContinuousClock.now < deadline else { Issue.record("timed out"); return }
            try await Task.sleep(for: .milliseconds(5))
        }
    }

}
