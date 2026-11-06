import CoreGraphics
import SwiftUI
import Testing
import UIKit
@testable import Togo

/// Two tabs side by side: what each pane shows, which one takes input, and where things may sit.
@MainActor struct PanesTests {
    private let services = FakeAppServices()
    private let one = TabID(hostID: Samples.studio, sessionID: "s-01")
    private let two = TabID(hostID: Samples.studio, sessionID: "s-02")
    private let four = TabID(hostID: Samples.studio, sessionID: "s-04")

    private static let iPad = CGSize(width: 1032, height: 1376)
    private static let duoOpen = CGSize(width: 951, height: 669)
    private static let book = FoldInfo.from(active: CGRect(x: 463, y: 0, width: 25, height: 669), inactive: nil)
    private static let tabletop = FoldInfo.from(active: CGRect(x: 0, y: 451, width: 669, height: 25), inactive: nil)

    private func store(_ tabs: [TabID], beside: TabID? = nil) -> AppStore {
        var state = Samples.controlCenter
        state.requests = [
            Samples.request(id: "r-01", bib: "01", title: "Edit the config?"),
            Samples.request(id: "r-04", bib: "04", title: "Run the tests?"),
        ]
        let store = AppStore(services: services, controlCenter: state)
        for tab in tabs {
            store.open(hostID: tab.hostID, sessionID: tab.sessionID)
        }
        store.select(tabs[0])
        if let beside { store.openBeside(beside) }
        return store
    }

    @Test func twoPanesShowTwoTerminals() throws {
        let store = store([one, four], beside: four)
        let pool = TerminalPool()
        let host = PanesHost(store: store, terminals: pool, size: Self.iPad, sizeClass: .regular)
        defer { host.close() }

        #expect(store.panesVisible)
        let left = try #require(host.visibleFrame(of: one))
        let right = try #require(host.visibleFrame(of: four))
        #expect(left.maxX <= right.minX)
        #expect(left.width > 360 && right.width > 360)
    }

    @Test func wideWindowWithoutSecondaryNeverSplits() {
        for (size, fold) in [(Self.iPad, FoldInfo.flat), (Self.duoOpen, .flat), (Self.duoOpen, Self.book)] {
            let geometry = PaneGeometry(
                PaneLayout.arrange(size: size, sizeClass: .regular, fold: fold, hasSecondary: false, primaryFraction: 0.5),
                size: size
            )
            #expect(geometry.secondary == nil)
            #expect(geometry.primary.size == size)
        }
        let store = store([one, two, four])
        let host = PanesHost(store: store, terminals: TerminalPool(), size: Self.iPad, sizeClass: .regular)
        defer { host.close() }
        #expect(!store.panesVisible)
        #expect(store.workspace.secondary == nil)
        #expect(host.visibleFrame(of: one)?.width == Self.iPad.width)
    }

    @Test func tapFocusesPane() {
        let store = store([one, four], beside: four)
        let host = PanesHost(store: store, terminals: TerminalPool(), size: Self.iPad, sizeClass: .regular)
        defer { host.close() }
        PaneView.tapped(.primary, store: store)
        #expect(store.workspace.focused == .primary)
        PaneView.tapped(.secondary, store: store)
        #expect(store.workspace.focused == .secondary)
        #expect(store.focusedTab?.id == four)
    }

    @Test func composeTargetsFocusedPane() {
        let store = store([one, four], beside: four)
        store.setPanesVisible(true)
        store.focus(.secondary)
        #expect(store.composeTab?.id == four)
        #expect(store.composeTarget == "04")
        store.focus(.primary)
        #expect(store.composeTab?.id == one)
        #expect(store.composeTarget == "01")
        // One pane on screen: the header already names the session.
        store.setPanesVisible(false)
        #expect(store.composeTarget == nil)
    }

    @Test func composeKeyInTheOtherPaneFocusesIt() {
        let store = store([one, four], beside: four)
        let host = PanesHost(store: store, terminals: TerminalPool(), size: Self.iPad, sizeClass: .regular)
        defer { host.close() }
        store.focus(.primary)
        store.toggleCompose(four)
        host.settle()
        #expect(store.workspace.focused == .secondary)
        #expect(store.composeTab?.id == four)
    }

