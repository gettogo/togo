import Foundation
@preconcurrency import LocalAuthentication
import Synchronization
import TogoKit

/// Adding a machine against the real network, keychain and trusted host keys.
@MainActor final class LiveOnboardingServices: OnboardingServices {
    /// How long a first connection waits before the machine counts as asleep. The copy on the
    /// unreachable screen names this number.
    static let connectTimeout: Duration = .seconds(30)
    private static let deviceKeyDefault = "deviceKeyID"

    /// The id of this phone's key once it has been made.
    static var storedDeviceKeyID: String? {
        UserDefaults.standard.string(forKey: deviceKeyDefault)
    }

    private let keys: KeychainKeyStore
    private let secrets: any SecretStore
    private let biometrics: BiometricSession
    private let hostKeys: any HostKeyStore

    init(keys: KeychainKeyStore, secrets: any SecretStore, biometrics: BiometricSession, hostKeys: any HostKeyStore) {
        self.keys = keys
        self.secrets = secrets
        self.biometrics = biometrics
        self.hostKeys = hostKeys
    }

    func discover() -> AsyncStream<DiscoveryEvent> {
        let browser = BonjourBrowser()
        return AsyncStream { continuation in
            let task = Task {
                await browser.start()
                await withTaskGroup(of: Void.self) { group in
                    group.addTask {
                        for await hosts in browser.results { continuation.yield(.hosts(hosts)) }
                    }
                    group.addTask {
                        for await status in browser.status {
                            switch status {
                            case .denied: continuation.yield(.denied)
                            case .browsing: continuation.yield(.browsing)
                            case .failed: break
                            }
                        }
                    }
                }
            }
            continuation.onTermination = { _ in
                task.cancel()
                Task { await browser.stop() }
            }
        }
    }

    /// The phone's own key, made on first use. On a device it lives in the Secure Enclave.
    func deviceKey() async throws -> KeyInfo {
        if let id = UserDefaults.standard.string(forKey: Self.deviceKeyDefault),
           let key = try await keys.list().first(where: { $0.id == id }) {
            return key
        }
        let key = try await keys.generateDeviceKey(label: String(localized: "onboard.key.deviceLabel", defaultValue: "Togo on this \(DeviceName.model)"))
        UserDefaults.standard.set(key.id, forKey: Self.deviceKeyDefault)
        return key
    }

    func importKey(openSSH: String, passphrase: String?, label: String) async throws -> ImportedKey {
        let key = try await keys.importKey(openSSH: openSSH, passphrase: passphrase, label: label)
        return ImportedKey(id: key.id, label: key.label)
    }

    func connect(
        _ profile: HostProfile, password: String?, answers: [String]?,
        trustNewKey: @escaping @Sendable (HostKeyPrompt) async -> Bool
    ) async -> ConnectOutcome {
        let prompter = OnboardingPrompter(password: password, answers: answers, trustNewKey: trustNewKey)
        // A key the machine does not list yet must end as a refused key, so the flow can show
        // the public key to add. Falling back to a password the form never asked for would
        // surface as a cancel instead.
        let keyOnly = if case .key = profile.auth { true } else { false }
        let typed = password.map { !$0.isEmpty } ?? false
        let secrets: any SecretStore = typed
            ? HidingSavedPassword(base: self.secrets, account: SSHConnection.passwordAccount(profile.id)) : self.secrets
        let connection = SSHConnection(
            profile: profile, keys: keys, secrets: secrets, biometrics: biometrics, hostKeys: hostKeys,
            prompter: prompter, connectTimeout: Self.connectTimeout, credentialFallback: !keyOnly
        )
        defer { Task { await connection.close() } }
        var signedIn = false
        do {
            try await connection.connect()
            signedIn = true
            return .ready(try await HelperProbe.run(on: connection))
        } catch {
            // The machine answered and let Togo in; only the check after it was slow, so
            // "asleep" would send the user the wrong way.
            if signedIn, (error as? SSHError) == .timeout {
                return .failed(String(
                    localized: "onboard.connect.probeTimeout",
                    defaultValue: "Signed in to \(profile.name), but checking it for the helper took too long. Try again."
                ))
            }
            let request = prompter.declinedPrompt.flatMap { SignInRequest($0, machine: profile.name) }
            return Self.outcome(for: error, promptDeclined: prompter.declined, request: request)
        }
    }

