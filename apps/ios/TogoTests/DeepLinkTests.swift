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

    @Test func newSessionLinkPicksTheNamedMachine() {
        let store = AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
        store.open(.newSession(host: "Build box"))
        #expect(store.sheet == .newSession(Samples.build))
        store.sheet = nil
        store.open(.newSession(host: "Nowhere"))
        #expect(store.sheet == .newSession(nil))
    }
}