    @Test func requestsHideForVisiblePanes() {
        let store = store([one, four], beside: four)
        let host = PanesHost(store: store, terminals: TerminalPool(), size: Self.iPad, sizeClass: .regular)
        #expect(store.incomingRequests.isEmpty)
        host.resize(to: CGSize(width: 402, height: 874), sizeClass: .compact)
        #expect(store.incomingRequests.map(\.id) == ["r-04"])
        host.close()
    }

    @Test func droppedTabOpensInThatPane() {
        let store = store([one, two, four], beside: four)
        store.setPanesVisible(true)
        #expect(PaneView.accept([two], into: .secondary, store: store))
        #expect(store.workspace.selected == one)
        #expect(store.workspace.secondary == two)
        #expect(store.workspace.focused == .secondary)

        // A tab this window has not opened, dragged from another window.
        let five = TabID(hostID: Samples.studio, sessionID: "s-05")
        #expect(PaneView.accept([five], into: .primary, store: store))
        #expect(store.workspace.selected == five)
        #expect(store.workspace.secondary == two)
        #expect(!PaneView.accept([], into: .primary, store: store))
    }

    @Test func nothingInteractiveInFold() {
        let cases: [(CGSize, FoldInfo, Bool)] = [
            (Self.duoOpen, Self.book, true), (Self.duoOpen, Self.book, false),
            (CGSize(width: 669, height: 951), Self.tabletop, true),
        ]
        for (size, fold, beside) in cases {
            let report = PanesReport(size: size, sizeClass: .regular, fold: fold, hasSecondary: beside, primaryFraction: 0.5)
            let geometry = report.geometry
            let band = try! #require(fold.division)
            var interactive: [CGRect] = [geometry.chromeFrame()].compactMap(\.self)
            for pane in [Pane.primary, .secondary] {
                interactive += [
                    geometry.headerFrame(of: pane), geometry.requestsFrame(focused: pane), geometry.composeFrame(focused: pane),
                ].compactMap(\.self)
            }
            interactive += [geometry.deck].compactMap(\.self)
            for frame in interactive {
                let overlap = frame.intersection(band)
                #expect(overlap.isNull || overlap.width * overlap.height == 0, "\(frame) overlaps the fold \(band)")
            }
        }
    }

    @Test func foldUnderTheSidebarIsNoFold() {
        // Book posture with the sidebar on the leading page: the fold lies just before the panes.
        let fold = FoldInfo.from(active: CGRect(x: -25, y: 0, width: 25, height: 669), inactive: nil)
        let report = PanesReport(
            size: CGSize(width: 463, height: 669), sizeClass: .regular, fold: fold, hasSecondary: true, primaryFraction: 0.5
        )
        #expect(report.arrangement == .single)
        #expect(report.geometry.leadingPage == nil)
        #expect(report.geometry.composeFrame(focused: .primary) == nil)
    }

    @Test func swipeInTheSecondaryPagesOnlyItsPane() {
        let store = store([one, two, four], beside: four)
        store.setPanesVisible(true)
        store.step(-1, from: four)
        #expect(store.workspace.selected == one)
        #expect(store.workspace.secondary == two)
        // The far end stops; the primary's tab is not among the secondary's pages.
        store.step(-1, from: two)
        #expect(store.workspace.secondary == two)
        #expect(store.workspace.selected == one)

        store.step(1, from: one)
        #expect(store.workspace.selected == four)
        #expect(store.workspace.secondary == two)
    }

    @Test func keyboardFollowsTheFocusedPane() throws {
        let store = store([one, four], beside: four)
        let pool = TerminalPool()
        let host = PanesHost(store: store, terminals: pool, size: Self.iPad, sizeClass: .regular)
        defer { host.close() }
        host.window.makeKey()
        store.focus(.primary)
        pool.handle(for: one)?.raiseKeyboard()
        host.settle()
        try #require(pool.handle(for: one)?.holdsKeyboard == true)

        store.focus(.secondary)
        host.settle(for: 0.4)
        #expect(pool.handle(for: four)?.holdsKeyboard == true)
        #expect(pool.handle(for: one)?.holdsKeyboard == false)
    }

    @Test func pagesBesideAnotherPaneLeaveTheirTerminalAlone() {
        let store = store([one, two, four], beside: four)
        store.workspace.primaryFraction = 1.0 / 3
        let pool = TerminalPool()
        let host = PanesHost(store: store, terminals: pool, size: CGSize(width: 1376, height: 1032), sizeClass: .regular)
        defer { host.close() }
        host.settle(for: 0.5)
        #expect(pool.attached.contains(two))
        // Off screen in both pagers, its terminal is in neither.
        #expect(pool.handle(for: two)?.engineView.parent == nil)
        #expect(host.visibleFrame(of: one) != nil)
        #expect(host.visibleFrame(of: four) != nil)
    }

