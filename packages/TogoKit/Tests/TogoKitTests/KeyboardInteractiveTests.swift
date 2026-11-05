@preconcurrency import Citadel
import Foundation
import NIOCore
@preconcurrency import NIOSSH
import Synchronization
import Testing
@testable import TogoKit

/// Sends one round of `prompts`, then an empty round as OpenSSH's PAM driver does, and
/// accepts only `expected` as the answers.
private final class TwoFactorServer: NIOSSHServerKeyboardInteractiveDelegate, Sendable {
    let prompts: [NIOSSHKeyboardInteractiveChallenge.Prompt]
    let expected: [String]

    init(prompts: [NIOSSHKeyboardInteractiveChallenge.Prompt], expected: [String]) {
        self.prompts = prompts
        self.expected = expected
    }

    var supportedAuthenticationMethods: NIOSSHAvailableUserAuthenticationMethods { .keyboardInteractive }

    func requestReceived(
        request: NIOSSHUserAuthenticationRequest, responsePromise: EventLoopPromise<NIOSSHUserAuthenticationOutcome>
    ) {
        responsePromise.succeed(.failure)
    }

    func keyboardInteractiveReceived(
        username: String, responses: [String]?, nextStepPromise: EventLoopPromise<NIOSSHKeyboardInteractiveStep>
    ) {
        switch responses {
        case nil:
            nextStepPromise.succeed(.challenge(.init(name: "", instruction: "Two-step login", prompts: prompts)))
        case expected?:
            nextStepPromise.succeed(.challenge(.init(prompts: [])))
        case []:
            nextStepPromise.succeed(.outcome(.success))
        default:
            nextStepPromise.succeed(.outcome(.failure))
        }
    }
}

/// Answers the up-front password prompt with "pw" and each keyboard-interactive round
/// with `responses`, or cancels it when `responses` is nil.
private final class TwoFactorPrompter: ConnectionPrompter {
    let responses: [String]?
    let asked = Mutex<[AuthPrompt]>([])

    init(responses: [String]?) {
        self.responses = responses
    }

    func answer(_ prompt: AuthPrompt) async -> AuthAnswer? {
        asked.withLock { $0.append(prompt) }
        switch prompt {
        case .keyboardInteractive: return responses.map(AuthAnswer.responses)
        case .password, .passphrase: return .secret("pw")
        }
    }

    func trustNewHostKey(host: String, keyType: String, fingerprint: String) async -> Bool { true }

    var passwordAsks: Int {
        asked.withLock { $0 }.filter { if case .password = $0 { true } else { false } }.count
    }

    var keyboardInteractiveRounds: [[KIPrompt]] {
        asked.withLock { $0 }.compactMap { if case .keyboardInteractive(_, _, let prompts) = $0 { prompts } else { nil } }
    }
}

/// Sends nothing but empty rounds, forever.
private final class EmptyRoundServer: NIOSSHServerKeyboardInteractiveDelegate, Sendable {
    var supportedAuthenticationMethods: NIOSSHAvailableUserAuthenticationMethods { .keyboardInteractive }

    func requestReceived(
        request: NIOSSHUserAuthenticationRequest, responsePromise: EventLoopPromise<NIOSSHUserAuthenticationOutcome>
    ) {
        responsePromise.succeed(.failure)
    }

    func keyboardInteractiveReceived(
        username: String, responses: [String]?, nextStepPromise: EventLoopPromise<NIOSSHKeyboardInteractiveStep>
    ) {
        nextStepPromise.succeed(.challenge(.init(prompts: [])))
    }
}

/// Keeps a keyboard-interactive prompt open until its task is cancelled.
private final class WaitingPrompter: ConnectionPrompter {
    let asked = Mutex(false)
    let dismissed = Mutex(false)

    func answer(_ prompt: AuthPrompt) async -> AuthAnswer? {
        if case .password = prompt { return .secret("pw") }
        asked.withLock { $0 = true }
        while !Task.isCancelled { try? await Task.sleep(for: .milliseconds(20)) }
        dismissed.withLock { $0 = true }
        return nil
    }

