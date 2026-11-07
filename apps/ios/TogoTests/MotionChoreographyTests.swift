import SwiftUI
import Testing
@testable import Togo

/// The choreography helpers: roles' values, the two-phase disclosure and its reversal guard,
/// the mirrored swap, and the Reduce Motion variants of the transitions.
@MainActor struct MotionChoreographyTests {
    @Test func newRolesMatchTheSpec() {
        #expect(Motion.spec(.respond) == MotionSpec(curve: .spring(response: 0.24, damping: 0.92), reduced: .fade(0.12)))
        #expect(Motion.spec(.resize) == MotionSpec(curve: .spring(response: 0.36, damping: 0.90), reduced: .fade(0.15)))
        #expect(Motion.spec(.reveal) == MotionSpec(curve: .easeOut(0.22), reduced: .fade(0.15)))
        #expect(Motion.spec(.conceal) == MotionSpec(curve: .easeOut(0.12), reduced: .fade(0.12)))
        #expect(Motion.spec(.emphasis) == MotionSpec(curve: .spring(response: 0.30, damping: 0.55), reduced: .instant))
        #expect(Motion.spec(.progress) == MotionSpec(curve: .spring(response: 0.50, damping: 1.0), reduced: .fade(0.2)))
        #expect(Motion.spec(.ambient) == MotionSpec(curve: .easeInOut(3.2), reduced: .instant))
    }

    @Test func revealStartsABeatAfterTheResize() {
        #expect(Motion.revealAnimation(reduce: false) == .easeOut(duration: 0.22).delay(0.06))
        #expect(Motion.revealAnimation(reduce: false, index: 2) == .easeOut(duration: 0.22).delay(0.12))
        // Under Reduce Motion: a plain fade, no delay, no stagger.
        #expect(Motion.revealAnimation(reduce: true) == .easeOut(duration: 0.15))
        #expect(Motion.revealAnimation(reduce: true, index: 4) == .easeOut(duration: 0.15))
    }

    @Test func staggerIsCapped() {
        #expect(Motion.staggerDelay(0) == 0)
        #expect(abs(Motion.staggerDelay(3) - 0.09) < 1e-9)
        #expect(Motion.staggerDelay(5) == 0.15)
        #expect(Motion.staggerDelay(40) == 0.15)
        #expect(Motion.staggerDelay(-1) == 0)
    }

    /// Under Reduce Motion transitions keep opacity and drop every movement.
    @Test func transitionsOnlyFadeUnderReduceMotion() {
        #expect(Motion.revealRise(reduce: false) == 6)
        #expect(Motion.revealRise(reduce: true) == 0)
        #expect(Motion.swapShift(reduce: false) == 4)
        #expect(Motion.swapShift(reduce: true) == 0)
        #expect(Motion.presentMoves(reduce: false))
        #expect(!Motion.presentMoves(reduce: true))
        // They all build.
        for reduce in [false, true] {
            _ = Motion.reveal(reduce: reduce)
            _ = Motion.conceal(reduce: reduce)
            _ = Motion.revealConceal(reduce: reduce)
            _ = Motion.swap(reduce: reduce)
            _ = Motion.present(from: .top, reduce: reduce)
        }
    }

    @Test func disclosureOpensInOneStep() {
        var disclosure = Disclosure()
        #expect(!disclosure.isOpen)
        let step = disclosure.begin()
        #expect(step == .open)
        disclosure.applyOpen()
        #expect(disclosure.open && disclosure.shown && disclosure.isOpen)
    }

    @Test func disclosureClosesContentFirst() {
        var disclosure = Disclosure(open: true)
        let step = disclosure.begin()
        guard case let .close(generation) = step else {
            Issue.record("expected a close, got \(step)")
            return
        }
        disclosure.applyConceal()
        // Concealed but still laid out: the container has not shrunk yet.
        #expect(disclosure.open && !disclosure.shown)
        #expect(!disclosure.isOpen)
        let shrank = disclosure.finishClose(generation: generation)
        #expect(shrank)
        #expect(!disclosure.open)
    }

    /// A tap during the conceal reopens, and the stale shrink must not collapse it.
    @Test func reversalDropsTheStaleShrink() {
        var disclosure = Disclosure(open: true)
        guard case let .close(stale) = disclosure.begin() else {
            Issue.record("expected a close")
            return
        }
        disclosure.applyConceal()
        let reopen = disclosure.begin()
        #expect(reopen == .open)
        disclosure.applyOpen()
        let shrank = disclosure.finishClose(generation: stale)
        #expect(!shrank)
        #expect(disclosure.isOpen)
    }

    /// Closing, reopening and closing again: only the latest shrink applies.
    @Test func onlyTheLatestCloseShrinks() {
        var disclosure = Disclosure(open: true)
        guard case let .close(first) = disclosure.begin() else { return }
        disclosure.applyConceal()
        _ = disclosure.begin()
        disclosure.applyOpen()
        guard case let .close(second) = disclosure.begin() else {
            Issue.record("expected a close")
            return
        }
        disclosure.applyConceal()
        let staleShrank = disclosure.finishClose(generation: first)
        #expect(!staleShrank)
        #expect(disclosure.open)
        let shrank = disclosure.finishClose(generation: second)
        #expect(shrank)
        #expect(!disclosure.open)
    }

    @Test func toggleDrivesTheBinding() {
        var state = Disclosure()
        let binding = Binding(get: { state }, set: { state = $0 })
        Motion.toggle(binding, reduce: true)
        #expect(state.isOpen)
        Motion.toggle(binding, reduce: true)
        // The shrink waits for the conceal; the content reads closed at once.
        #expect(!state.isOpen)
    }

    @Test func settleSmallSwapsAtOnce() {
        var state = Mirrored("waiting")
        let binding = Binding(get: { state }, set: { state = $0 })
        Motion.settle(binding, to: "working", small: true, reduce: false)
        #expect(state.value == "working" && state.shown)
        // The same value is a no-op.
        let generation = state.generation
        Motion.settle(binding, to: "working", reduce: false)
        #expect(state.generation == generation)
    }
}
