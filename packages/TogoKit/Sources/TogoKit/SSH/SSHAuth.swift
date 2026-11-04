import Foundation
import NIOCore
@preconcurrency import NIOSSH
import Synchronization

/// Offers the credentials resolved before the handshake, each once, key first, then
/// keyboard-interactive if the server lists it.
///
/// Key and password are in hand before NIO asks, so those never wait on a prompt or a
/// biometric check on the event loop. Keyboard-interactive prompts are the exception: the
/// server sends them mid-handshake, and the responder answers without blocking the loop.
/// The attempts are recorded for `.authFailed(tried:)`.
final class PreparedAuthDelegate: NIOSSHClientUserAuthenticationDelegate, Sendable, CustomStringConvertible {
    private struct State: CustomStringConvertible {
        var key: NIOSSHPrivateKey?
        var password: String?
        var tried: [String] = []
        var serverOffersPassword = false
        var keyboardInteractiveOffered = false

        var description: String { "State(tried: \(tried), <credentials redacted>)" }
    }

    private let username: String
    private let state: Mutex<State>
    private let keyboardInteractive: KeyboardInteractiveResponder?

    init(username: String, key: NIOSSHPrivateKey?, password: String?, keyboardInteractive: KeyboardInteractiveResponder? = nil) {
        self.username = username
        self.keyboardInteractive = keyboardInteractive
        state = Mutex(State(key: key, password: password))
    }

    var keyboardInteractiveCancelled: Bool { keyboardInteractive?.cancelled ?? false }

    var tried: [String] { state.withLock { $0.tried } }
    var description: String { "PreparedAuthDelegate(\(username), <credentials redacted>)" }
    var serverOffersPassword: Bool { state.withLock { $0.serverOffersPassword } }

    func nextAuthenticationType(
        availableMethods: NIOSSHAvailableUserAuthenticationMethods,
        nextChallengePromise: EventLoopPromise<NIOSSHUserAuthenticationOffer?>
    ) {
        let username = self.username
        let offer: NIOSSHUserAuthenticationOffer? = state.withLock { state in
            state.serverOffersPassword = availableMethods.contains(.password)
            if availableMethods.contains(.publicKey), let key = state.key {
                state.key = nil
                state.tried.append("publickey")
                return .init(username: username, serviceName: "", offer: .privateKey(.init(privateKey: key)))
            }
            if availableMethods.contains(.password), let password = state.password {
                state.password = nil
                state.tried.append("password")
                return .init(username: username, serviceName: "", offer: .password(.init(password: password)))
            }
            if availableMethods.contains(.keyboardInteractive), keyboardInteractive != nil, !state.keyboardInteractiveOffered {
                state.keyboardInteractiveOffered = true
                state.tried.append("keyboard-interactive")
                return .init(username: username, serviceName: "", offer: .keyboardInteractive(.init()))
            }
            return nil
        }
        if let offer {
            nextChallengePromise.succeed(offer)
        } else {
            nextChallengePromise.fail(AuthExhausted())
        }
    }

    func keyboardInteractiveChallengeReceived(
        _ challenge: NIOSSHKeyboardInteractiveChallenge, responsePromise: EventLoopPromise<[String]>
    ) {
        guard let keyboardInteractive else {
            responsePromise.fail(AuthExhausted())
            return
        }
        keyboardInteractive.respond(to: challenge, promise: responsePromise)
    }
}

struct AuthExhausted: Error {}

/// Compares the presented host key with the fingerprints trusted before the handshake.
/// An unknown key fails the handshake instead of waiting for the user, because Citadel
/// gives a login ten seconds; the connection asks, stores, and connects again.
final class SnapshotHostKeyValidator: NIOSSHClientServerAuthenticationDelegate, Sendable {
    struct Presented: Sendable, Equatable {
        var keyType: String
        var fingerprint: String
    }

    let trusted: [String]
    private let presentedKey = Mutex<Presented?>(nil)

    init(trusted: [String]) {
        self.trusted = trusted
    }

    var presented: Presented? { presentedKey.withLock { $0 } }

    func validateHostKey(hostKey: NIOSSHPublicKey, validationCompletePromise: EventLoopPromise<Void>) {
        let presented = Presented(keyType: PublicKeyLine.keyType(of: hostKey), fingerprint: PublicKeyLine.fingerprint(for: hostKey))
        presentedKey.withLock { $0 = presented }
        if trusted.contains(presented.fingerprint) {
            validationCompletePromise.succeed(())
        } else {
            validationCompletePromise.fail(UntrustedHostKey())
        }
    }
}

struct UntrustedHostKey: Error {}
