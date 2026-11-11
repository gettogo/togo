import Foundation
import Testing
import UIKit
@testable import TogoTerminal

/// A tap on the terminal, or Show keyboard, brings the keyboard (owner: "in terminal mode
/// there's no way to open the keyboard"), including when the terminal kept focus while its
/// keyboard went away; a long press still selects and a drag still scrolls.
@MainActor
@Suite struct TerminalFocusTests {
    @Test func aTerminalWithoutFocusTakesIt() {
        #expect(TerminalFocusPolicy.action(accepts: true, covered: false, focused: false, keyboardShown: false, hardwareKeyboard: false) == .focus)
        #expect(TerminalFocusPolicy.action(accepts: true, covered: false, focused: false, keyboardShown: false, hardwareKeyboard: true) == .focus)
    }

    /// The bug: focused, keyboard gone. Asking again did nothing; now focus is taken anew.
    @Test func focusedWithoutAKeyboardTakesFocusAgain() {
        #expect(TerminalFocusPolicy.action(accepts: true, covered: false, focused: true, keyboardShown: false, hardwareKeyboard: false) == .refocus)
    }

    @Test func aKeyboardAlreadyTypingHereNeedsNothing() {
        #expect(TerminalFocusPolicy.action(accepts: true, covered: false, focused: true, keyboardShown: true, hardwareKeyboard: false) == .none)
        #expect(TerminalFocusPolicy.action(accepts: true, covered: false, focused: true, keyboardShown: false, hardwareKeyboard: true) == .none)
    }

    @Test func coveredOrNotLiveWaits() {
        #expect(TerminalFocusPolicy.action(accepts: false, covered: false, focused: false, keyboardShown: false, hardwareKeyboard: false) == .wait)
        #expect(TerminalFocusPolicy.action(accepts: true, covered: true, focused: false, keyboardShown: false, hardwareKeyboard: false) == .wait)
    }

    /// Hide keyboard: nothing the app does on its own (appearing, a pane move, uncovering)
    /// raises the keyboard again; a tap or Show keyboard does.
    @Test func hiddenStaysHiddenUntilTheUserAsks() {
        for focused in [true, false] {
            #expect(TerminalFocusPolicy.action(accepts: true, covered: false, focused: focused, keyboardShown: false, hardwareKeyboard: false, dismissed: true, automatic: true) == .none)
        }
        #expect(TerminalFocusPolicy.action(accepts: true, covered: false, focused: false, keyboardShown: false, hardwareKeyboard: false, dismissed: true, automatic: false) == .focus)
        #expect(TerminalFocusPolicy.action(accepts: true, covered: false, focused: false, keyboardShown: false, hardwareKeyboard: false, dismissed: false, automatic: true) == .focus)
    }

    @Test func dismissingMarksTheKeyboardDismissedUntilTheUserAsks() {
        let h = EngineHarness()
        #expect(!h.engine.keyboardDismissed)
        h.engine.endEditing()
        #expect(h.engine.keyboardDismissed)
        h.engine.showKeyboard(automatic: true)
        #expect(h.engine.keyboardDismissed, "an automatic raise does not undo Hide keyboard")
        h.engine.showKeyboard()
        #expect(!h.engine.keyboardDismissed)
    }

    @Test func hideRejectsUIKitResponderRestoration() {
        let h = EngineHarness()
        let view = h.engine.terminalView
        h.engine.endEditing()
        #expect(!view.canBecomeFirstResponder)
        #expect(!view.becomeFirstResponder(), "popover dismissal must not restore a hidden keyboard")
        #expect(!view.wantsFocus, "a rejected restoration must not queue an automatic raise")
        #expect(h.engine.keyboardDismissed)
        h.engine.showKeyboard()
        #expect(!h.engine.keyboardDismissed, "an explicit request unlocks keyboard focus")
    }

    // MARK: Arbitration

    private func controller(_ h: EngineHarness) -> (GestureController, UIView) {
        let host = UIView(frame: CGRect(x: 0, y: 0, width: 480, height: 320))
        host.addSubview(h.engine.view)
        let gestures = GestureController(engine: h.engine, view: host, bindings: GestureConfig())
        return (gestures, host)
    }

    private func recognizers(on view: UIView) -> [UIGestureRecognizer] { view.gestureRecognizers ?? [] }

    /// SwiftTerm's own single tap (the one that focuses) waits only for Togo's taps, never for
    /// the long press or a pan: a tap is never held up by selection.
    @Test func theFocusingTapWaitsOnlyForTaps() throws {
        let h = EngineHarness()
        let (gestures, host) = controller(h)
        let theirs = try #require(recognizers(on: h.engine.gestureView).first {
            ($0 as? UITapGestureRecognizer)?.numberOfTapsRequired == 1 && ($0 as? UITapGestureRecognizer)?.numberOfTouchesRequired == 1
        })
        for mine in recognizers(on: host) where mine.delegate === gestures {
            let waits = gestures.gestureRecognizer(mine, shouldBeRequiredToFailBy: theirs)
            if !(mine is UITapGestureRecognizer) { #expect(!waits, "\(type(of: mine)) must not hold up a tap") }
        }
        withExtendedLifetime(gestures) {}
    }

    /// The long press beats SwiftTerm's own (its Select / Select All menu) and the selection
    /// drag beats scrolling, so a press selects and a drag from a handle extends.
    @Test func pressAndHandleDragTakePrecedence() throws {
        let h = EngineHarness()
        let (gestures, host) = controller(h)
        let press = try #require(recognizers(on: host).first { $0 is UILongPressGestureRecognizer && $0.delegate === gestures })
        let theirPress = try #require(recognizers(on: h.engine.gestureView).first { $0 is UILongPressGestureRecognizer })
        #expect(gestures.gestureRecognizer(press, shouldBeRequiredToFailBy: theirPress))
        // The touch selection's drag: a plain one-finger pan (the edge pan is a subclass).
        let drag = try #require(recognizers(on: host).first {
            type(of: $0) == UIPanGestureRecognizer.self && $0.delegate === gestures
        })
        #expect(gestures.gestureRecognizer(drag, shouldBeRequiredToFailBy: h.engine.scrollPan))
        // Without a selection the drag never starts, so scrolling goes ahead at once.
        #expect(!gestures.gestureRecognizerShouldBegin(drag))
        withExtendedLifetime(gestures) {}
    }
}
