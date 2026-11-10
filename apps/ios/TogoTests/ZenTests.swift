import CoreGraphics
import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

/// Zen mode: the workspace without its chrome, the terminal at its full height.
@MainActor struct ZenTests {
    private let one = TabID(hostID: Samples.studio, sessionID: "s-01")
    private let four = TabID(hostID: Samples.studio, sessionID: "s-04")

    private func store() -> AppStore {
        var state = Samples.controlCenter
        state.requests = [Samples.request(id: "r-04", bib: "04", title: "Run the tests?")]
        let store = AppStore(services: FakeAppServices(), controlCenter: state)
        store.open(hostID: one.hostID, sessionID: one.sessionID)
        store.open(hostID: four.hostID, sessionID: four.sessionID)
        store.select(one)
        return store
    }

    @Test func zenHidesChromeKeepsRequests() {
        let store = store()
        let requests = store.incomingRequests
        #expect(!requests.isEmpty)

        store.toggleZen()
        #expect(store.zen)
        #expect(store.incomingRequests == requests)

        let hidden = WorkspaceChrome(zen: true, revealed: false)
        #expect(!hidden.shown && hidden.revealItem)
        let revealed = WorkspaceChrome(zen: true, revealed: true)
        #expect(revealed.shown && !revealed.revealItem)
        let normal = WorkspaceChrome(zen: false, revealed: false)
        #expect(normal.shown && !normal.revealItem)

        // Zen belongs to the workspace: with it closed there is nothing to hide.
        store.toggleZen()
        #expect(!store.zen)
        store.workspaceOpen = false
        store.toggleZen()
        #expect(!store.zen)
    }

    @Test func revealShowsChromeForThreeSeconds() async {
        var asked: [Duration] = []
        var wake: CheckedContinuation<Void, Never>?
        let reveal = ZenChrome { duration in
            asked.append(duration)
            await withCheckedContinuation { wake = $0 }
        }
        #expect(!reveal.revealed)
        reveal.reveal()
        #expect(reveal.revealed)
        for _ in 0..<100 where wake == nil { await Task.yield() }
        #expect(asked == [.seconds(3)])

        wake?.resume()
        for _ in 0..<100 where reveal.revealed { await Task.yield() }
        #expect(!reveal.revealed)

        // Hiding at once, as leaving zen does.
        reveal.reveal()
        reveal.hide()
        #expect(!reveal.revealed)
    }

    @Test func zenNeverResizes() throws {
        let store = store()
        let pool = TerminalPool()
        let zen = ZenChrome()
        let host = WorkspaceHost(store: store, terminals: pool, zen: zen, size: CGSize(width: 402, height: 874))
        defer { host.close() }
        let before = try #require(host.terminalSize(of: one))

        var sizes = [before]
        store.toggleZen()
        host.settle { if let size = host.terminalSize(of: one), sizes.last != size { sizes.append(size) } }
        #expect(sizes == [before])

        // Showing the controls for a moment fills the same places without resizing it.
        zen.reveal()
        host.settle { if let size = host.terminalSize(of: one), sizes.last != size { sizes.append(size) } }
        #expect(zen.revealed)

        // Leaving zen does not resize it either.
        store.toggleZen()
        host.settle { if let size = host.terminalSize(of: one), sizes.last != size { sizes.append(size) } }
        #expect(sizes == [before])
    }

    /// Zen keeps the input tools away: the key bar's Attach key does nothing until zen ends.
    @Test func attachWaitsForZenToEnd() {
        let store = store()
        store.toggleZen()
        store.attach(one)
        #expect(store.attaching == nil)
        store.toggleZen()
        store.attach(one)
        #expect(store.attaching == one)
    }

    /// The Dictate key waits for zen to end like Attach does.
    @Test func dictateWaitsForZenToEnd() {
        let store = store()
        store.toggleZen()
        store.dictate(one)
        #expect(store.dictationRequest == nil)
        store.toggleZen()
        store.dictate(one)
        #expect(store.dictationRequest == one)
    }

