import Foundation
import NIOCore
@preconcurrency import NIOSSH
import Synchronization

/// Answers keyboard-interactive rounds (RFC 4256) through the connection's prompter.
///
/// The server sends these prompts in the middle of the handshake, so the answer is awaited
/// in a task and the event loop only holds the promise.
final class KeyboardInteractiveResponder: Sendable {
    /// Long enough to read a code off another device; TCP connect keeps its own short timeout.
    static let loginTimeout: TimeAmount = .seconds(180)
    /// A server may end with an empty round; more than this many is a loop, not a login.
    static let maxEmptyRounds = 8

    private struct State {
        var cancelled = false
        var emptyRounds = 0
        var pending: [Task<Void, Never>] = []
    }

    private let prompter: any ConnectionPrompter
    private let autofill: PasswordAutofill?
    private let state = Mutex(State())

    init(prompter: any ConnectionPrompter, autofill: PasswordAutofill? = nil) {
        self.prompter = prompter
        self.autofill = autofill
    }

    var cancelled: Bool { state.withLock { $0.cancelled } }

    /// Cancels prompts still on screen once the handshake has ended, so the UI dismisses them.
    func cancelPending() {
        for task in state.withLock({ $0.pending }) { task.cancel() }
    }

    func respond(to challenge: NIOSSHKeyboardInteractiveChallenge, promise: EventLoopPromise<[String]>) {
        if challenge.prompts.isEmpty {
            let tooMany = state.withLock { state in
                state.emptyRounds += 1
                return state.emptyRounds > Self.maxEmptyRounds
            }
            guard !tooMany else {
                promise.fail(KeyboardInteractiveLoop())
                return
            }
        }
        var answers = [String?](repeating: nil, count: challenge.prompts.count)
        if let index = challenge.prompts.firstIndex(where: Self.asksForPassword), let password = autofill?.take() {
            answers[index] = password
        }
        let open = answers.indices.filter { answers[$0] == nil }
        // Covers OpenSSH's closing round without prompts and a round fully answered above.
        guard !open.isEmpty else {
            promise.succeed(answers.compactMap { $0 })
            return
        }
        let prompts = open.map { KIPrompt(text: challenge.prompts[$0].text, echo: challenge.prompts[$0].echo) }
        let prompt = AuthPrompt.keyboardInteractive(name: challenge.name, instruction: challenge.instruction, prompts: prompts)
        let task = Task {
            if case .responses(let given) = await prompter.answer(prompt), given.count == open.count, !Task.isCancelled {
                for (index, answer) in zip(open, given) { answers[index] = answer }
                promise.succeed(answers.compactMap { $0 })
            } else {
                // A prompt dismissed because the handshake already ended is not a user cancel.
                if !Task.isCancelled { state.withLock { $0.cancelled = true } }
                promise.fail(KeyboardInteractiveCancelled())
            }
        }
        state.withLock { $0.pending.append(task) }
    }

    static func asksForPassword(_ prompt: NIOSSHKeyboardInteractiveChallenge.Prompt) -> Bool {
        !prompt.echo && prompt.text.range(of: "password", options: .caseInsensitive) != nil
    }
}

struct KeyboardInteractiveCancelled: Error {}
struct KeyboardInteractiveLoop: Error {}

/// The password the user already gave for this connection, offered at most once to a
/// keyboard-interactive password prompt so PAM hosts do not ask for it a second time.
///
/// Held as bytes so they can be overwritten after use. The `String` handed to NIOSSH and the
/// one in the connection's credentials cannot be wiped; this only shortens the copy's life.
final class PasswordAutofill: Sendable, CustomStringConvertible {
    private struct State {
        var bytes: [UInt8]?
        var spent = false
    }

    private let state = Mutex(State())

    var description: String { "PasswordAutofill(<redacted>)" }

    /// Ignored once the password has been used.
    func load(_ password: String?) {
        state.withLock { state in
            guard !state.spent else { return }
            Self.wipe(&state.bytes)
            state.bytes = password.map { Array($0.utf8) }
        }
    }

    func take() -> String? {
        state.withLock { state in
            // No local binding of the array: a second reference would make the wipe
            // below copy the buffer and overwrite the copy instead.
            guard !state.spent, state.bytes != nil else { return nil }
            state.spent = true
            let password = String(decoding: state.bytes!, as: UTF8.self)
            Self.wipe(&state.bytes)
            return password
        }
    }

    private static func wipe(_ bytes: inout [UInt8]?) {
        guard bytes != nil else { return }
        bytes!.withUnsafeMutableBufferPointer { $0.update(repeating: 0) }
        bytes = nil
    }
}
