import Foundation

public enum AuthPrompt: Sendable {
    case password(user: String, host: String)
    case passphrase(keyLabel: String)
    /// One keyboard-interactive round (RFC 4256), often a password then a one-time code.
    /// Answer with `.responses`, one per prompt in order. It is asked while the handshake
    /// waits, so the connection allows a longer login window.
    case keyboardInteractive(name: String, instruction: String, prompts: [KIPrompt])
}

public struct KIPrompt: Sendable, Hashable {
    public var text: String
    public var echo: Bool

    public init(text: String, echo: Bool) {
        self.text = text
        self.echo = echo
    }
}

public enum AuthAnswer: Sendable, CustomStringConvertible, CustomDebugStringConvertible {
    case secret(String)
    case responses([String])

    public var description: String {
        switch self {
        case .secret: "secret(<redacted>)"
        case .responses(let answers): "responses(<\(answers.count) redacted>)"
        }
    }

    public var debugDescription: String { description }
}

/// The UI side of a connection. Both calls may wait as long as the user takes; the
/// connection never holds the SSH handshake open while they run.
public protocol ConnectionPrompter: Sendable {
    /// nil means the user cancelled. A keyboard-interactive prompt runs while the handshake
    /// waits; if the connection ends first, the calling task is cancelled and the prompt
    /// should be dismissed.
    func answer(_ prompt: AuthPrompt) async -> AuthAnswer?
    func trustNewHostKey(host: String, keyType: String, fingerprint: String) async -> Bool
}
