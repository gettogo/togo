import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct DeferredActionResultTests {
    @Test(arguments: ["fallback", "failed", "typed", "queued", "future", "applied", "relaunched"])
    func unconfirmedChangeNeverClaimsARestart(_ outcome: String) async throws {
        let services = FakeHarnessServices(descriptor: ChatTests.claude)
        services.actionResult = HarnessActionResult(result: .init(rawValue: outcome))
        services.feeds = [.init(elements: [.opened(try HarnessFixtures.open())], end: .hold)]
        let store = HarnessSessionStore(subject: ChatTests.subject, services: services)
        store.launch()
        defer { store.stop() }
        try await until { store.phase == .live }
        store.runAfterTurn(action: "set_model", note: "Restarted with test model")
        store.runDeferred()
        try await until { !store.notes.isEmpty }
        #expect(store.notes.contains { $0.text == "Restarted with test model" } == (outcome == "relaunched"))
        if outcome == "applied" { #expect(store.notes.last?.text == PrimitiveCopy.done) }
        if ["fallback", "failed", "future"].contains(outcome) { #expect(store.notes.last?.code == "error") }
        if outcome == "typed" { #expect(store.notes.last?.text == PrimitiveCopy.sent) }
        if outcome == "queued" { #expect(store.notes.last?.text == PrimitiveCopy.queued) }
    }

    private func until(_ predicate: () -> Bool) async throws {
        let deadline = ContinuousClock.now + .seconds(3)
        while !predicate() {
            guard ContinuousClock.now < deadline else { throw HarnessUnavailable() }
            try await Task.sleep(for: .milliseconds(5))
        }
    }
}
