/// What a tap on the terminal, Show keyboard, or something that raises the keyboard on its own
/// (`automatic`), does about the keyboard.
///
/// UIKit raises the software keyboard only as a view becomes first responder. A terminal that
/// is already first responder while its keyboard is away (dismissed by the system, as on a fold,
/// a return from the background or the keyboard's own dismiss key) gets nothing from asking
/// again, so it lets focus go and takes it back.
enum TerminalFocusPolicy {
    enum Action: Equatable, Sendable {
        /// Nothing to do: the keyboard is up, or a hardware keyboard types here already.
        case none
        /// Not now (covered, or the link not live): the keyboard comes when it may.
        case wait
        case focus
        /// Focused with no keyboard on screen: let focus go and take it again.
        case refocus
    }

    static func action(
        accepts: Bool, covered: Bool, focused: Bool, keyboardShown: Bool, hardwareKeyboard: Bool,
        dismissed: Bool = false, automatic: Bool = false
    ) -> Action {
        // Hide keyboard stays hidden: only the user (a tap, Show keyboard) brings it back, never
        // a layout pass, a pane move or a terminal appearing.
        if dismissed, automatic { return .none }
        guard accepts, !covered else { return .wait }
        guard focused else { return .focus }
        return keyboardShown || hardwareKeyboard ? .none : .refocus
    }
}
