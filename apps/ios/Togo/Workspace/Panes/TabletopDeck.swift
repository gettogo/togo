import SwiftUI
import UIKit

/// The lower half of a Duo held as a tabletop, under the fold. Request cards sit at its top,
/// just below the fold; the input status and the Keyboard button sit at its foot, where the
/// software keyboard and key bar rise as usual, leaving the terminal above the fold in view.
struct TabletopDeck<Requests: View>: View {
    private let requests: Requests

    init(@ViewBuilder requests: () -> Requests) {
        self.requests = requests()
    }

    var body: some View {
        requests
            // width-ok: the request deck's widest on the tabletop half; narrower halves fill.
            .frame(maxWidth: 560)
            .padding(.horizontal, 12)
            .padding(.top, 12)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
    }

    /// Brings up the keyboard for the focused terminal, whose own view sits above the fold out
    /// of easy reach.
    struct KeyboardButton: View {
        let action: () -> Void

        var body: some View {
            Button(PanesCopy.keyboard, systemImage: "keyboard", action: action)
                .buttonStyle(.secondary(.fit))
                .padding(.vertical, 8)
                .frame(maxWidth: .infinity)
                .background(Color.togoInk)
        }
    }
}

extension TerminalHandle {
    /// Gives the terminal keyboard focus, which brings up the software keyboard and key bar,
    /// also when the terminal kept focus while its keyboard went away (`showKeyboard`).
    /// `automatic`: the app raising it on its own (a pane move, coming back from the chat); it
    /// never undoes the user's Hide keyboard.
    func raiseKeyboard(automatic: Bool = false) {
        if let engine {
            engine.showKeyboard(automatic: automatic)
            return
        }
        guard let view = engineView.viewIfLoaded else { return }
        Self.firstResponder(in: view)?.becomeFirstResponder()
    }

    /// The terminal has the keyboard.
    var holdsKeyboard: Bool {
        guard let view = engineView.viewIfLoaded else { return false }
        return Self.contains(firstResponderIn: view)
    }

    private static func contains(firstResponderIn view: UIView) -> Bool {
        view.isFirstResponder || view.subviews.contains { contains(firstResponderIn: $0) }
    }

    private static func firstResponder(in view: UIView) -> UIView? {
        if view.canBecomeFirstResponder { return view }
        for subview in view.subviews {
            if let found = firstResponder(in: subview) { return found }
        }
        return nil
    }
}