    @Test func dividerSnapsToTheNearestFraction() {
        let store = store([one, four], beside: four)
        // A landscape iPad fits every split.
        PaneDivider.settle(at: 900, width: 1376, store: store)
        #expect(store.workspace.primaryFraction == 2.0 / 3)
        PaneDivider.settle(at: 400, width: 1376, store: store)
        #expect(store.workspace.primaryFraction == 1.0 / 3)
        // In portrait a third would leave a pane too narrow, so the panes stay even.
        PaneDivider.settle(at: 300, width: 1032, store: store)
        #expect(store.workspace.primaryFraction == 0.5)
    }

    @Test func closePaneFromEitherSideKeepsTheOther() {
        let store = store([one, four], beside: four)
        store.setPanesVisible(true)
        PaneHeader.closePane(.primary, store: store)
        #expect(store.workspace.selected == four)
        #expect(store.workspace.secondary == nil)

        store.openBeside(one)
        PaneHeader.closePane(.secondary, store: store)
        #expect(store.workspace.selected == four)
        #expect(store.workspace.secondary == nil)
    }

    @Test func chipTapInTwoPanesFillsTheFocusedPane() {
        let store = store([one, two, four], beside: four)
        store.setPanesVisible(true)
        store.focus(.secondary)
        store.choose(two)
        #expect(store.workspace.selected == one)
        #expect(store.workspace.secondary == two)
        store.choose(one)
        #expect(store.workspace.focused == .primary)
        #expect(store.workspace.secondary == two)
    }
}

/// WorkspacePanes in a real window on the app's scene, so terminals attach as they do on screen.
@MainActor final class PanesHost {
    @Observable final class Frame {
        var sizeClass: UserInterfaceSizeClass
        var fold: FoldInfo
        init(sizeClass: UserInterfaceSizeClass, fold: FoldInfo) {
            self.sizeClass = sizeClass
            self.fold = fold
        }
    }

    private struct Root: View {
        let frame: Frame
        let store: AppStore
        let terminals: TerminalPool

        var body: some View {
            WorkspacePanes(store: store, terminals: terminals, window: .single)
                .environment(\.horizontalSizeClass, frame.sizeClass)
                .environment(\.fold, frame.fold)
        }
    }

    let window: UIWindow
    let frame: Frame
    let store: AppStore
    let terminals: TerminalPool

    init(store: AppStore, terminals: TerminalPool, size: CGSize, sizeClass: UserInterfaceSizeClass, fold: FoldInfo = .flat) {
        let scene = UIApplication.shared.connectedScenes.lazy.compactMap { $0 as? UIWindowScene }.first!
        self.store = store
        self.terminals = terminals
        frame = Frame(sizeClass: sizeClass, fold: fold)
        window = UIWindow(windowScene: scene)
        window.frame = CGRect(origin: .zero, size: size)
        window.windowLevel = .normal + 1
        let controller = UIHostingController(rootView: Root(frame: frame, store: store, terminals: terminals))
        controller.safeAreaRegions = []
        window.rootViewController = controller
        window.isHidden = false
        settle()
    }

    func resize(to size: CGSize, sizeClass: UserInterfaceSizeClass, fold: FoldInfo = .flat) {
        window.frame = CGRect(origin: .zero, size: size)
        frame.sizeClass = sizeClass
        frame.fold = fold
        settle()
    }

    /// Lets SwiftUI apply state changes, sync the pool and lay out the terminals.
    func settle(for seconds: Double = 0.3) {
        for _ in 0..<Int(seconds / 0.05) {
            terminals.sync(window: .single, with: store)
            window.layoutIfNeeded()
            RunLoop.main.run(until: .now + 0.05)
        }
    }

    /// Where the tab's terminal is on screen, nil when it is not in the window or lies outside it.
    func visibleFrame(of id: TabID) -> CGRect? {
        guard let view = terminals.handle(for: id)?.engineView.viewIfLoaded, view.window === window else { return nil }
        let frame = view.convert(view.bounds, to: nil).intersection(window.bounds)
        return frame.isNull || frame.width < 1 ? nil : frame
    }

    func close() {
        window.isHidden = true
        window.rootViewController = nil
    }
}
