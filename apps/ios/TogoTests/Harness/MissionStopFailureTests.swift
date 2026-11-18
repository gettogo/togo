import Testing
import TogoKit
@testable import Togo

@MainActor struct MissionStopFailureTests {
    @Test func stopFallbackReportsFailure() async {
        let services = FakeHarnessServices(descriptor: ChatTests.claude)
        services.interruptResult = HarnessActionResult(result: .fallback)
        let store = HarnessSessionStore(subject: ChatTests.subject, services: services)
        #expect(await MissionWorkspaceModel.stop(store) == MissionCopy.stopFailed)
    }

    @Test func unsupportedStopIsNotOffered() {
        let services = FakeHarnessServices(descriptor: ChatTests.claude)
        var descriptor = ChatTests.claude
        descriptor.capabilities.removeAll { $0 == HarnessCapability.interrupt }
        let store = HarnessSessionStore(subject: ChatTests.subject, services: services, cached: descriptor)
        store.feed.load(HarnessOpenResult(epoch: "e", cursor: 0, items: [], more: false,
            mission: HarnessMission(rev: 1, summary: MissionSummary(working: true), sections: [])))
        let item = MissionToolbarItem(store: store, presentation: .sheet)
        #expect(!item.menuItems.contains(.stop))
    }
}
