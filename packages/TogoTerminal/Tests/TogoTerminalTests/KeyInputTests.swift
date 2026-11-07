import Foundation
import Testing
import UIKit
@testable import TogoTerminal

/// Modifiers × keys to bytes, in plain xterm-256color (no kitty, no modifyOtherKeys).
@Suite struct KeyMatrixTests {
    static let e = "\u{1b}"

    static let table: [(KeyStroke, Bool, String)] = [
        (KeyStroke(.tab), false, "\t"),
        (KeyStroke(.tab, .shift), false, "\(e)[Z"),
        (KeyStroke(.tab, .alt), false, "\(e)\t"),
        (KeyStroke(.tab, [.shift, .alt]), false, "\(e)\(e)[Z"),
        (KeyStroke(.enter), false, "\r"),
        (KeyStroke(.enter, .shift), false, "\(e)\r"),
        (KeyStroke(.enter, .alt), false, "\(e)\r"),
        (KeyStroke(.enter, [.shift, .alt]), false, "\(e)\r"),
        (KeyStroke(.backspace), false, "\u{7f}"),
        (KeyStroke(.backspace, .shift), false, "\u{7f}"),
        (KeyStroke(.backspace, .ctrl), false, "\u{08}"),
        (KeyStroke(.backspace, .alt), false, "\(e)\u{7f}"),
        (KeyStroke(.up), false, "\(e)[A"),
        (KeyStroke(.up), true, "\(e)OA"),
        (KeyStroke(.up, .shift), false, "\(e)[1;2A"),
        (KeyStroke(.up, .shift), true, "\(e)[1;2A"),
        (KeyStroke(.down, .alt), false, "\(e)[1;3B"),
        (KeyStroke(.right, [.shift, .alt]), false, "\(e)[1;4C"),
        (KeyStroke(.left, .ctrl), false, "\(e)[1;5D"),
        (KeyStroke(.left, [.ctrl, .shift]), false, "\(e)[1;6D"),
        (KeyStroke(.left, [.ctrl, .alt, .shift]), false, "\(e)[1;8D"),
        (KeyStroke(.home), false, "\(e)[H"),
        (KeyStroke(.home, .shift), false, "\(e)[1;2H"),
        (KeyStroke(.end, .ctrl), false, "\(e)[1;5F"),
        (KeyStroke(.delete, .shift), false, "\(e)[3;2~"),
        (KeyStroke(.char("a")), false, "a"),
        (KeyStroke(.char("a"), .shift), false, "A"),
        (KeyStroke(.char("a"), .ctrl), false, "\u{01}"),
        (KeyStroke(.char("a"), [.ctrl, .shift]), false, "\u{01}"),
        (KeyStroke(.char("c"), [.ctrl, .alt]), false, "\(e)\u{03}"),
        (KeyStroke(.char("a"), .alt), false, "\(e)a"),
        (KeyStroke(.char("a"), [.alt, .shift]), false, "\(e)A"),
    ]

    @Test func everyModifierCombinesWithEveryKey() {
        for (stroke, applicationCursor, expected) in Self.table {
            let bytes = KeyEncoder.encode(stroke, applicationCursor: applicationCursor, mode: .legacy)
            #expect(String(decoding: bytes, as: UTF8.self) == expected, "\(stroke) app=\(applicationCursor)")
        }
    }

    @Test func shiftEnterInExtendedModes() {
        let e = Self.e
        let kitty = KeyEncoder.encode(KeyStroke(.enter, .shift), applicationCursor: false, mode: .kitty)
        let xterm = KeyEncoder.encode(KeyStroke(.enter, .shift), applicationCursor: false, mode: .modifyOtherKeys)
        #expect(String(decoding: kitty, as: UTF8.self) == "\(e)[13;2u")
        #expect(String(decoding: xterm, as: UTF8.self) == "\(e)[27;2;13~")
    }
}

@MainActor
@Suite struct LatchStateTests {
    @MainActor final class Keyboard {
        var shifted = false
        var released = 0
    }

    func latch(_ clock: @escaping () -> Date = { Date(timeIntervalSince1970: 0) }) -> ModifierLatch {
        ModifierLatch(now: clock)
    }

    @Test func tapArmsDoubleTapLocksThirdTapReleases() {
        let l = latch()
        l.tap(.shift)
        #expect(l.shift == .once)
        l.tap(.shift)
        #expect(l.shift == .locked)
        l.tap(.shift)
        #expect(l.shift == .off)
    }

    @Test func slowSecondTapDisarms() {
        var now = Date(timeIntervalSince1970: 0)
        let l = latch { now }
        l.tap(.alt)
        now += ModifierLatch.doubleTapWindow + 0.1
        l.tap(.alt)
        #expect(l.alt == .off)
    }

    @Test func longPressLocksFromAnyState() {
        let l = latch()
        l.lock(.ctrl)
        #expect(l.ctrl == .locked)
        l.tap(.ctrl)
        #expect(l.ctrl == .off)
    }

    @Test func oneShotClearsAfterOneKeyLockedStays() {
        let l = latch()
        l.tap(.shift)
        l.lock(.ctrl)
        #expect(l.consume(KeyStroke(.tab)) == KeyStroke(.tab, [.shift, .ctrl]))
        #expect(l.shift == .off)
        #expect(l.consume(KeyStroke(.up)) == KeyStroke(.up, .ctrl))
        #expect(l.ctrl == .locked)
    }

    @Test func modifiersCombine() {
        let l = latch()
        l.tap(.ctrl)
        l.tap(.alt)
        l.tap(.shift)
        #expect(l.consume(KeyStroke(.left)) == KeyStroke(.left, [.ctrl, .alt, .shift]))
        #expect(!l.isActive)
    }