    /// `promptDeclined` means the machine asked for something the form has no answer for, such
    /// as a one-time code. The connection reports that as a cancel, but the user cancelled
    /// nothing; it is a login the machine did not accept.
    /// A declined prompt the user can answer, such as a code after the key was accepted, comes
    /// back as `needsAnswer` before anything reads as a refused key.
    nonisolated static func outcome(for error: any Error, promptDeclined: Bool, request: SignInRequest? = nil) -> ConnectOutcome {
        guard let error = error as? SSHError else { return .failed(ServiceErrors.message(error)) }
        if promptDeclined, let request {
            switch error {
            case .cancelled, .authFailed: return .needsAnswer(request)
            default: break
            }
        }
        switch error {
        case .hostKeyChanged(let host, let expected, let presented):
            return .hostKeyChanged(ChangedHostKey(hostID: host, trusted: expected, presented: presented))
        case .cancelled where promptDeclined:
            return .authFailed
        case .hostKeyRejected, .cancelled:
            return .cancelled
        case .authFailed:
            return .authFailed
        case .timeout:
            return .unreachable
        case .unreachable, .refused, .channelClosed:
            return .failed(ServiceErrors.message(error))
        }
    }

    func forgetHostKey(_ hostID: String) async throws {
        try await hostKeys.forget(hostID)
    }

    /// Behind user presence and `ThisDeviceOnly`, like the keys: never synced, never in a
    /// backup another device can restore, and read only after the biometric check.
    func setSavedPassword(_ password: String?, for machine: UUID) throws {
        let account = SSHConnection.passwordAccount(machine)
        guard let password else {
            try secrets.delete(account)
            return
        }
        try secrets.set(Data(password.utf8), for: account, requireUserPresence: true)
    }

    func keyLabel(id: String) -> String? {
        (try? keys.info(id: id))?.label
    }
}

/// The secret store with one machine's saved password hidden, so a password typed in the
/// form is the one tried rather than an older saved one.
private struct HidingSavedPassword: SecretStore {
    let base: any SecretStore
    let account: String

    func set(_ data: Data, for account: String, requireUserPresence: Bool) throws {
        try base.set(data, for: account, requireUserPresence: requireUserPresence)
    }

    func get(_ account: String, context: LAContext?) throws -> Data? {
        account == self.account ? nil : try base.get(account, context: context)
    }

    func delete(_ account: String) throws {
        try base.delete(account)
    }
}

/// Answers the connection's questions from the form: the password typed there, the answers
/// the user gave to the machine's last question (once), and the host key check shown in the
/// flow. Password prompts inside keyboard-interactive are filled by the connection itself; any
/// other prompt is declined and kept, so the flow can ask the user.
private final class OnboardingPrompter: ConnectionPrompter {
    let password: String?
    let trustNewKey: @Sendable (HostKeyPrompt) async -> Bool
    private let state: Mutex<(answers: [String]?, declined: AuthPrompt?)>

    init(password: String?, answers: [String]?, trustNewKey: @escaping @Sendable (HostKeyPrompt) async -> Bool) {
        self.password = password
        self.trustNewKey = trustNewKey
        state = Mutex((answers, nil))
    }

    var declined: Bool { declinedPrompt != nil }
    var declinedPrompt: AuthPrompt? { state.withLock { $0.declined } }

    func answer(_ prompt: AuthPrompt) async -> AuthAnswer? {
        if case .password = prompt, let password { return .secret(password) }
        return state.withLock { state -> AuthAnswer? in
            if let given = state.answers {
                state.answers = nil
                switch prompt {
                case .password: return given.first.map(AuthAnswer.secret)
                case .keyboardInteractive(_, _, let prompts) where prompts.count == given.count: return .responses(given)
                default: break
                }
            }
            state.declined = prompt
            return nil
        }
    }

    func trustNewHostKey(host: String, keyType: String, fingerprint: String) async -> Bool {
        await trustNewKey(HostKeyPrompt(keyType: keyType, fingerprint: fingerprint))
    }
}