    func trustNewHostKey(host: String, keyType: String, fingerprint: String) async -> Bool { true }
}

/// Offers every method a stock sshd does and refuses public keys, like a machine whose
/// authorized_keys does not list the phone yet.
private final class RefusesKeys: NIOSSHServerKeyboardInteractiveDelegate, Sendable {
    var supportedAuthenticationMethods: NIOSSHAvailableUserAuthenticationMethods { [.publicKey, .password, .keyboardInteractive] }

    func requestReceived(
        request: NIOSSHUserAuthenticationRequest, responsePromise: EventLoopPromise<NIOSSHUserAuthenticationOutcome>
    ) {
        responsePromise.succeed(.failure)
    }

    func keyboardInteractiveReceived(
        username: String, responses: [String]?, nextStepPromise: EventLoopPromise<NIOSSHKeyboardInteractiveStep>
    ) {
        if responses == nil {
            nextStepPromise.succeed(.challenge(.init(name: "", instruction: "", prompts: [KeyboardInteractiveTests.password])))
        } else {
            nextStepPromise.succeed(.outcome(.failure))
        }
    }
}

/// Accepts any key as a first step, then asks for a one-time code, like sshd with
/// `AuthenticationMethods publickey,keyboard-interactive`.
private final class KeyThenCode: NIOSSHServerKeyboardInteractiveDelegate, Sendable {
    var supportedAuthenticationMethods: NIOSSHAvailableUserAuthenticationMethods { [.publicKey, .keyboardInteractive] }

    func requestReceived(
        request: NIOSSHUserAuthenticationRequest, responsePromise: EventLoopPromise<NIOSSHUserAuthenticationOutcome>
    ) {
        if case .publicKey = request.request {
            responsePromise.succeed(.partialSuccess(remainingMethods: .keyboardInteractive))
        } else {
            responsePromise.succeed(.failure)
        }
    }

    func keyboardInteractiveReceived(
        username: String, responses: [String]?, nextStepPromise: EventLoopPromise<NIOSSHKeyboardInteractiveStep>
    ) {
        switch responses {
        case nil: nextStepPromise.succeed(.challenge(.init(name: "", instruction: "", prompts: [KeyboardInteractiveTests.code])))
        case ["123456"]?: nextStepPromise.succeed(.outcome(.success))
        default: nextStepPromise.succeed(.outcome(.failure))
        }
    }
}

struct KeyboardInteractiveTests {
    static let password = NIOSSHKeyboardInteractiveChallenge.Prompt(text: "Password:", echo: false)
    static let code = NIOSSHKeyboardInteractiveChallenge.Prompt(text: "Verification code:", echo: true)

    fileprivate static func startServer(_ delegate: some NIOSSHServerUserAuthenticationDelegate & Sendable) async throws -> (SSHServer, Int) {
        for _ in 0..<20 {
            let port = Int.random(in: 20_000...60_000)
            if let server = try? await SSHServer.host(
                host: "127.0.0.1", port: port, hostKeys: [try SSHServerTests.hostKey(7)], authenticationDelegate: delegate
            ) {
                return (server, port)
            }
        }
        throw SSHError.unreachable("no free port")
    }

    private static func connection(port: Int, prompter: any ConnectionPrompter) -> SSHConnection {
        let secrets = MemorySecretStore()
        let biometrics = BiometricSession(evaluator: CountingEvaluator())
        let endpoint = HostEndpoint(host: "127.0.0.1", port: port, user: "u")
        let profile = HostProfile(id: UUID(), name: "2fa", endpoint: endpoint, auth: .password)
        return SSHConnection(
            profile: profile, keys: KeychainKeyStore(secrets: secrets, biometrics: biometrics), secrets: secrets,
            biometrics: biometrics, hostKeys: FileHostKeyStore(url: SSHServerTests.scratchURL()), prompter: prompter
        )
    }

