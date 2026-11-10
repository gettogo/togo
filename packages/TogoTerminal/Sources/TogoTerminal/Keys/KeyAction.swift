import Foundation

/// One step of a key bar macro: a key, literal text typed as-is, or a pause so a TUI can
/// redraw before the next key (Claude Code reads `Esc Esc` as rewind only when both arrive).
public enum KeyStep: Hashable, Codable, Sendable {
    case stroke(KeyStroke)
    case text(String)
    case delay(milliseconds: Int)
}

public enum KeyAction: Hashable, Codable, Sendable {
    case steps([KeyStep])
    case ctrlLatch
    case altLatch
    case shiftLatch
    case paste
    case hideKeyboard
    case dpad
    /// Saved by earlier builds for the key that opened a compose bar. The terminal has no
    /// composer any more: a bar that still holds it reads it back as `attach`.
    case compose
    /// Listens, then types the words it heard into the terminal. It never presses Enter.
    case dictate
    /// Opens the app's photo picker; the picture goes to the machine and its path is typed.
    case attach
}

@MainActor
extension SwiftTermEngine {
    /// Runs steps in order. Steps after a delay run from a task, so a sequence without delays
    /// is fully written before the call returns and stays ordered with the next tap. Returns
    /// the task writing the rest, if any.
    @discardableResult
    func run(_ steps: some Collection<KeyStep>) -> Task<Void, Never>? {
        var remaining = steps[...]
        while let step = remaining.popFirst() {
            switch step {
            case .stroke(let stroke):
                send(stroke)
            case .text(let text):
                sendText(text)
            case .delay(let milliseconds):
                let rest = Array(remaining)
                return Task { [weak self] in
                    try? await Task.sleep(for: .milliseconds(milliseconds))
                    await self?.run(rest)?.value
                }
            }
        }
        return nil
    }
}
