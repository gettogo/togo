import Foundation
@preconcurrency import LocalAuthentication
import Security
import Synchronization
import Testing
@testable import TogoKit

struct PasswordCredentialRecoveryTests {
    @Test(arguments: [errSecNotAvailable, errSecInteractionNotAllowed])
    func temporaryKeychainFailureDoesNotAskForAnotherPassword(_ status: Int32) async {
        let prompter = CancelPasswordPrompter()
        let connection = connection(status: status, prompter: prompter)
        await #expect(throws: SecretStoreError(status: status)) { try await connection.connect() }
        #expect(prompter.asked.withLock { $0 } == 0)
        await connection.close()
    }

    @Test func rejectedUnlockStillOffersPasswordRecoveryOnce() async {
        let prompter = CancelPasswordPrompter()
        let connection = connection(status: errSecAuthFailed, prompter: prompter)
        await #expect(throws: SecretStoreError(status: errSecAuthFailed)) { try await connection.connect() }
        #expect(prompter.asked.withLock { $0 } == 1)
        await connection.close()
    }

    private func connection(status: Int32, prompter: CancelPasswordPrompter) -> SSHConnection {
        let secrets = UnavailablePasswordStore(status: status)
        let biometrics = BiometricSession(evaluator: CountingEvaluator())
        return SSHConnection(
            profile: HostProfile(id: UUID(), name: "Test", endpoint: HostEndpoint(host: "127.0.0.1", port: 1, user: "test"), auth: .password),
            keys: KeychainKeyStore(secrets: secrets, biometrics: biometrics), secrets: secrets, biometrics: biometrics,
            hostKeys: FileHostKeyStore(url: FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)),
            prompter: prompter
        )
    }
}

private struct UnavailablePasswordStore: SecretStore {
    let status: Int32
    func set(_ data: Data, for account: String, requireUserPresence: Bool) throws {}
    func get(_ account: String, context: LAContext?) throws -> Data? { throw SecretStoreError(status: status) }
    func delete(_ account: String) throws {}
}

private final class CancelPasswordPrompter: ConnectionPrompter {
    let asked = Mutex(0)
    func answer(_ prompt: AuthPrompt) async -> AuthAnswer? { asked.withLock { $0 += 1 }; return nil }
    func trustNewHostKey(host: String, keyType: String, fingerprint: String) async -> Bool { false }
}
