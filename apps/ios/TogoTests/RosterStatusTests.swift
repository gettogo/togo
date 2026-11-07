import Testing
import TogoKit
@testable import Togo

struct RosterStatusTests {
    @Test func statesFollowTheSessionStatus() {
        #expect(RosterState(.needsYou) == .needsYou)
        #expect(RosterState(.working) == .working)
        #expect(RosterState(.done) == .done)
        #expect(RosterState(.idle) == .idle)
        #expect(RosterState(.exited) == .ended)
        #expect(RosterState(.ended) == .ended)
        #expect(RosterState(.failed) == .failed)
        #expect(RosterState(.unknown("waiting_on_ci")) == .idle)
    }

    @Test func wordsMatchTheStatusWords() {
        #expect(StatusWord.label(.needsYou) == "Needs you")
        #expect(StatusWord.label(.working) == "Working")
        #expect(StatusWord.label(.done) == "Done")
        #expect(StatusWord.label(.idle) == "Idle")
    }

    @MainActor @Test func workingIsAStillSpinnerUnlessAnimated() {
        #expect(RosterStatus.glyph(for: .working, animated: false) == "\u{283F}")
        #expect(RosterStatus.glyph(for: .working, animated: false, at: 0.5) == "\u{283F}")
        let frames = Set([0.0, 0.15, 0.35, 0.55, 0.75, 0.95].compactMap {
            RosterStatus.glyph(for: .working, animated: true, at: $0)
        })
        #expect(frames.count > 1)
        #expect(!frames.contains("\u{283F}"))
    }

    @Test func theSpinnerRepeatsEverySecond() {
        #expect(
            RosterStatus.glyph(for: .working, animated: true, at: 0.35)
                == RosterStatus.glyph(for: .working, animated: true, at: 3.35))
        #expect(RosterStatus.spinnerLoop == 1)
    }

    @Test func doneIsThePromptAndNothingElseHasAGlyph() {
        #expect(RosterStatus.glyph(for: .done, animated: true) == ">")
        for state in [RosterState.needsYou, .connecting, .idle, .ended, .failed] {
            #expect(RosterStatus.glyph(for: state, animated: true) == nil)
        }
    }
}
