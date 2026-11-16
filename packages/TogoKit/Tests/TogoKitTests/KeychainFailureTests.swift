import Foundation
import LocalAuthentication
import Security
import Synchronization
import Testing
@testable import TogoKit

struct KeychainFailureTests {
    @Test func failedReplacementNeverDeletesTheExistingSecret() throws {
        let calls = Mutex<[String]>([])
        let store = KeychainSecretStore(service: "test", operations: .init(
            add: { _ in calls.withLock { $0.append("add") }; return errSecInteractionNotAllowed },
            update: { _, _ in calls.withLock { $0.append("update") }; return errSecInteractionNotAllowed },
            delete: { _ in calls.withLock { $0.append("delete") }; return errSecSuccess }
        ))
        #expect(throws: SecretStoreError(status: errSecInteractionNotAllowed)) {
            try store.set(Data("new".utf8), for: "existing", requireUserPresence: false)
        }
        #expect(calls.withLock { $0 } == ["update"])
    }

    @Test func missingSecretAddsAndConcurrentInsertRetriesOnlyTheUpdate() throws {
        let calls = Mutex<[String]>([])
        let store = KeychainSecretStore(service: "test", operations: .init(
            add: { _ in calls.withLock { $0.append("add") }; return errSecDuplicateItem },
            update: { _, _ in calls.withLock { values in
                values.append("update")
                return values.count == 1 ? errSecItemNotFound : errSecSuccess
            } },
            delete: { _ in calls.withLock { $0.append("delete") }; return errSecSuccess }
        ))
        try store.set(Data("new".utf8), for: "existing", requireUserPresence: false)
        #expect(calls.withLock { $0 } == ["update", "add", "update"])
    }

    @Test func failedIndexWriteDoesNotRemovePrivateKeyMaterial() async throws {
        let secrets = FailingIndexSecretStore()
        let store = KeychainKeyStore(secrets: secrets, biometrics: BiometricSession(evaluator: CountingEvaluator()))
        let scratch = try KeygenScratch()
        let path = try scratch.generate(type: "ed25519")
        let key = try await store.importKey(openSSH: String(contentsOfFile: path, encoding: .utf8), passphrase: nil, label: "test")
        let material = try #require(try secrets.get("key:\(key.id)", context: nil))
        secrets.rejectIndex.withLock { $0 = true }
        await #expect(throws: SecretStoreError(status: errSecInteractionNotAllowed)) {
            try await store.delete(id: key.id)
        }
        #expect(try secrets.get("key:\(key.id)", context: nil) == material)
        #expect(try await store.list().map(\.id) == [key.id])
    }
}

private final class FailingIndexSecretStore: SecretStore {
    let rejectIndex = Mutex(false)
    private let inner = MemorySecretStore()

    func set(_ data: Data, for account: String, requireUserPresence: Bool) throws {
        if account == "keys.index", rejectIndex.withLock({ $0 }) {
            throw SecretStoreError(status: errSecInteractionNotAllowed)
        }
        try inner.set(data, for: account, requireUserPresence: requireUserPresence)
    }

    func get(_ account: String, context: LAContext?) throws -> Data? { try inner.get(account, context: context) }
    func delete(_ account: String) throws { try inner.delete(account) }
}
