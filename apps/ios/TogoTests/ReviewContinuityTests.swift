import Foundation
import SwiftUI
import Testing
import TogoKit
@testable import Togo

/// Folding or unfolding a Duo during a review keeps what was open in it and what was typed.
@MainActor struct ReviewContinuityTests {
    private func subject(_ session: String = "s-01") -> ReviewSubject {
        ReviewSubject(
            hostID: Samples.studio, sessionID: session, bib: "01", title: "api", cwd: "/Users/sam/src/api",
            managed: true, capabilities: [Capability.git, Capability.gitWrite]
        )
    }

    private func store() -> AppStore {
        AppStore(services: FakeAppServices(), controlCenter: Samples.controlCenter)
    }

    // MARK: Navigation across layouts

    @Test func unfoldMovesTheOpenDiffBesideTheList() {
        var navigation = ReviewNavigation(tab: .history)
        navigation.open(.commit(ReviewFixtures.log[0]), layout: .stack)
        navigation.open(.diff(.commit(hash: ReviewFixtures.log[0].hash, path: "a.ts")), layout: .stack)
        navigation.relayout(to: .split)
        #expect(navigation.path == [.commit(ReviewFixtures.log[0])])
        #expect(navigation.detail == .diff(.commit(hash: ReviewFixtures.log[0].hash, path: "a.ts")))
        // A second pass changes nothing.
        navigation.relayout(to: .split)
        #expect(navigation.path.count == 1)
    }

    @Test func foldPushesTheDetailOntoTheStack() {
        var navigation = ReviewNavigation(tab: .changes)
        navigation.open(.diff(.worktree(path: "a.ts")), layout: .split)
        navigation.relayout(to: .stack)
        #expect(navigation.path == [.diff(.worktree(path: "a.ts"))])
        #expect(navigation.detail == nil)
        navigation.relayout(to: .stack)
        #expect(navigation.path.count == 1)
    }

    // MARK: Screen models

    @Test func foldKeepsTheOpenDiffsModel() {
        let session = ReviewSession(subject: subject(), services: FakeReviewServices(), tab: .changes, machine: "Studio")
        let route = ReviewRoute.diff(.worktree(path: "a.ts"))
        session.navigation.open(route, layout: .split)
        let made = session.screen(for: route) { DiffModel.preview(.worktree(path: "a.ts")) }
        session.navigation.relayout(to: .stack)
        let again = session.screen(for: route) { DiffModel.preview(.worktree(path: "a.ts")) }
        #expect(again === made)
    }

    @Test func closedRoutesLetTheirModelGo() {
        let session = ReviewSession(subject: subject(), services: FakeReviewServices(), tab: .changes, machine: "Studio")
        let route = ReviewRoute.diff(.worktree(path: "a.ts"))
        session.navigation.open(route, layout: .stack)
        let made = session.screen(for: route) { DiffModel.preview(.worktree(path: "a.ts")) }
        session.navigation.path = []
        session.navigation.open(route, layout: .stack)
        let fresh = session.screen(for: route) { DiffModel.preview(.worktree(path: "a.ts")) }
        #expect(fresh !== made)
    }

    // MARK: Kept review

    @Test func reviewIsKeptWhileItsPresentationChanges() {
        let store = store()
        let services = FakeReviewServices()
        let first = store.keptReview(for: subject(), services: services, tab: .changes, machine: "Studio", regular: false)
        first.changes.message = "Fix the 404 for unknown ids"
        first.navigation.open(.diff(.worktree(path: "a.ts")), layout: .stack)

        let again = store.keptReview(for: subject(), services: services, tab: .changes, machine: "Studio", regular: true)
        #expect(again === first)
        #expect(again.changes.hasDraft)
        #expect(again.navigation.path == [.diff(.worktree(path: "a.ts"))])
        // The presentation stays the one the review opened with.
        #expect(again.regular == false)
    }

    @Test func anotherSessionGetsAFreshReview() {
        let store = store()
        let services = FakeReviewServices()
        let first = store.keptReview(for: subject(), services: services, tab: .changes, machine: "", regular: false)
        let other = store.keptReview(for: subject("s-02"), services: services, tab: .files, machine: "", regular: true)
        #expect(other !== first)
        #expect(other.navigation.tab == .files)
        #expect(other.regular == true)
    }

    @Test func closingTheReviewLetsGoOfIt() {
        let store = store()
        let services = FakeReviewServices()
        let first = store.keptReview(for: subject(), services: services, tab: .changes, machine: "", regular: false)
        store.reviewClosed()
        let next = store.keptReview(for: subject(), services: services, tab: .changes, machine: "", regular: true)
        #expect(next !== first)
        #expect(next.regular == true)
    }

    // MARK: Presentation

    @Test func presentationFollowsTheReviewAsOpened() {
        #expect(ReviewPresentation.regular(kept: nil, sizeClass: .regular, width: 1000))
        #expect(!ReviewPresentation.regular(kept: nil, sizeClass: .compact, width: 1000))
        // Opened folded, then unfolded: still the cover.
        #expect(!ReviewPresentation.regular(kept: false, sizeClass: .regular, width: 1000))
        // Opened unfolded, then folded: still the sheet.
        #expect(ReviewPresentation.regular(kept: true, sizeClass: .compact, width: 466))
    }

    /// Regular size class in a Split View half (475 pt) is compact by the policy: the cover.
    @Test func aDuoSplitViewHalfGetsTheCover() {
        #expect(!ReviewPresentation.regular(kept: nil, sizeClass: .regular, width: 475))
    }
}
