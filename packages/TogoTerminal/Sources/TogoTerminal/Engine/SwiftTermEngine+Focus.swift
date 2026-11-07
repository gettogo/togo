import UIKit

extension SwiftTermEngine {
    /// Whether the terminal may take the keyboard. Turning it off lets the keyboard go; turning
    /// it on brings the keyboard up only when the terminal asked for it in the meantime, as
    /// appearing on screen does, and is still on screen.
    public var acceptsFocus: Bool {
        get { terminalView.acceptsFocus }
        set {
            guard newValue != terminalView.acceptsFocus else { return }
            terminalView.acceptsFocus = newValue
            if !newValue {
                _ = terminalView.resignFirstResponder()
            } else if terminalView.wantsFocus, !terminalView.covered {
                terminalView.wantsFocus = false
                if terminalView.window != nil { _ = terminalView.becomeFirstResponder() }
            }
        }
    }
}
