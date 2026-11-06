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
        store.composing = one
        let requests = store.incomingRequests
        #expect(!requests.isEmpty)

        store.toggleZen()
        #expect(store.zen)
        #expect(store.composing == nil)
        #expect(store.incomingRequests == requests)

        let hidden = WorkspaceChrome(zen: true, revealed: false)
        #expect(!hidden.inset && !hidden.overlay && hidden.revealStrip)
        let revealed = WorkspaceChrome(zen: true, revealed: true)
        #expect(!revealed.inset && revealed.overlay && !revealed.revealStrip)
        let normal = WorkspaceChrome(zen: false, revealed: false)
        #expect(normal.inset && !normal.overlay && !normal.revealStrip)

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
        #expect(ZenChrome.stripHeight == 44)

        wake?.resume()
        for _ in 0..<100 where reveal.revealed { await Task.yield() }
        #expect(!reveal.revealed)

        // Hiding at once, as leaving zen does.
        reveal.reveal()
        reveal.hide()
        #expect(!reveal.revealed)
    }

    @Test func zenResizesOnce() throws {
        let store = store()
        let pool = TerminalPool()
        let zen = ZenChrome()
        let host = WorkspaceHost(store: store, terminals: pool, zen: zen, size: CGSize(width: 402, height: 874))
        defer { host.close() }
        let before = try #require(host.terminalSize(of: one))

        var sizes = [before]
        store.toggleZen()
        host.settle { if let size = host.terminalSize(of: one), sizes.last != size { sizes.append(size) } }
        let after = try #require(host.terminalSize(of: one))
        #expect(after.height > before.height)
        #expect(after.width == before.width)
        #expect(sizes == [before, after])

        // Showing the chrome from the top edge lays it over the terminal without resizing it.
        zen.reveal()
        host.settle { if let size = host.terminalSize(of: one), sizes.last != size { sizes.append(size) } }
        #expect(zen.revealed)
        #expect(sizes == [before, after])
    }

    /// In zen the compose bar stays shut: the key bar's Compose key does nothing until zen ends.
    @Test func composeToggleWaitsForZenToEnd() {
        let store = store()
        store.toggleZen()
        store.toggleCompose(one)
        #expect(store.composing == nil)
        store.toggleZen()
        store.toggleCompose(one)
        #expect(store.composing == one)
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
