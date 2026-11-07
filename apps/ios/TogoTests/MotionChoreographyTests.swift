import SwiftUI
import Testing
@testable import Togo

/// The choreography helpers: roles' values, the two-phase disclosure and its reversal guard,
/// the mirrored swap, and the Reduce Motion variants of the transitions.
@MainActor struct MotionChoreographyTests {
    @Test func rolesMatchTheSpec() {
        #expect(Motion.spec(.respond) == MotionSpec(curve: .spring(response: 0.24, damping: 0.92), reduced: .fade(0.12)))
        #expect(Motion.spec(.resize) == MotionSpec(curve: .spring(response: 0.36, damping: 0.90), reduced: .fade(0.15)))
        #expect(Motion.spec(.reveal) == MotionSpec(curve: .easeOut(0.22), reduced: .fade(0.15)))
        #expect(Motion.spec(.conceal) == MotionSpec(curve: .easeOut(0.12), reduced: .fade(0.12)))
        #expect(Motion.spec(.emphasis) == MotionSpec(curve: .spring(response: 0.30, damping: 0.55), reduced: .instant))
        #expect(Motion.spec(.progress) == MotionSpec(curve: .spring(response: 0.50, damping: 1.0), reduced: .fade(0.2)))
        #expect(Motion.spec(.ambient) == MotionSpec(curve: .easeInOut(3.2), reduced: .instant))
    }

    /// Content arrives with its opacity only: no rise, no delay, no stagger. Under Reduce Motion
    /// a surface presenting from its edge only fades.
    @Test func transitionsNeverRise() {
        #expect(Motion.presentMoves(reduce: false))
        #expect(!Motion.presentMoves(reduce: true))
        // They all build.
        for reduce in [false, true] {
            _ = Motion.arrive(reduce: reduce)
            _ = Motion.unfold(reduce: reduce)
            _ = Motion.conceal(reduce: reduce)
            _ = Motion.swap(reduce: reduce)
            _ = Motion.present(from: .top, reduce: reduce)
        }
    }

    /// The shared vocabulary itself never moves content to make it appear and never delays it:
    /// no offset, no delay, no stagger anywhere in the choreography file.
    @Test func vocabularyHasNoRiseOrDelay() throws {
        let code = try MotionLint.source("apps/ios/Togo/Design/MotionChoreography.swift")
            .split(separator: "\n").filter { !$0.trimmingCharacters(in: .whitespaces).hasPrefix("//") }
            .joined(separator: "\n")
        for banned in [".offset(", ".delay(", "revealRise", "swapShift", "revealDelay", "staggerDelay", ".move(edge: .bottom"] {
            #expect(!code.contains(banned), "\(banned)")
        }
    }

    /// A clock is never animated: monospaced digits, no content transition, and any inherited
    /// animation stripped. The rolling forwarder and the old reveals are gone.
    @Test func clockTextStripsAnimation() throws {
        let code = try MotionLint.source("apps/ios/Togo/Design/MotionChoreography.swift")
        let clock = try #require(code.range(of: "func clockText() -> some View {"))
        let body = code[clock.upperBound...].prefix(while: { $0 != "}" })
        #expect(body.contains("monospacedDigit()"))
        #expect(body.contains(".contentTransition(.identity)"))
        #expect(code[clock.upperBound...].contains(".transaction { $0.animation = nil }"))
        for retired in ["func rollingNumber(", "func revealOnAppear(", "func revealConceal(", "func reveal("] {
            #expect(!code.contains(retired), "\(retired)")
        }
        _ = Text(verbatim: "1m 9s").clockText()
    }

    /// A phase shown for under 250 ms is replaced at once; one shown longer crossfades.
    @Test func phaseSwapIsInstantUnder250ms() {
        #expect(!PhaseSwapTiming.crossfades(shownFor: 0))
        #expect(!PhaseSwapTiming.crossfades(shownFor: 0.1))
        #expect(!PhaseSwapTiming.crossfades(shownFor: 0.249))
        #expect(PhaseSwapTiming.crossfades(shownFor: 0.25))
        #expect(PhaseSwapTiming.crossfades(shownFor: 2))
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
