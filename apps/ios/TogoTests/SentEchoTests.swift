import Foundation
import Testing
@testable import Togo

/// After Send the field is empty and stays empty: the keyboard's dictation, still listening when
/// Send was tapped, does not put the sent words back.
@MainActor
struct SentEchoTests {
    private let start = ContinuousClock.now
    private var echo: SentEcho { SentEcho(text: "fix the failing test", at: start) }

    @Test func theSameWordsLandingInTheEmptyFieldAreDropped() {
        #expect(echo.outcome(of: "fix the failing test", previous: "", now: start + .seconds(1)) == .echo(kept: ""))
    }

    @Test func theFinalResultDiffersOnlyInCaseAndPunctuation() {
        #expect(echo.outcome(of: "Fix the failing test.", previous: "", now: start + .seconds(1)) == .echo(kept: ""))
    }

    @Test func wordsSaidAfterTheSendStay() {
        #expect(echo.outcome(of: "fix the failing test. Then run it", previous: "", now: start + .seconds(1))
            == .echo(kept: "Then run it"))
    }

    @Test func somethingElseStands() {
        #expect(echo.outcome(of: "f", previous: "", now: start + .seconds(1)) == .other)
        #expect(echo.outcome(of: "a new message", previous: "", now: start + .seconds(1)) == .other)
    }

    @Test func typingAfterTheSendIsNeverFiltered() {
        #expect(echo.outcome(of: "fix the failing test", previous: "fix the failing tes", now: start + .seconds(1)) == .other)
    }

    @Test func theWatchEndsAfterItsWindow() {
        #expect(echo.outcome(of: "fix the failing test", previous: "", now: start + .seconds(6)) == .other)
    }

    @Test func anEmptyFieldKeepsTheWatch() {
        #expect(echo.outcome(of: "", previous: "", now: start + .seconds(1)) == .waiting)
    }
}