    /// Zen in one window leaves the Attach and Dictate keys alone on a terminal another window
    /// shows, and a terminal moving into a zen window takes that window's setting.
    @Test func zenKeysStayInTheirWindow() throws {
        let pool = TerminalPool()
        let left = WindowID(), right = WindowID()
        let a = store(), b = store()
        pool.sync(window: left, with: a)
        pool.sync(window: right, with: b)
        pool.show(one, in: right)
        a.toggleZen()
        #expect(a.zen)
        pool.zenChanged(true, in: left)
        let handle = try #require(pool.handle(for: one))
        #expect(!handle.hidesInputKeys, "the terminal is on screen in the other window")

        pool.show(one, in: left)
        #expect(handle.hidesInputKeys)
        pool.show(one, in: right)
        #expect(!handle.hidesInputKeys)
    }

    /// The chrome stays while it is pinned, such as while find is open, and hides three seconds
    /// after it is let go.
    @Test func pinnedChromeStaysRevealed() async {
        var wakes: [CheckedContinuation<Void, Never>] = []
        let reveal = ZenChrome { _ in await withCheckedContinuation { wakes.append($0) } }
        reveal.reveal()
        reveal.pin(true)
        for _ in 0..<100 { await Task.yield() }
        for wake in wakes { wake.resume() }
        wakes.removeAll()
        for _ in 0..<100 { await Task.yield() }
        #expect(reveal.revealed)

        reveal.pin(false)
        for _ in 0..<100 where wakes.isEmpty { await Task.yield() }
        #expect(reveal.revealed)
        for wake in wakes { wake.resume() }
        for _ in 0..<100 where reveal.revealed { await Task.yield() }
        #expect(!reveal.revealed)
    }

    /// ⌘F in zen shows the chrome with its find field and keeps it until find closes. The
    /// chrome exists once, so focus can only go to the field on screen.
    @Test func findInZenRevealsTheChrome() throws {
        let store = store()
        let zen = ZenChrome { _ in try? await Task.sleep(for: .seconds(60)) }
        let host = WorkspaceHost(store: store, terminals: TerminalPool(), zen: zen, size: CGSize(width: 402, height: 874))
        defer { host.close() }
        store.toggleZen()
        host.settle()
        #expect(!zen.revealed)

        store.requestFind()
        host.settle()
        #expect(zen.revealed)
        #expect(zen.pinned)
        #expect(host.textFields == 1)
    }
}

/// WorkspaceView in a real window on the app's scene, so terminals attach as they do on screen.
@MainActor final class WorkspaceHost {
    let window: UIWindow
    let store: AppStore
    let terminals: TerminalPool

    init(store: AppStore, terminals: TerminalPool, zen: ZenChrome, size: CGSize) {
        let scene = UIApplication.shared.connectedScenes.lazy.compactMap { $0 as? UIWindowScene }.first!
        self.store = store
        self.terminals = terminals
        window = UIWindow(windowScene: scene)
        window.frame = CGRect(origin: .zero, size: size)
        window.windowLevel = .normal + 1
        let controller = UIHostingController(rootView: WorkspaceView(store: store, terminals: terminals, zen: zen))
        window.rootViewController = controller
        window.isHidden = false
        settle()
    }

    /// Lets SwiftUI apply state changes and lay the terminal out, checking after each step.
    func settle(for seconds: Double = 0.6, each: () -> Void = {}) {
        for _ in 0..<Int(seconds / 0.02) {
            terminals.sync(window: .single, with: store)
            window.layoutIfNeeded()
            each()
            RunLoop.main.run(until: .now + 0.02)
        }
    }

    func terminalSize(of id: TabID) -> CGSize? {
        guard let view = terminals.handle(for: id)?.engineView.viewIfLoaded, view.window === window else { return nil }
        return view.bounds.size
    }

    /// Text fields in the window, on screen or not: the find field is the only one here.
    var textFields: Int {
        window.rootViewController.map { Self.textFields(in: $0.view) } ?? 0
    }

    private static func textFields(in view: UIView) -> Int {
        (view is UITextField ? 1 : 0) + view.subviews.reduce(0) { $0 + textFields(in: $1) }
    }

    func close() {
        window.isHidden = true
        window.rootViewController = nil
    }
}
