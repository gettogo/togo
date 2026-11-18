import Foundation
import LocalAuthentication
import Security
import Synchronization
import Testing
@testable import TogoKit

struct SecretPresenceTests {
    @Test func keychainPresenceUsesOnlyMetadataAndDisallowsAuthenticationUI() throws {
        let inspected = Mutex(false)
        let store = KeychainSecretStore(service: "test", operations: .init(
            add: { _ in Issue.record("Unexpected add"); return errSecParam },
            update: { _, _ in Issue.record("Unexpected update"); return errSecParam },
            delete: { _ in Issue.record("Unexpected delete"); return errSecParam },
            find: { query in
                let values = query as NSDictionary
                #expect(values[kSecReturnData] == nil)
                #expect(values[kSecReturnAttributes] as? Bool == true)
                #expect((values[kSecUseAuthenticationContext] as? LAContext)?.interactionNotAllowed == true)
                inspected.withLock { $0 = true }
                return errSecSuccess
            }
        ))
        #expect(try store.contains("password:test"))
        #expect(inspected.withLock { $0 })
    }

    @Test func memoryPresenceFollowsDeletionWithoutReadingASecret() throws {
        let store = MemorySecretStore()
        #expect(try !store.contains("test"))
        try store.set(Data("secret".utf8), for: "test", requireUserPresence: true)
        #expect(try store.contains("test"))
        try store.delete("test")
        #expect(try !store.contains("test"))
    }
}
