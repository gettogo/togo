import GameController
import Observation
import SwiftUI
import TogoTerminal
import UIKit

/// Whether the on-screen keyboard is up, and whether it types into a terminal rather than a
/// text field. While a terminal takes the typing the prompt bar steps aside.
///
/// The keyboard's frame alone cannot say this: a split or floating keyboard, as on an unfolded
/// Duo, reports itself hidden while it is on screen, and a fold moves the terminal between
/// windows' layouts with the keyboard dropping for a moment. So the first responder decides,
/// with the frame as a second witness, and a terminal losing the keyboard counts only once it
/// has stayed lost for a moment.
@Observable @MainActor final class KeyboardWatch {
    static let shared = KeyboardWatch()

    /// A software keyboard is on screen, docked or not. A hardware keyboard's accessory row
    /// alone is not.
    private(set) var keyboardUp = false
    /// The keyboard is up and a terminal holds it, so it types into a terminal.
    private(set) var terminalTyping = false

    /// Shorter than this is an accessory row over a hardware keyboard, not a keyboard.
    private static let keyboardHeight: CGFloat = 120
    /// How long a terminal may be without the keyboard before the chrome comes back, so a fold
    /// or a terminal moving between panes does not flash the bar and header.
    private static let release: Duration = .milliseconds(450)
    @ObservationIgnored private var observers: [NSObjectProtocol] = []
    @ObservationIgnored private var dockedKeyboard = false
    @ObservationIgnored private var pendingRelease: Task<Void, Never>?

    private init() {
        let center = NotificationCenter.default
        observers.append(center.addObserver(
            forName: UIResponder.keyboardWillChangeFrameNotification, object: nil, queue: .main
        ) { note in
            let frame = (note.userInfo?[UIResponder.keyboardFrameEndUserInfoKey] as? NSValue)?.cgRectValue ?? .zero
            MainActor.assumeIsolated { KeyboardWatch.shared.keyboardMoved(to: frame) }
        })
        observers.append(center.addObserver(
            forName: UIResponder.keyboardWillHideNotification, object: nil, queue: .main
        ) { _ in
            MainActor.assumeIsolated { KeyboardWatch.shared.keyboardMoved(to: .zero) }
        })
        // The keyboard can stay up while it moves between a field and a terminal, and a split
        // keyboard sends no frames at all; the responder says who has it.
        for name in [
            UITextField.textDidBeginEditingNotification, UITextField.textDidEndEditingNotification,
            UITextView.textDidBeginEditingNotification, UITextView.textDidEndEditingNotification,
            TerminalFocus.didChange, Notification.Name.GCKeyboardDidConnect, .GCKeyboardDidDisconnect,
            UIApplication.didBecomeActiveNotification,
        ] {
            observers.append(center.addObserver(forName: name, object: nil, queue: .main) { _ in
                // The responder settles after the notification; look once it has.
                Task { @MainActor in KeyboardWatch.shared.refresh() }
            })
        }
    }

    /// Looks again, as after a fold, a rotation or a size class change moved things around.
    func refresh() {
        let responder = UIResponder.togoCurrent
        let software = GCKeyboard.coalesced == nil
        let field = responder is UITextField || responder is UITextView
        let terminal = TerminalFocus.hasKeyboard
        let up = dockedKeyboard || (software && (field || terminal))
        if keyboardUp != up { keyboardUp = up }
        let typing = terminal && (dockedKeyboard || software)
        if typing {
            pendingRelease?.cancel()
            pendingRelease = nil
            if !terminalTyping { terminalTyping = true }
        } else if terminalTyping, pendingRelease == nil {
            pendingRelease = Task { @MainActor in
                try? await Task.sleep(for: Self.release)
                guard !Task.isCancelled else { return }
                KeyboardWatch.shared.settleRelease()
            }
        }
    }

    /// The terminal stayed without the keyboard: unless it took it back, typing is over.
    private func settleRelease() {
        pendingRelease = nil
        let stillTyping = TerminalFocus.hasKeyboard && (dockedKeyboard || GCKeyboard.coalesced == nil)
        if terminalTyping != stillTyping { terminalTyping = stillTyping }
    }

    /// `frame` is in screen coordinates; one that starts at or past the screen's foot is off it.
    private func keyboardMoved(to frame: CGRect) {
        let screen = UIApplication.shared.connectedScenes.compactMap { ($0 as? UIWindowScene)?.screen.bounds }.first
        var up = frame.height >= Self.keyboardHeight
        if let screen, frame.minY >= screen.maxY { up = false }
        dockedKeyboard = up
        refresh()
    }
}

@MainActor extension UIResponder {
    private static weak var togoCaptured: UIResponder?

    /// The first responder, found by sending an action down the responder chain.
    static var togoCurrent: UIResponder? {
        togoCaptured = nil
        UIApplication.shared.sendAction(#selector(togoCaptureResponder), to: nil, from: nil, for: nil)
        return togoCaptured
    }

    @objc private func togoCaptureResponder() {
        UIResponder.togoCaptured = self
    }
}
