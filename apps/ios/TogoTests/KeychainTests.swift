import Foundation
import Testing
import TogoKit

struct KeychainTests {
    @Test func keychainSecretRoundTrip() throws {
        let store = KeychainSecretStore(service: "app.togo.tests.\(UUID().uuidString)")
        let secret = Data("not-a-real-secret".utf8)
        try store.set(secret, for: "account", requireUserPresence: false)
        #expect(try store.get("account", context: nil) == secret)
        try store.set(Data("replaced".utf8), for: "account", requireUserPresence: false)
        #expect(try store.get("account", context: nil) == Data("replaced".utf8))
        try store.delete("account")
        #expect(try store.get("account", context: nil) == nil)
    }
}
