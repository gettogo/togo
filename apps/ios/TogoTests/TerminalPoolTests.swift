import Foundation
import Testing
import TogoKit
@testable import Togo

/// One pool serves every window: a tab has one connection however many windows list it.
@MainActor struct TerminalPoolTests {
    private let services = FakeAppServices()
    private let pool = TerminalPool()
    private let left = WindowID()
    private let right = WindowID()

    private func tab(_ session: String) -> TabID {
        TabID(hostID: Samples.studio, sessionID: session)
    }

    private func store() -> AppStore {
        AppStore(services: services, controlCenter: Samples.controlCenter)
    }

    private func open(_ session: String, in store: AppStore, window: WindowID) {
        store.open(hostID: Samples.studio, sessionID: session)
        pool.sync(window: window, with: store)
    }

    @Test func tabInTwoWindowsHasOneConnection() {
        let a = store(), b = store()
        open("s-01", in: a, window: left)
        open("s-01", in: b, window: right)
        #expect(services.terminalsMade == [tab("s-01")])
        #expect(pool.attached == [tab("s-01")])
        #expect(services.stoppedTerminals.isEmpty)
    }

    @Test func closingWindowKeepsOtherWindowsTabs() {
        let a = store(), b = store()
        open("s-01", in: a, window: left)
        open("s-04", in: a, window: left)
        open("s-01", in: b, window: right)
        pool.release(window: left)
        #expect(services.stoppedTerminals == [tab("s-04")])
        #expect(pool.attached == [tab("s-01")])
        #expect(pool.host(of: tab("s-01")) == right)
    }

    @Test func closingTheLastWindowListingATabLetsItGo() {
        let a = store(), b = store()
        open("s-01", in: a, window: left)
        open("s-01", in: b, window: right)
        b.close(tab("s-01"))
        pool.sync(window: right, with: b)
        #expect(services.stoppedTerminals.isEmpty)
        pool.release(window: left)
        #expect(services.stoppedTerminals == [tab("s-01")])
        #expect(pool.attached.isEmpty)
    }

    @Test func visibleTabsNeverEvicted() {
        let a = store(), b = store()
        // The fifth tab is the newest, so only pinning keeps the four on screen.
        open("s-01", in: a, window: left)
        a.openBeside(tab("s-02"))
        pool.sync(window: left, with: a)
        open("s-04", in: b, window: right)
        b.openBeside(tab("s-scratch"))
        pool.sync(window: right, with: b)
        open("s-05", in: a, window: left)
        #expect(pool.attached.contains(tab("s-05")))
        a.select(tab("s-01"))
        pool.sync(window: left, with: a)

        #expect(pool.attached == [tab("s-01"), tab("s-02"), tab("s-04"), tab("s-scratch")])
        #expect(services.stoppedTerminals.last == tab("s-05"))
    }

    @Test func pastTheLimitTheTabUsedLongestAgoLetsGo() {
        let a = store()
        for session in ["s-01", "s-02", "s-04", "s-05", "s-scratch"] {
            open(session, in: a, window: left)
        }
        #expect(pool.attached.count == TerminalPool.limit)
        #expect(services.stoppedTerminals == [tab("s-01")])
    }

    @Test func showHereMovesTheTerminal() {
        let a = store(), b = store()
        open("s-01", in: a, window: left)
        open("s-01", in: b, window: right)
        pool.show(tab("s-01"), in: left)
        #expect(pool.host(of: tab("s-01")) == left)

        pool.show(tab("s-01"), in: right)
        #expect(pool.host(of: tab("s-01")) == right)
        #expect(services.terminalsMade.count == 1)
        #expect(services.stoppedTerminals.isEmpty)

        // Gestures on the terminal act on the window that shows it.
        a.workspaceOpen = true
        b.workspaceOpen = true
        pool.handle(for: tab("s-01"))?.onEdgeSwipe()
        #expect(a.workspaceOpen)
        #expect(!b.workspaceOpen)
    }

    @Test func restartKeepsTheWindowShowingTheTerminal() throws {
        let a = store(), b = store()
        open("s-01", in: a, window: left)
        open("s-01", in: b, window: right)
        pool.show(tab("s-01"), in: right)
        let listed = try #require(a.selectedTab)

        // Try again tapped in either window, or the machine's details edited.
        pool.restart(listed, store: a)
        #expect(pool.host(of: tab("s-01")) == right)
        #expect(pool.shows(tab("s-01"), in: right))
        #expect(!pool.shows(tab("s-01"), in: left))
        pool.restartTabs(on: Samples.studio)
        #expect(pool.host(of: tab("s-01")) == right)
        #expect(!pool.shows(tab("s-01"), in: left))
        #expect(services.terminalsMade.count == 3)

        // Gestures still act on the window that shows it.
        a.workspaceOpen = true
        b.workspaceOpen = true
        pool.handle(for: tab("s-01"))?.onEdgeSwipe()
        #expect(a.workspaceOpen)
        #expect(!b.workspaceOpen)
    }

    @Test func aTerminalWithoutAWindowIsDrawnNowhereWhileTwoListIt() {
        let a = store(), b = store()
        open("s-01", in: a, window: left)
        open("s-01", in: b, window: right)
        #expect(pool.shows(tab("s-01"), in: left) != pool.shows(tab("s-01"), in: right))
    }

    @Test func transportPickedInOneWindowReachesTheOthers() {
        let a = store(), b = store()
        open("s-01", in: a, window: left)
        open("s-01", in: b, window: right)
        pool.show(tab("s-01"), in: right)

        a.open(hostID: Samples.studio, sessionID: "s-01", transport: .mosh)
        pool.sync(window: left, with: a)
        #expect(b.selectedTab?.transport == .mosh)
        #expect(services.terminalsMade.count == 2)
        #expect(pool.host(of: tab("s-01")) == right)

        // Moving the terminal between windows never reconnects it.
        pool.show(tab("s-01"), in: left)
        pool.sync(window: right, with: b)
        pool.show(tab("s-01"), in: right)
        #expect(services.terminalsMade.count == 2)

        // A window that lists the tab later takes the picked transport too.
        let c = store()
        open("s-01", in: c, window: WindowID())
        #expect(c.selectedTab?.transport == .mosh)
        #expect(services.terminalsMade.count == 2)
    }

    @Test func linkReachesEveryWindowListingTheTab() async {
        let a = store(), b = store()
        open("s-01", in: a, window: left)
        open("s-01", in: b, window: right)
        let live = LinkPhase.live(transport: "SSH", note: nil)
        for _ in 0..<200 where a.selectedTab?.link != live || b.selectedTab?.link != live {
            await Task.yield()
        }
        #expect(a.selectedTab?.link == live)
        #expect(b.selectedTab?.link == live)

        // A window that lists the tab later starts from where the link already is.
        let c = store()
        open("s-01", in: c, window: WindowID())
        #expect(c.selectedTab?.link == live)
        #expect(services.terminalsMade.count == 1)
    }
}