    @Test func passwordOnlyViaKeyboardInteractiveAsksOnce() async throws {
        let (server, port) = try await Self.startServer(TwoFactorServer(prompts: [Self.password], expected: ["pw"]))
        let prompter = TwoFactorPrompter(responses: nil)
        let connection = Self.connection(port: port, prompter: prompter)

        try await connection.connect()
        #expect(await connection.isConnected)
        #expect(prompter.passwordAsks == 1)
        #expect(prompter.keyboardInteractiveRounds.isEmpty)
        await connection.close()
        try await server.close()
    }

    @Test func passwordAndCodeAskEachOnce() async throws {
        let (server, port) = try await Self.startServer(
            TwoFactorServer(prompts: [Self.password, Self.code], expected: ["pw", "123456"])
        )
        let prompter = TwoFactorPrompter(responses: ["123456"])
        let connection = Self.connection(port: port, prompter: prompter)

        try await connection.connect()
        #expect(await connection.isConnected)
        #expect(prompter.passwordAsks == 1)
        #expect(prompter.keyboardInteractiveRounds == [[KIPrompt(text: "Verification code:", echo: true)]])
        await connection.close()
        try await server.close()
    }

    @Test func promptsReachPrompter() async throws {
        let pin = NIOSSHKeyboardInteractiveChallenge.Prompt(text: "PIN:", echo: false)
        let (server, port) = try await Self.startServer(TwoFactorServer(prompts: [pin, Self.code], expected: ["4321", "123456"]))
        let prompter = TwoFactorPrompter(responses: ["4321", "123456"])
        let connection = Self.connection(port: port, prompter: prompter)

        try await connection.connect()
        #expect(prompter.keyboardInteractiveRounds == [[KIPrompt(text: "PIN:", echo: false), KIPrompt(text: "Verification code:", echo: true)]])
        await connection.close()
        try await server.close()
    }

    @Test func cancelStopsAuth() async throws {
        let (server, port) = try await Self.startServer(TwoFactorServer(prompts: [Self.code], expected: ["123456"]))
        let connection = Self.connection(port: port, prompter: TwoFactorPrompter(responses: nil))

        await #expect(throws: SSHError.cancelled) { try await connection.connect() }
        try await server.close()
    }

