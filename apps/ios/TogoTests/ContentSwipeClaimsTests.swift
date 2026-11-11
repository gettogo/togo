import Testing
import UIKit
@testable import Togo

/// Two terminals under one navigation controller share its content swipe: it stays off while
/// either is visible and uncovered, and comes back as it was when neither is.
@MainActor struct ContentSwipeClaimsTests {
    @Test func theFirstClaimTurnsTheSwipeOffAndTheLastReleasePutsItBack() {
        let claims = ContentSwipeClaims()
        let swipe = UIPanGestureRecognizer()
        claims.claim(swipe)
        claims.claim(swipe)
        #expect(!swipe.isEnabled)
        claims.release(swipe)
        #expect(!swipe.isEnabled, "the other terminal still shows")
        #expect(claims.count(for: swipe) == 1)
        claims.release(swipe)
        #expect(swipe.isEnabled)
        #expect(claims.count(for: swipe) == 0)
    }

    @Test func theSwipeGoesBackToWhatItWasBeforeTheFirstClaim() {
        let claims = ContentSwipeClaims()
        let swipe = UIPanGestureRecognizer()
        swipe.isEnabled = false
        claims.claim(swipe)
        claims.release(swipe)
        #expect(!swipe.isEnabled)
    }

    @Test func aReleaseWithoutAClaimChangesNothing() {
        let claims = ContentSwipeClaims()
        let swipe = UIPanGestureRecognizer()
        claims.claim(swipe)
        claims.release(swipe)
        claims.release(swipe)
        #expect(swipe.isEnabled)
        claims.claim(swipe)
        #expect(!swipe.isEnabled, "a stray release never leaves a debt the next claim pays")
    }

    @Test func eachNavigationControllerCountsOnItsOwn() {
        let claims = ContentSwipeClaims()
        let one = UIPanGestureRecognizer()
        let two = UIPanGestureRecognizer()
        claims.claim(one)
        claims.claim(two)
        claims.release(two)
        #expect(!one.isEnabled)
        #expect(two.isEnabled)
    }

    // MARK: Two panes, two containers, one navigation controller

    private struct Panes {
        let window: UIWindow
        let nav: UINavigationController
        let left: TerminalContainer
        let right: TerminalContainer
        var swipe: UIGestureRecognizer? { nav.interactiveContentPopGestureRecognizer }
    }

    private func panes(_ claims: ContentSwipeClaims) throws -> Panes {
        let root = UIViewController()
        let nav = UINavigationController(rootViewController: root)
        let scene = try #require(UIApplication.shared.connectedScenes.lazy.compactMap { $0 as? UIWindowScene }.first)
        let window = UIWindow(windowScene: scene)
        window.frame = CGRect(x: 0, y: 0, width: 951, height: 669)
        window.rootViewController = nav
        window.isHidden = false
        window.layoutIfNeeded()
        let left = TerminalContainer()
        let right = TerminalContainer()
        for container in [left, right] {
            container.swipeClaims = claims
            root.addChild(container)
            root.view.addSubview(container.view)
            container.didMove(toParent: root)
            container.beginAppearanceTransition(true, animated: false)
            container.endAppearanceTransition()
        }
        window.layoutIfNeeded()
        return Panes(window: window, nav: nav, left: left, right: right)
    }

    @Test func aChatInOnePaneKeepsTheSwipeOffForTheTerminalInTheOther() throws {
        let claims = ContentSwipeClaims()
        let panes = try panes(claims)
        let swipe = try #require(panes.swipe)
        #expect(claims.count(for: swipe) == 2)
        panes.left.setCovered(true)
        #expect(!swipe.isEnabled)
        panes.right.setCovered(true)
        #expect(swipe.isEnabled, "under two chats the content swipe is the chat's")
        panes.left.setCovered(false)
        #expect(!swipe.isEnabled)
        panes.left.leave()
        panes.right.leave()
    }

    @Test func aPaneClosingKeepsTheSwipeOffForTheOneLeft() throws {
        let claims = ContentSwipeClaims()
        let panes = try panes(claims)
        let swipe = try #require(panes.swipe)
        panes.right.beginAppearanceTransition(false, animated: false)
        panes.right.endAppearanceTransition()
        panes.right.leave()
        #expect(!swipe.isEnabled)
        #expect(claims.count(for: swipe) == 1)
        panes.left.leave()
        #expect(swipe.isEnabled)
        #expect(claims.count(for: swipe) == 0)
    }

    @Test func aDisappearAndALeaveReleaseOnlyOnce() throws {
        let claims = ContentSwipeClaims()
        let panes = try panes(claims)
        let swipe = try #require(panes.swipe)
        panes.left.beginAppearanceTransition(false, animated: false)
        panes.left.endAppearanceTransition()
        panes.left.leave()
        panes.left.setCovered(false)
        #expect(claims.count(for: swipe) == 1)
        panes.right.leave()
        #expect(swipe.isEnabled)
    }
}
