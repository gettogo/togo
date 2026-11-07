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
    /// Keyboard-interactive only as a second step after the server accepted the key, such
    /// as a one-time code. A refused key leaves public key in the server's list, so it still
    /// ends as a refused key rather than a prompt.
    private let keyboardInteractiveAfterKeyOnly: Bool

    init(
        username: String, key: NIOSSHPrivateKey?, password: String?, keyboardInteractive: KeyboardInteractiveResponder? = nil,
        keyboardInteractiveAfterKeyOnly: Bool = false
    ) {
        self.username = username
        self.keyboardInteractive = keyboardInteractive
        self.keyboardInteractiveAfterKeyOnly = keyboardInteractiveAfterKeyOnly
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
        let afterKeyOnly = keyboardInteractiveAfterKeyOnly
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
            let keyAccepted = state.tried.contains("publickey") && !availableMethods.contains(.publicKey)
            if availableMethods.contains(.keyboardInteractive), keyboardInteractive != nil, !state.keyboardInteractiveOffered,
               !afterKeyOnly || keyAccepted {
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
    /// Set while racing routes: only the first route whose key matches goes on to sign in.
    private let gate: (gate: RouteGate, token: Int)?
    private let presentedKey = Mutex<Presented?>(nil)
    private let lostRace = Mutex(false)

    init(trusted: [String], gate: RouteGate? = nil, token: Int = 0) {
        self.trusted = trusted
        self.gate = gate.map { ($0, token) }
    }

    var presented: Presented? { presentedKey.withLock { $0 } }

    /// The key matched, but another route had already won.
    var lost: Bool { lostRace.withLock { $0 } }

    func validateHostKey(hostKey: NIOSSHPublicKey, validationCompletePromise: EventLoopPromise<Void>) {
        let presented = Presented(keyType: PublicKeyLine.keyType(of: hostKey), fingerprint: PublicKeyLine.fingerprint(for: hostKey))
        presentedKey.withLock { $0 = presented }
        let decision = gate.map { $0.gate.admit(presented.fingerprint, pinned: trusted, token: $0.token) }
            ?? (trusted.contains(presented.fingerprint) ? .accept : .mismatch)
        switch decision {
        case .accept:
            validationCompletePromise.succeed(())
        case .mismatch:
            validationCompletePromise.fail(UntrustedHostKey())
        case .lost:
            lostRace.withLock { $0 = true }
            validationCompletePromise.fail(RouteRace.Lost())
        }
    }
}

/// Lets exactly one raced route past the host key check. A key that is not pinned is a
/// mismatch on every route, whoever has won, and that route never signs in; a pinned key
/// admits the first route to show it and turns every later one away, so only one route ever
/// signs in or asks a question.
public final class RouteGate: Sendable {
    public enum Decision: Equatable, Sendable {
        case accept, mismatch, lost
    }

    private let owner = Mutex<Int?>(nil)
    private let admitted = Mutex(false)

    public init() {}

    /// Whether any route has shown the pinned key, even one that failed afterwards.
    public var sawPinnedKey: Bool { admitted.withLock { $0 } }

    public func admit(_ fingerprint: String, pinned: [String], token: Int) -> Decision {
        guard pinned.contains(fingerprint) else { return .mismatch }
        admitted.withLock { $0 = true }
        return owner.withLock { owner in
            if owner == nil { owner = token }
            return owner == token ? .accept : .lost
        }
    }

    /// The winner's link died before it signed in, so a route still on its way may win.
    public func release(_ token: Int) {
        owner.withLock { if $0 == token { $0 = nil } }
    }
}

struct UntrustedHostKey: Error {}
