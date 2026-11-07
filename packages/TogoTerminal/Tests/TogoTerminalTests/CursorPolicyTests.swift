import Testing
@testable import TogoTerminal

struct CursorPolicyTests {
    @Test func focusedTerminalBlinks() {
        #expect(CursorPolicy.blinks(requested: true, focused: true, reduceMotion: false, prefersNonBlinking: false))
    }

    @Test func unfocusedTerminalStaysStill() {
        #expect(!CursorPolicy.blinks(requested: true, focused: false, reduceMotion: false, prefersNonBlinking: false))
    }

    @Test func reduceMotionAndNonBlinkingPreferenceKeepItSolid() {
        #expect(!CursorPolicy.blinks(requested: true, focused: true, reduceMotion: true, prefersNonBlinking: false))
        #expect(!CursorPolicy.blinks(requested: true, focused: true, reduceMotion: false, prefersNonBlinking: true))
    }

    @Test func settingOffKeepsItSolid() {
        #expect(!CursorPolicy.blinks(requested: false, focused: true, reduceMotion: false, prefersNonBlinking: false))
    }

    @Test func stepIsOneSecond() {
        #expect(CursorPolicy.period == 1)
    }
}
