import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct DeepLinkTests {
    private func link(_ text: String) -> DeepLink? {
        URL(string: text).flatMap(DeepLink.init(url:))
    }

    @Test func parsesSessionLink() {
        #expect(link("togo://session/07?host=Studio") == .session(bib: "07", host: "Studio"))
        #expect(link("togo://session/07") == .session(bib: "07", host: nil))
        #expect(link("TOGO://session/12?host=") == .session(bib: "12", host: nil))
        #expect(link("togo://new?host=Build%20box") == .newSession(host: "Build box"))
        #expect(link("togo://new") == .newSession(host: nil))
    }

    @Test func rejectsUnknownSchemeAndHost() {
        let refused = [
            "https://session/07?host=Studio",
            "togo://open/07",
            "togo://session",
            "togo://session/07/extra",
            "togo://session/0%0A7",
            "togo://session/123456789",
            "togo://new/07",
        ]
        for text in refused {
            #expect(link(text) == nil, "\(text)")
        }
    }

    @Test func endedBibShowsNotice() {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.path = [.settings]
        store.open(.session(bib: "07", host: "Studio"))
        #expect(store.notice == Notice(text: "Session 07 has ended.", isError: false))
        #expect(!store.workspaceOpen)
        #expect(store.path.isEmpty)
        #expect(store.pendingLink == nil)
    }

    @Test func liveBibOpensItsTab() {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.open(.session(bib: "04", host: "studio"))
        #expect(store.workspaceOpen)
        #expect(store.workspace.selected == TabID(hostID: Samples.studio, sessionID: "s-04"))
        #expect(store.notice == nil)
    }

    @Test(arguments: [false, true], [false, true])
    func aRequestLinkRevealsItsFullQuestion(inline: Bool, alreadyOpen: Bool) async throws {
        var center = Samples.controlCenter
        center.machines[0].capabilities = [Capability.harness]
        let full = "Choose the deployment target, including the complete explanation before the final line."
        let question = HarnessItem(id: "question-full", seq: 1, at: WireDate(Samples.now), body: .question(.init(
            questions: [.init(id: "q", prompt: full, options: [], multi: false, allowText: true)], requestId: "r1"
        )))
        let feed = HarnessOpenResult(epoch: "e1", cursor: 1, items: [question], more: false,
            mission: .init(rev: 1, summary: .init(needsYou: 1), sections: []))
        let services = FakeHarnessServices(descriptor: ChatTests.claude)
        services.feeds = [.init(elements: [.opened(feed)], end: .hold)]
        let app = AppStore(services: FakeAppServices(), controlCenter: center, harnessServices: services)
        defer { HarnessTabsRegistry.shared.release(app) }
        app.setWorkspaceInline(inline)
        let id = TabID(hostID: Samples.studio, sessionID: "s-04")
        if alreadyOpen {
            app.open(hostID: id.hostID, sessionID: id.sessionID)
            let chat = try #require(app.harness(for: id))
            try await wait { chat.phase == .live }
            chat.layer = .terminal
            chat.focusedAgent = "old-agent"
        }
        app.workspaceOpen = false
        app.path = [.settings]
        app.open(.session(bib: "04", host: "Studio", hostID: Samples.studio, sessionID: "s-04"))
        let chat = try #require(app.harness(for: id))
        try await wait { chat.phase == .live && chat.scrollRequest?.row == question.id }
        #expect(app.path.isEmpty && app.workspaceOnScreen)
        #expect(app.focusedTab?.id == id)
        #expect(chat.shownLayer == .chat && chat.focusedAgent == nil)
        #expect(chat.pendingNeedsRequestID == nil)
        let item = try #require(chat.timeline.item(question.id))
        if case .question(let value) = item.body { #expect(value.questions.first?.prompt == full) }
        else { Issue.record("The full question must remain in the chat") }
        #expect(chat.interaction == nil)
    }

    private func wait(_ ready: () -> Bool) async throws {
        let deadline = ContinuousClock.now + .seconds(3)
        while !ready(), ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(5)) }
        try #require(ready())
    }

    /// A cold launch from a notification arrives before the machine has reported.
    @Test func sessionLinkWaitsForItsMachine() {
        let store = AppStore(services: FakeAppServices())
        store.open(.session(bib: "04", host: "Studio"))
        #expect(store.notice == nil)
        #expect(store.pendingLink != nil)
        store.apply(HostViewSnapshot(
            hostID: Samples.studio, reachability: .connecting, helper: .checking, sessions: [], requests: [],
            at: Samples.now
        ))
        #expect(store.pendingLink != nil)
        store.apply(HostViewSnapshot(
            hostID: Samples.studio, reachability: .online, helper: .present(version: "0.3.0", live: true),
            sessions: [Samples.row(bib: "04", name: "app", cwd: "/Users/sam/src/app")], requests: [], at: Samples.now
        ))
        #expect(store.workspace.selected == TabID(hostID: Samples.studio, sessionID: "s-04"))
        #expect(store.pendingLink == nil)
    }

    /// With no machine set up there is nowhere to start a session: the link is dropped and
    /// first-run setup stays on screen.
    @Test func newSessionLinkWithNoMachinesOpensNothing() {
        let store = AppStore(services: FakeAppServices(hosts: []), controlCenter: Samples.controlCenter)
        store.open(.newSession(host: nil))
        #expect(store.sheet == nil)
        #expect(store.pendingLink == nil)
    }

    @Test func newSessionLinkPicksTheNamedMachine() {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.open(.newSession(host: "Build box"))
        #expect(store.sheet == .newSession(Samples.build))
        store.sheet = nil
        store.open(.newSession(host: "Nowhere"))
        #expect(store.sheet == .newSession(nil))
    }
}