    @Test func systemShiftJoinsBarKeysAndIsReleased() {
        let keyboard = Keyboard()
        let l = latch()
        l.softShift = SoftShift(isShifted: { keyboard.shifted }, release: { keyboard.shifted = false; keyboard.released += 1 })
        keyboard.shifted = true
        #expect(l.consume(KeyStroke(.tab)) == KeyStroke(.tab, .shift))
        #expect(keyboard.released == 1)
        #expect(l.consume(KeyStroke(.tab)) == KeyStroke(.tab))

        keyboard.shifted = true
        l.tap(.ctrl)
        #expect(l.consume(KeyStroke(.up)) == KeyStroke(.up, [.ctrl, .shift]))
    }

    @Test func typedReturnWithSystemShiftIsShiftEnter() {
        let keyboard = Keyboard()
        let l = latch()
        l.softShift = SoftShift(isShifted: { keyboard.shifted }, release: { keyboard.shifted = false })
        #expect(l.stroke(forTyped: "\n") == nil)
        #expect(l.stroke(forTyped: "a") == nil)
        keyboard.shifted = true
        #expect(l.stroke(forTyped: "\n") == KeyStroke(.enter, .shift))
        #expect(!keyboard.shifted)
    }

    @Test func typedTextTakesLatchOnlyForOneCharacter() {
        let l = latch()
        l.tap(.ctrl)
        #expect(l.stroke(forTyped: "hello") == nil)
        #expect(l.ctrl == .once)
        #expect(l.stroke(forTyped: "c") == KeyStroke(.char("c"), .ctrl))
        #expect(l.ctrl == .off)
        l.tap(.shift)
        #expect(l.apply(to: Array("\n".utf8)) == [0x1b, 0x0d])
    }
}

@MainActor
@Suite struct RepeatTimingTests {
    @Test func startsAfterHalfASecondAndSpeedsUp() {
        #expect(KeyRepeatTiming.delay(beforeRepeat: 0) == 0.5)
        #expect(KeyRepeatTiming.delay(beforeRepeat: 1) == 0.1)
        #expect(KeyRepeatTiming.delay(beforeRepeat: 10) == 0.1)
        #expect(KeyRepeatTiming.delay(beforeRepeat: 11) < 0.1)
        #expect(KeyRepeatTiming.delay(beforeRepeat: 100) < KeyRepeatTiming.delay(beforeRepeat: 11))
        let delays = (0..<60).map(KeyRepeatTiming.delay(beforeRepeat:))
        #expect(zip(delays, delays.dropFirst()).allSatisfy { $0 >= $1 })
    }

    @Test func repeaterFollowsTheCurveAndStopsOnRelease() {
        var pending: [(TimeInterval, @MainActor () -> Void)] = []
        let repeater = ChordRepeater(
            schedule: { delay, body in pending.append((delay, body)); return {} },
            timing: KeyRepeatTiming.delay(beforeRepeat:))
        var sent: [[UInt8]] = []
        let finger = LatchStateTests.Keyboard()
        finger.shifted = true
        repeater.send = { sent.append($0) }
        repeater.start(key: 1, bytes: [0x7f], isHeld: { finger.shifted })
        var delays: [TimeInterval] = []
        for _ in 0..<12 {
            let (delay, body) = pending.removeFirst()
            delays.append(delay)
            body()
        }
        #expect(delays.first == 0.5)
        #expect(delays[1] == 0.1)
        #expect(delays[11] < 0.1)
        #expect(sent.count == 12)
        finger.shifted = false
        pending.removeFirst().1()
        #expect(sent.count == 12)
        #expect(pending.isEmpty)
        #expect(repeater.heldKey == nil)
    }
}

@Suite struct TrackpadAxisTests {
    @Test func oneRowOfTravelIsOneArrow() {
        var rows = FloatingCursorRows()
        rows.begin(at: CGPoint(x: 0, y: 100))
        #expect(rows.move(to: CGPoint(x: 0, y: 110), rowHeight: 20) == 0)
        #expect(rows.move(to: CGPoint(x: 0, y: 121), rowHeight: 20) == 1)
        #expect(rows.move(to: CGPoint(x: 0, y: 181), rowHeight: 20) == 3)
        #expect(rows.move(to: CGPoint(x: 0, y: 100), rowHeight: 20) == -4)
    }

    @Test func horizontalTravelAloneMovesNoRows() {
        var rows = FloatingCursorRows()
        rows.begin(at: CGPoint(x: 0, y: 50))
        #expect(rows.move(to: CGPoint(x: 300, y: 55), rowHeight: 18) == 0)
        #expect(rows.move(to: CGPoint(x: -300, y: 45), rowHeight: 18) == 0)
    }

    @Test func tinyRowsStillNeedAMinimumTravel() {
        var rows = FloatingCursorRows()
        rows.begin(at: .zero)
        #expect(rows.move(to: CGPoint(x: 0, y: FloatingCursorRows.minimumStep - 1), rowHeight: 4) == 0)
        #expect(rows.move(to: CGPoint(x: 0, y: FloatingCursorRows.minimumStep), rowHeight: 4) == 1)
    }

    @Test func nothingBeforeBeginOrAfterEnd() {
        var rows = FloatingCursorRows()
        #expect(rows.move(to: CGPoint(x: 0, y: 500), rowHeight: 20) == 0)
        rows.begin(at: .zero)
        rows.end()
        #expect(rows.move(to: CGPoint(x: 0, y: 500), rowHeight: 20) == 0)
    }
}
