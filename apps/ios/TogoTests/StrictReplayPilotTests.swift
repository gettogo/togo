#if DEBUG
import Foundation
import Testing
import TogoKit
@testable import Togo

/// The UI test verifies strict launch and the real deep link. This drives the same launch's
/// composer model so typed-input automation is independent of simulator hit points.
@MainActor struct StrictReplayPilotTests {
    @Test func capturedSendRefusalAndReconnectPassThroughTheComposer() async throws {
        let launch = UITestLaunch(arguments: ["-ui-fakes", "-ui-one-machine", "-ui-strict-replay", "strict-send-reopen"])
        let composition = AppComposition(uiTest: launch)
        let window = WindowID()
        let app = composition.makeStore(window: window, restoring: nil)
        defer { composition.release(window: window) }
        let store = HarnessSessionStore(
            subject: HarnessSubject(hostID: Samples.studio, sessionID: "s-01", capabilities: [Capability.harness]),
            services: app.harnessServices, retry: (.milliseconds(5), .milliseconds(20)), expectsChat: true
        )
        store.launch()
        defer { store.stop() }
        #expect(await until { store.phase == .live })
        let composer = CommandLineModel(store: store, memory: CommandMemory(defaults: launch.defaults()))
        let text = "Reply with exactly one word: ok"
        composer.typed(text)
        await composer.submit()
        #expect(await until { store.timeline.cursor == 4 && !store.working })
        #expect(store.timeline.items.contains { if case .assistant(let reply) = $0.body { return reply.text == "ok" }; return false })
        composer.typed(text)
        await composer.submit()
        #expect(composer.notice == ComposerCopy.notSent)
        #expect(composer.draft == text)
        #expect(await until { store.timeline.cursor == 5 && store.phase == .live })
        #expect(store.timeline.items.contains { $0.id == "recovery-note" })
        #expect(composition.strictReplayVerdict == "complete")
    }

    private func until(_ condition: @MainActor () -> Bool) async -> Bool {
        let deadline = ContinuousClock.now + .seconds(3)
        while !condition(), ContinuousClock.now < deadline { try? await Task.sleep(for: .milliseconds(10)) }
        return condition()
    }
}
#endif
