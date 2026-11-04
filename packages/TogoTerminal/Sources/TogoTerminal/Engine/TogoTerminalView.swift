internal import SwiftTerm
import UIKit

/// SwiftTerm's view with Togo's one-finger scroll routing.
///
/// SwiftTerm installs its own one-finger wheel pan while the remote tracks the mouse. It caps
/// reports per gesture and, on an alternate screen without mouse tracking, turns the drag into
/// cursor keys, which reach an agent's prompt as history navigation. Togo routes that pan
/// through `ScrollRouter` instead, so SwiftTerm's recognizer is installed but kept disabled.
///
/// Text input goes through SwiftTerm's `UITextInput` path, which handles IME and CJK
/// composition. Togo steps in only for a typed key while a key bar modifier is latched, and
/// never while text is marked.
final class TogoTerminalView: TerminalView {
    /// Called with the vertical travel since the last call and the touch location in bounds.
    var onWheelPan: ((CGFloat, CGPoint) -> Void)?
    var onWheelPanEnded: (() -> Void)?
    /// Asked when a one-finger pan starts; true when the remote should get wheel events.
    var capturesWheelPan: (() -> Bool)?
    /// Bytes for typed text while a modifier is latched; nil passes the text to SwiftTerm.
    var latchedInput: ((String) -> [UInt8]?)?
    var onShortcut: ((ShortcutAction) -> Void)?
    let chordRepeater: ChordRepeater

    private let wheelPan = UIPanGestureRecognizer()

    override init(frame: CGRect, font: UIFont?, options: TerminalOptions) {
        chordRepeater = ChordRepeater()
        super.init(frame: frame, font: font, options: options)
        chordRepeater.send = { [weak self] bytes in self?.send(data: bytes[...]) }
        wheelPan.maximumNumberOfTouches = 1
        wheelPan.addTarget(self, action: #selector(handleWheelPan(_:)))
        addGestureRecognizer(wheelPan)
    }

    required init?(coder: NSCoder) {
        nil
    }

    override func addGestureRecognizer(_ gestureRecognizer: UIGestureRecognizer) {
        if gestureRecognizer !== wheelPan,
           let pan = gestureRecognizer as? UIPanGestureRecognizer,
           pan.maximumNumberOfTouches == 1 {
            pan.isEnabled = false
        }
        super.addGestureRecognizer(gestureRecognizer)
    }

    override func gestureRecognizerShouldBegin(_ gestureRecognizer: UIGestureRecognizer) -> Bool {
        if gestureRecognizer === wheelPan {
            let velocity = wheelPan.velocity(in: self)
            return abs(velocity.y) >= abs(velocity.x) && (capturesWheelPan?() ?? false)
        }
        return super.gestureRecognizerShouldBegin(gestureRecognizer)
    }

    override func insertText(_ text: String) {
        if markedTextRange == nil, let bytes = latchedInput?(text) {
            if !bytes.isEmpty { send(data: bytes[...]) }
            return
        }
        super.insertText(text)
    }

    /// Ctrl and Option-as-Meta chords are encoded by `KeyEncoder.hardware`, so hardware and
    /// key bar chords send the same bytes. With the kitty keyboard protocol on, or during
    /// composition, SwiftTerm keeps every press because it must report them its own way.
    override func pressesBegan(_ presses: Set<UIPress>, with event: UIPressesEvent?) {
        // Any new key ends a held chord, as it does for SwiftTerm's own repeat.
        chordRepeater.stop()
        let kitty = !(terminalInputStateSnapshot()?.keyboardEnhancementFlags.isEmpty ?? true)
        guard !kitty, markedTextRange == nil else {
            super.pressesBegan(presses, with: event)
            return
        }
        let unhandled = presses.filter { press in
            guard let key = press.key else { return true }
            // SwiftTerm's pressesEnded is public, not open, so release is read from the press
            // itself, which UIKit keeps updating for the life of the keypress.
            return !handleChord(
                characters: key.characters, charactersIgnoringModifiers: key.charactersIgnoringModifiers,
                modifiers: key.modifierFlags, keyCode: key.keyCode.rawValue,
                isHeld: { [weak press] in press.map { $0.phase != .ended && $0.phase != .cancelled } ?? false })
        }
        if !unhandled.isEmpty { super.pressesBegan(unhandled, with: event) }
    }

    /// Sends a chord Togo encodes and starts its repeat; false leaves the press to SwiftTerm.
    func handleChord(
        characters: String, charactersIgnoringModifiers: String, modifiers: UIKeyModifierFlags, keyCode: Int,
        isHeld: @escaping @MainActor () -> Bool
    ) -> Bool {
        guard let bytes = KeyEncoder.hardware(
            characters: characters, charactersIgnoringModifiers: charactersIgnoringModifiers,
            modifiers: modifiers, optionAsMeta: optionAsMetaKey) else { return false }
        send(data: bytes[...])
        chordRepeater.start(key: keyCode, bytes: bytes, isHeld: isHeld)
        return true
    }

    /// A key released after focus moves never reaches this view, so the repeat ends here.
    override func resignFirstResponder() -> Bool {
        chordRepeater.stop()
        return super.resignFirstResponder()
    }

    override var keyCommands: [UIKeyCommand]? {
        (super.keyCommands ?? []) + HardwareShortcuts.commands(action: #selector(performShortcut(_:)))
    }

    /// SwiftTerm's `canPerformAction` is public, not open, and refuses selectors it does not
    /// know, so the shortcut action is claimed here instead.
    override func target(forAction action: Selector, withSender sender: Any?) -> Any? {
        action == #selector(performShortcut(_:)) ? self : super.target(forAction: action, withSender: sender)
    }

    @objc func performShortcut(_ command: UIKeyCommand) {
        guard let action = ShortcutAction(command: command) else { return }
        onShortcut?(action)
    }

    @objc private func handleWheelPan(_ gesture: UIPanGestureRecognizer) {
        switch gesture.state {
        case .changed:
            let travel = gesture.translation(in: self).y
            gesture.setTranslation(.zero, in: self)
            onWheelPan?(travel, gesture.location(in: self))
        case .ended, .cancelled, .failed:
            onWheelPanEnded?()
        default:
            break
        }
    }
}
