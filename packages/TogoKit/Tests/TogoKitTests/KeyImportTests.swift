import Foundation
import Testing
@testable import TogoKit

struct KeyImportTests {
    private func makeStore() -> KeychainKeyStore {
        KeychainKeyStore(secrets: MemorySecretStore(), biometrics: BiometricSession(evaluator: CountingEvaluator()))
    }

    /// `ssh-keygen -y` prints "type base64" plus the comment; the comparison ignores comments.
    private func openSSHPublic(_ scratch: KeygenScratch, _ key: String, passphrase: String = "") throws -> String {
        let line = try scratch.keygen(["-y", "-P", passphrase, "-f", key])
        return line.split(separator: " ").prefix(2).joined(separator: " ")
    }

    private func typeAndBlob(_ line: String) -> String {
        line.split(separator: " ").prefix(2).joined(separator: " ")
    }

    @Test func importEd25519WithPassphrase() async throws {
        let scratch = try KeygenScratch()
        let key = try scratch.generate(type: "ed25519", passphrase: "pw")
        let text = try String(contentsOfFile: key, encoding: .utf8)
        let store = makeStore()
        let info = try await store.importKey(openSSH: text, passphrase: "pw", label: "laptop")
        #expect(typeAndBlob(info.publicKeyLine) == (try openSSHPublic(scratch, key, passphrase: "pw")))
        #expect(info.kind == .ed25519)
        let comment = String(info.publicKeyLine.split(separator: " ").last ?? "")
        #expect(comment.hasPrefix("togo-"))
        await #expect(throws: KeyError.badPassphrase) {
            try await store.importKey(openSSH: text, passphrase: "wrong", label: "x")
        }
        await #expect(throws: KeyError.badPassphrase) {
            try await store.importKey(openSSH: text, passphrase: nil, label: "x")
        }
        #expect(try await store.list().map(\.id) == [info.id])
    }

    @Test func importRSAAndECDSA() async throws {
        let scratch = try KeygenScratch()
        let store = makeStore()
        let rsa = try scratch.generate(type: "rsa", bits: 3072)
        let rsaInfo = try await store.importKey(openSSH: String(contentsOfFile: rsa, encoding: .utf8), passphrase: nil, label: "rsa")
        #expect(typeAndBlob(rsaInfo.publicKeyLine) == (try openSSHPublic(scratch, rsa)))
        #expect(rsaInfo.kind == .rsa)

        // Citadel 0.12.1 has no OpenSSH ECDSA parser; the import says so instead of failing vaguely.
        let ecdsa = try scratch.generate(type: "ecdsa", bits: 256)
        await #expect(throws: KeyError.unsupportedKeyType("ecdsa-sha2-nistp256")) {
            try await store.importKey(openSSH: String(contentsOfFile: ecdsa, encoding: .utf8), passphrase: nil, label: "ec")
        }
    }

    @Test func fingerprintOfImportedKeyMatchesOpenSSH() async throws {
        let scratch = try KeygenScratch()
        let key = try scratch.generate(type: "ed25519")
        let info = try await makeStore().importKey(openSSH: String(contentsOfFile: key, encoding: .utf8), passphrase: nil, label: "k")
        let expected = try scratch.keygen(["-l", "-E", "sha256", "-f", key]).split(separator: " ")[1]
        #expect(info.fingerprint == String(expected))
    }

    @Test func importedKeyLoadsForSigningAndDeletes() async throws {
        let scratch = try KeygenScratch()
        let key = try scratch.generate(type: "rsa", bits: 2048, passphrase: "pw")
        let store = makeStore()
        let info = try await store.importKey(openSSH: String(contentsOfFile: key, encoding: .utf8), passphrase: "pw", label: "k")
        let loaded = try store.privateKey(id: info.id, context: .init())
        #expect(PublicKeyLine.fingerprint(for: loaded.publicKey) == info.fingerprint)
        try await store.delete(id: info.id)
        #expect(try await store.list().isEmpty)
    }

    @Test func keyFromAnotherDeviceIsMissingNotMalformed() {
        #expect(throws: KeyError.missing) {
            try makeStore().privateKey(id: "synced-from-another-device", context: .init())
        }
    }

    @Test func garbageIsMalformed() async {
        await #expect(throws: KeyError.malformed) {
            try await makeStore().importKey(openSSH: "not a key", passphrase: nil, label: "x")
        }
    }
}
