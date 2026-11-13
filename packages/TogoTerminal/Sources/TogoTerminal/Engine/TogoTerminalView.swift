internal import SwiftTerm
import Synchronization
import UIKit
import Accessibility

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
    /// The selection text after a Copy, so it reaches the engine's clipboard hook.
    var onCopy: ((String) -> Void)?
    /// Edit menu Paste, so it takes the same sanitised, bracketed path as every other paste.
    var onPaste: ((String) -> Void)?
    var pasteboardText: () -> String? = { UIPasteboard.general.string }
    /// Edit menu Paste when the pasteboard has no text, so a screenshot takes the image path.
    var onPasteWithoutText: (() -> Void)?
    /// Asked per hardware press; kitty mode never gets here, SwiftTerm reports those keys.
    var extendedKeyMode: (() -> ExtendedKeyMode)?
    let chordRepeater: ChordRepeater
    var onFocusChange: (() -> Void)?
    /// The user typed or pressed a key here; the terminal's own replies do not count.
    var onUserInput: (() -> Void)?
    /// VoiceOver's value for the view: the cursor line, or nil.
    var cursorLine: (() -> String?)?
    /// Kept from the parser thread: an app hid the cursor (DECTCEM), so its row means nothing.
    let cursorHidden = Atomic(false)

    /// Bytes for a held Backspace while a modifier is latched (Alt-Backspace deletes a word).
    var latchedBackspace: (() -> [UInt8]?)?
    /// The keyboard trackpad moved this many rows; negative is up.
    var onFloatingRows: ((Int) -> Void)?
    var floatingRowHeight: (() -> CGFloat)?
    var floatingRows = FloatingCursorRows()
    var floatingPin: FloatingPin?

    private let wheelPan = UIPanGestureRecognizer()
    private var selectionThroughLineEnd = false

    override init(frame: CGRect, font: UIFont?, options: TerminalOptions) {
        chordRepeater = ChordRepeater()
        super.init(frame: frame, font: font, options: options)
        _ = Self.installFloatingCursor
        chordRepeater.send = { [weak self] bytes in self?.send(data: bytes[...]) }
        wheelPan.maximumNumberOfTouches = 1
        wheelPan.addTarget(self, action: #selector(handleWheelPan(_:)))
        addGestureRecognizer(wheelPan)
        // The grid is sized from the bounds alone. An inset adjusted for a safe area or bar
        // would shift the content sideways and push the last columns past the edge.
        contentInsetAdjustmentBehavior = .never
        alwaysBounceHorizontal = false
        NotificationCenter.default.addObserver(
            self, selector: #selector(keyboardWillShow(_:)), name: UIResponder.keyboardWillShowNotification, object: nil)
        NotificationCenter.default.addObserver(
            self, selector: #selector(keyboardWillHide(_:)), name: UIResponder.keyboardWillHideNotification, object: nil)
        for name in [
            UIAccessibility.reduceMotionStatusDidChangeNotification, UIApplication.willEnterForegroundNotification,
            AccessibilitySettings.prefersNonBlinkingTextInsertionIndicatorDidChangeNotification,
        ] {
            NotificationCenter.default.addObserver(self, selector: #selector(caretEnvironmentChanged), name: name, object: nil)
        }
    }

    /// Whether the cursor may blink at all; the engine sets it from the terminal settings.
    var cursorBlinkRequested = true
    /// The cursor's shape, always a steady style; how it blinks is decided by `refreshCaret`.
    var cursorShape: CursorStyle = .steadyBlock {
        didSet { refreshCaret() }
    }
    /// True while the step is drawn here on SwiftTerm's caret view. When that view cannot be
    /// found, SwiftTerm's own blinking style is used instead, so the cursor never goes missing.
    private let ownsBlink = Mutex(true)
    private static let blinkKey = "togo.caretBlink"
    private static let caretClassName = "CaretView"

    /// SwiftTerm draws the cursor hollow while this view is not first responder. Its own blink
    /// is a slow fade, so the engine keeps the cursor steady and the step is added here: solid
    /// snow on a one second hard step while focused, solid and still under Reduce Motion or
    /// "Prefer Non-Blinking Cursor". SwiftTerm has no public hook for its caret, so the view is
    /// found by class name; if it is missing, the cursor falls back to SwiftTerm's own blink.
    func refreshCaret() {
        // Re-assigning makes SwiftTerm redraw the caret for the current focus and clear its animations.
        caretViewTracksFocus = true
        let caret = subviews.first { String(describing: type(of: $0)) == Self.caretClassName }
        let wantsBlink = CursorPolicy.blinks(
            requested: cursorBlinkRequested, focused: true,
            reduceMotion: UIAccessibility.isReduceMotionEnabled,
            prefersNonBlinking: AccessibilitySettings.prefersNonBlinkingTextInsertionIndicator)
        guard let caret else {
            ownsBlink.withLock { $0 = false }
            setCursorStyle(wantsBlink ? Self.blinking(cursorShape) : cursorShape)
            return
        }
        ownsBlink.withLock { $0 = true }
        setCursorStyle(cursorShape)
        caret.layer.removeAnimation(forKey: Self.blinkKey)
        caret.layer.opacity = 1
        guard wantsBlink, isFirstResponder, window != nil else { return }
        let step = CAKeyframeAnimation(keyPath: "opacity")
        step.values = [1, 0]
        // Discrete keyframes take one more time than values: on at 0, off at 0.5, to the end at 1.
        step.keyTimes = [0, 0.5, 1]
        step.calculationMode = .discrete
        step.duration = CursorPolicy.period
        step.repeatCount = .infinity
        caret.layer.add(step, forKey: Self.blinkKey)
    }

    @objc private func caretEnvironmentChanged() {
        DispatchQueue.main.async { [weak self] in self?.refreshCaret() }
    }

    override func didMoveToWindow() {
        super.didMoveToWindow()
        DispatchQueue.main.async { [weak self] in self?.refreshCaret() }
    }

    /// SwiftTerm rebuilds or restyles its caret on font and size changes, which drops the step;
    /// put it back when it should be running and is not.
    override func layoutSubviews() {
        super.layoutSubviews()
        guard isFirstResponder, window != nil, ownsBlink.withLock({ $0 }),
              let caret = subviews.first(where: { String(describing: type(of: $0)) == Self.caretClassName }),
              caret.layer.animation(forKey: Self.blinkKey) == nil,
              CursorPolicy.blinks(
                  requested: cursorBlinkRequested, focused: true,
                  reduceMotion: UIAccessibility.isReduceMotionEnabled,
                  prefersNonBlinking: AccessibilitySettings.prefersNonBlinkingTextInsertionIndicator)
        else { return }
        refreshCaret()
    }

    /// Remote programs may ask for a blinking cursor; blinking is decided by `refreshCaret`.
    nonisolated override func cursorStyleChanged(source: Terminal, newStyle: CursorStyle) {
        let steady = Self.steady(newStyle)
        let owns = ownsBlink.withLock { $0 }
        super.cursorStyleChanged(source: source, newStyle: owns ? steady : newStyle)
        DispatchQueue.main.async { [weak self] in
            guard let self, self.cursorShape != steady else { return }
            self.cursorShape = steady
        }
    }

    nonisolated static func steady(_ style: CursorStyle) -> CursorStyle {
        switch style {
        case .blinkBlock, .steadyBlock: .steadyBlock
        case .blinkUnderline, .steadyUnderline: .steadyUnderline
        case .blinkBar, .steadyBar: .steadyBar
        }
    }

    static func blinking(_ style: CursorStyle) -> CursorStyle {
        switch style {
        case .blinkBlock, .steadyBlock: .blinkBlock
        case .blinkUnderline, .steadyUnderline: .blinkUnderline
        case .blinkBar, .steadyBar: .blinkBar
        }
    }

    /// VoiceOver focus decides when it is on a terminal, so a split has one focused pane.
    var hasFocus: Bool {
        let voiceOver = UIAccessibility.focusedElement(using: .notificationVoiceOver)
        if let terminal = voiceOver as? TogoTerminalView { return terminal === self }
        return isFirstResponder
    }

    /// SwiftTerm reads the screen line by line; the value says where the cursor is.
    override var accessibilityValue: String? {
        get { cursorLine?() }
        set {}
    }

    nonisolated override func hideCursor(source: Terminal) {
        cursorHidden.store(true, ordering: .relaxed)
        super.hideCursor(source: source)
    }

    nonisolated override func showCursor(source: Terminal) {
        cursorHidden.store(false, ordering: .relaxed)
        super.showCursor(source: source)
    }

    /// False while the session is not connected: nothing can be typed there, so a tap or an
    /// appearance must not raise the keyboard for it.
    var acceptsFocus = true
    /// Something asked for the keyboard while focus was refused, such as the terminal
    /// appearing before its session connected. The keyboard comes up once it may.
    var wantsFocus = false
    /// The user put the keyboard away (Hide keyboard). Nothing but the user brings it back: a tap
    /// on the terminal or Show keyboard clear this; appearing, a pane change or the terminal
    /// uncovering do not.
    var keyboardDismissed = false
    /// Another layer covers the terminal, so it may not take the keyboard until uncovered.
    var covered = false
    /// Counts cover changes, so a delayed hide from an earlier cover does nothing.
    var coverGeneration = 0

    override var canBecomeFirstResponder: Bool {
        acceptsFocus && !covered && !keyboardDismissed && super.canBecomeFirstResponder
    }

    override func becomeFirstResponder() -> Bool {
        #if DEBUG
        TerminalKeyTouchTrace.focusAttempt(dismissed: keyboardDismissed)
        #endif
        // UIKit restores the presenting view's responder after a popover closes. Hide must
        // survive that restoration too; explicit terminal taps / Show keyboard clear the flag.
        guard !keyboardDismissed else { return false }
        guard acceptsFocus, !covered else {
            wantsFocus = true
            return false
        }
        wantsFocus = false
        defer {
            refreshCaret()
            onFocusChange?()
            TerminalFocus.changed(self)
        }
        return super.becomeFirstResponder()
    }

    override func deleteBackward() {
        onUserInput?()
        if markedTextRange == nil, let bytes = latchedBackspace?() {
            send(data: bytes[...])
            return
        }
        super.deleteBackward()
    }

    /// The far side always has something Backspace can delete. SwiftTerm answers from its own
    /// input buffer, empty at most prompts, and the system keyboard repeats a held delete key
    /// only while this is true.
    override var hasText: Bool { true }

    // Typing goes to a program, not prose: no inline completions, Writing Tools or math results.
    @objc var inlinePredictionType: UITextInlinePredictionType { .no }
    @objc var mathExpressionCompletionType: UITextMathExpressionCompletionType { .no }
    @objc var writingToolsBehavior: UIWritingToolsBehavior { .none }

    override var contentOffset: CGPoint {
        get { super.contentOffset }
        set { super.contentOffset = pinnedOffset(for: newValue) }
    }

    override func setContentOffset(_ offset: CGPoint, animated: Bool) {
        super.setContentOffset(pinnedOffset(for: offset), animated: animated)
    }

    override func scrollRectToVisible(_ rect: CGRect, animated: Bool) {
        guard floatingPin == nil else { return }
        super.scrollRectToVisible(rect, animated: animated)
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
        // SwiftTerm's own taps; its handler runs first, as its target was added first.
        if let tap = gestureRecognizer as? UITapGestureRecognizer, tap.numberOfTouchesRequired == 1 {
            tap.addTarget(self, action: #selector(tapEnded(_:)))
        }
        super.addGestureRecognizer(gestureRecognizer)
    }

    /// Typing goes straight to the terminal, so a tap on it always brings the keyboard. SwiftTerm
    /// spends a tap on an app that tracks the mouse as a click and leaves the keyboard down;
    /// here the keyboard comes up after that handler ran, so the click still reaches the app.
    /// A terminal that kept focus while its keyboard went away gets it back too (`showKeyboard`).
    @objc private func tapEnded(_ tap: UITapGestureRecognizer) {
        guard tap.state == .ended, tap.numberOfTapsRequired == 1 else { return }
        keyboardDismissed = false
        DispatchQueue.main.async { [weak self] in
            guard let self, self.window != nil, !self.keyboardDismissed else { return }
            self.showKeyboard()
        }
    }

    /// Brings up the software keyboard for typing here, or with a hardware keyboard attached,
    /// focuses the terminal so it types (`TerminalFocusPolicy`).
    func showKeyboard(automatic: Bool = false) {
        let action = TerminalFocusPolicy.action(
            accepts: acceptsFocus, covered: covered, focused: isFirstResponder,
            keyboardShown: softwareKeyboardShown, hardwareKeyboard: KeyboardPresence.shared.hardwareKeyboardConnected,
            dismissed: keyboardDismissed, automatic: automatic
        )
        if !automatic { keyboardDismissed = false }
        switch action {
        case .none:
            break
        case .wait:
            wantsFocus = true
        case .focus:
            _ = becomeFirstResponder()
        case .refocus:
            _ = resignFirstResponder()
            _ = becomeFirstResponder()
        }
    }

    /// Hide keyboard: lets focus go and keeps the keyboard down until the user asks for it. If
    /// something still holds the keyboard, the window is told to end editing.
    func dismissKeyboard() {
        keyboardDismissed = true
        wantsFocus = false
        chordRepeater.stop()
        _ = resignFirstResponder()
        // Whatever holds the keyboard goes too (the chat's composer stays under the terminal
        // layer and can own it), so Hide keyboard always hides it.
        window?.endEditing(true)
    }

    /// The software keyboard is on screen, as the keyboard's own notifications say.
    private(set) var softwareKeyboardShown = false

    @objc private func keyboardWillShow(_ note: Notification) {
        let frame = (note.userInfo?[UIResponder.keyboardFrameEndUserInfoKey] as? CGRect) ?? .zero
        softwareKeyboardShown = frame.height > 0
    }

    @objc private func keyboardWillHide(_ note: Notification) {
        softwareKeyboardShown = false
    }

    override func gestureRecognizerShouldBegin(_ gestureRecognizer: UIGestureRecognizer) -> Bool {
        if gestureRecognizer === wheelPan {
            let velocity = wheelPan.velocity(in: self)
            return abs(velocity.y) >= abs(velocity.x) && (capturesWheelPan?() ?? false)
        }
        return super.gestureRecognizerShouldBegin(gestureRecognizer)
    }

    /// More than one character at once is dictation or a text replacement, not a key, so it
    /// gets the paste filter. A single typed character, a control included, passes unchanged.
    override func insertText(_ text: String) {
        onUserInput?()
        if markedTextRange == nil, let bytes = latchedInput?(text) {
            if !bytes.isEmpty { send(data: bytes[...]) }
            return
        }
        super.insertText(text.count > 1 ? PasteText.sanitize(text) : text)
    }

    /// Ctrl and Option-as-Meta chords are encoded by `KeyEncoder.hardware`, so hardware and
    /// key bar chords send the same bytes. With the kitty keyboard protocol on, or during
    /// composition, SwiftTerm keeps every press because it must report them its own way.
    override func pressesBegan(_ presses: Set<UIPress>, with event: UIPressesEvent?) {
        onUserInput?()
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
        let extended = KeyEncoder.extendedHardware(
            characters: characters,
            charactersIgnoringModifiers: charactersIgnoringModifiers, modifiers: modifiers, keyCode: keyCode,
            optionAsMeta: optionAsMetaKey, mode: extendedKeyMode?() ?? .legacy)
        guard let bytes = extended ?? KeyEncoder.hardware(
            characters: characters, charactersIgnoringModifiers: charactersIgnoringModifiers,
            modifiers: modifiers, optionAsMeta: optionAsMetaKey) else { return false }
        send(data: bytes[...])
        chordRepeater.start(key: keyCode, bytes: bytes, isHeld: isHeld)
        return true
    }

    /// SwiftTerm's copy writes the pasteboard itself and also ends the selection drag, so it
    /// still runs; the text is read first because the copy clears the selection, and written
    /// again after it without the spaces programs pad their lines with (`CopyText`).
    override func copy(_ sender: Any?) {
        var text = getSelection()
        if selectionThroughLineEnd, text?.hasSuffix("\n") == true { text = text.map { String($0.dropLast()) } }
        text = text.map(CopyText.trimmingLineEnds)
        super.copy(sender)
        if let text { UIPasteboard.general.string = text }
        selectionThroughLineEnd = false
        if let text { onCopy?(text) }
    }

    /// Ends the selection as SwiftTerm's paste does. SwiftTerm's kitty paste event (mode 5522)
    /// is internal to SwiftTerm, so with the filter in front it cannot be sent from here.
    override func paste(_ sender: Any?) {
        guard let onPaste else { return super.paste(sender) }
        clearSelection()
        if let text = pasteboardText() { onPaste(text) } else { onPasteWithoutText?() }
    }

    /// A key released after focus moves never reaches this view, so the repeat ends here.
    override func resignFirstResponder() -> Bool {
        chordRepeater.stop()
        defer {
            refreshCaret()
            onFocusChange?()
            TerminalFocus.changed(self)
        }
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

    /// A pointer click-drag: selects from `start` up to, not including, `end`. SwiftTerm keeps
    /// an end column inside the row, so an end past the last column runs to the next row's
    /// start and the copy drops the line break that brings.
    ///
    /// The last line of the buffer has no next row: SwiftTerm would clamp the end back to that
    /// line's first column, before the start, and copy the text left of the drag instead. There
    /// the end stays in the last column, which only a row filled to the edge loses.
    func select(from start: GridCell, to end: GridCell) {
        let top = terminalStateSnapshot().viewportRow
        let dimensions = terminalDimensions
        let throughLineEnd = end.col >= dimensions.cols
        let scrolledBack = !(terminalInputStateSnapshot()?.isAlternateBuffer ?? false) && canScroll && scrollPosition < 1
        let hasNextLine = end.row + 1 < dimensions.rows || scrolledBack
        selectionThroughLineEnd = throughLineEnd && hasNextLine
        let stop: Position
        if selectionThroughLineEnd {
            stop = Position(col: 0, row: end.row + top + 1)
        } else if throughLineEnd {
            stop = Position(col: max(0, dimensions.cols - 1), row: end.row + top)
        } else {
            stop = Position(col: end.col, row: end.row + top)
        }
        setSelectionRange(start: Position(col: start.col, row: start.row + top), end: stop)
    }

    /// Display widths of a visible row's cells: 2 and 0 for the halves of a wide glyph.
    func cellWidths(row: Int) -> [Int] {
        terminalStateSnapshot().visibleRows.dropFirst(row).first?.cellWidths ?? []
    }

    func showEditMenu(at point: CGPoint) {
        showStandardContextMenu(at: point)
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
