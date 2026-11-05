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
    case paste
    case hideKeyboard
    case dpad
    /// Opens or closes the app's compose bar; the bar itself sends nothing.
    case compose
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