    @Test func wrongCodeFailsAuth() async throws {
        let (server, port) = try await Self.startServer(
            TwoFactorServer(prompts: [Self.password, Self.code], expected: ["pw", "123456"])
        )
        let connection = Self.connection(port: port, prompter: TwoFactorPrompter(responses: ["000000"]))

        await #expect(throws: SSHError.authFailed(tried: ["password", "keyboard-interactive"])) {
            try await connection.connect()
        }
        try await server.close()
    }

    @Test func onlyEchoOffPasswordPromptsAreFilled() {
        let fill = KeyboardInteractiveResponder.asksForPassword
        #expect(fill(.init(text: "Password:", echo: false)))
        #expect(fill(.init(text: "u@host's PASSWORD: ", echo: false)))
        #expect(!fill(.init(text: "Password:", echo: true)))
        #expect(!fill(.init(text: "Verification code:", echo: false)))
        #expect(!fill(.init(text: "One-time passcode:", echo: false)))
    }

    @Test func autofillIsUsedOnce() {
        let autofill = PasswordAutofill()
        autofill.load("pw")
        #expect(autofill.take() == "pw")
        #expect(autofill.take() == nil)
        autofill.load("pw")
        #expect(autofill.take() == nil)
        #expect(!String(describing: autofill).contains("pw"))
    }

    @Test func emptyRoundsAreCapped() async throws {
        let (server, port) = try await Self.startServer(EmptyRoundServer())
        let connection = Self.connection(port: port, prompter: TwoFactorPrompter(responses: nil))

        await #expect(throws: SSHError.authFailed(tried: ["password", "keyboard-interactive"])) {
            try await connection.connect()
        }
        try await server.close()
    }

    @Test func cancellingConnectAbortsHandshakeAndDismissesPrompt() async throws {
        let (server, port) = try await Self.startServer(TwoFactorServer(prompts: [Self.code], expected: ["123456"]))
        let prompter = WaitingPrompter()
        let connection = Self.connection(port: port, prompter: prompter)
        let attempt = Task { try await connection.connect() }

        let clock = ContinuousClock()
        let deadline = clock.now + .seconds(5)
        while !prompter.asked.withLock({ $0 }), clock.now < deadline { try await Task.sleep(for: .milliseconds(20)) }
        #expect(prompter.asked.withLock { $0 })

        let cancelledAt = clock.now
        attempt.cancel()
        await #expect(throws: SSHError.cancelled) { try await attempt.value }
        #expect(clock.now - cancelledAt < .seconds(5), "the handshake must not wait out the login timeout")
        while !prompter.dismissed.withLock({ $0 }), clock.now < deadline + .seconds(5) { try await Task.sleep(for: .milliseconds(20)) }
        #expect(prompter.dismissed.withLock { $0 })
        try await server.close()
    }

    @Test func answersRedacted() {
        let answer = AuthAnswer.responses(["123456"])
        #expect(!String(describing: answer).contains("123456"))
        #expect(!String(reflecting: answer).contains("123456"))
    }

    /// With the fallback off, a refused key ends as a login failure the caller can explain,
    /// not as a password or code prompt the user never asked for.
    @Test func refusedKeyWithoutFallbackFailsAuth() async throws {
        let (server, port) = try await Self.startServer(RefusesKeys())
        let scratch = try KeygenScratch()
        try scratch.keygen(["-q", "-t", "ed25519", "-N", "", "-f", scratch.path("id")])
        let secrets = MemorySecretStore()
        let biometrics = BiometricSession(evaluator: CountingEvaluator())
        let keys = KeychainKeyStore(secrets: secrets, biometrics: biometrics)
        let key = try await keys.importKey(openSSH: String(contentsOfFile: scratch.path("id"), encoding: .utf8), passphrase: nil, label: "k")
        let prompter = TwoFactorPrompter(responses: nil)
        let profile = HostProfile(
            id: UUID(), name: "key", endpoint: HostEndpoint(host: "127.0.0.1", port: port, user: "u"), auth: .key(id: key.id)
        )
        let connection = SSHConnection(
            profile: profile, keys: keys, secrets: secrets, biometrics: biometrics,
            hostKeys: FileHostKeyStore(url: SSHServerTests.scratchURL()), prompter: prompter, credentialFallback: false
        )

        await #expect(throws: SSHError.authFailed(tried: ["publickey"])) { try await connection.connect() }
        #expect(prompter.asked.withLock { $0 }.isEmpty)
        try await server.close()
    }

    /// A key the server accepts with a second step still reaches the prompter with the fallback off.
    @Test func acceptedKeyWithoutFallbackStillAsksForTheCode() async throws {
        let (server, port) = try await Self.startServer(KeyThenCode())
        let scratch = try KeygenScratch()
        try scratch.keygen(["-q", "-t", "ed25519", "-N", "", "-f", scratch.path("id")])
        let secrets = MemorySecretStore()
        let biometrics = BiometricSession(evaluator: CountingEvaluator())
        let keys = KeychainKeyStore(secrets: secrets, biometrics: biometrics)
        let key = try await keys.importKey(openSSH: String(contentsOfFile: scratch.path("id"), encoding: .utf8), passphrase: nil, label: "k")
        let prompter = TwoFactorPrompter(responses: ["123456"])
        let profile = HostProfile(
            id: UUID(), name: "key", endpoint: HostEndpoint(host: "127.0.0.1", port: port, user: "u"), auth: .key(id: key.id)
        )
        let connection = SSHConnection(
            profile: profile, keys: keys, secrets: secrets, biometrics: biometrics,
            hostKeys: FileHostKeyStore(url: SSHServerTests.scratchURL()), prompter: prompter, credentialFallback: false
        )

        try await connection.connect()
        let asked = prompter.asked.withLock { $0 }
        #expect(asked.count == 1)
        if case .keyboardInteractive(_, _, let prompts)? = asked.first {
            #expect(prompts.map(\.text) == ["Verification code:"])
        } else {
            Issue.record("expected a keyboard-interactive prompt, got \(asked)")
        }
        await connection.close()
        try await server.close()
    }
}
