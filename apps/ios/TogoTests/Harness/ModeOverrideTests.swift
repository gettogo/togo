import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct ModeOverrideTests {
    @Test(arguments: [false, true])
    func confirmedModeNeverResurrectsAfterReturningToItsPreviousValue(readIntermediate: Bool) async {
        let session = CommandLineModelTests.Session()
        let model = CommandLineModel(
            store: .preview(), memory: CommandMemory(defaults: UserDefaults(suiteName: UUID().uuidString)!),
            routes: session.routes()
        )
        await model.setMode("plan")
        #expect(model.mode == "plan")
        session.mode = "plan"
        if readIntermediate { #expect(model.mode == "plan") }
        session.mode = "default"
        #expect(model.mode == "default")
        await model.setMode("plan")
        #expect(session.runs.count == 2)
    }
    @Test(arguments: [false, true])
    func realStoreRetiresOptimismIncludingChangesDuringTheAction(duringAction: Bool) async {
        let store = HarnessSessionStore(
            subject: ChatTests.subject, services: FakeHarnessServices(descriptor: HarnessSamples.descriptor),
            cached: HarnessSamples.descriptor
        )
        func report(_ mode: String) {
            store.feed.load(HarnessOpenResult(
                epoch: "mode-test", cursor: 0, items: [], more: false,
                mission: HarnessMission(rev: 1, summary: MissionSummary(), sections: [
                    HarnessSection(kind: .status, status: HarnessStatus(mode: mode))
                ])
            ))
            store.changed()
        }
        report("default")
        var routes = CommandLineModel.Routes.store(store)
        routes.run = { _, _, _ in
            if duringAction { report("plan"); report("default") }
            return HarnessActionResult(result: .applied)
        }
        let model = CommandLineModel(store: store, routes: routes)
        await model.setMode("plan")
        if !duringAction {
            #expect(model.mode == "plan")
            report("plan")
            report("default")
        }
        #expect(store.currentMode == "default")
        #expect(model.mode == "default")
    }

}
