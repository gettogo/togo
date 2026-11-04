import Foundation
import Testing
@testable import TogoKit

/// Runs ssh-keygen in a fresh scratch directory; the tests compare against OpenSSH itself.
struct KeygenScratch {
    let directory: URL

    init() throws {
        directory = FileManager.default.temporaryDirectory.appendingPathComponent("togo-keys-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    }

    func path(_ name: String) -> String { directory.appendingPathComponent(name).path }

    @discardableResult
    func keygen(_ arguments: [String]) throws -> String {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/ssh-keygen")
        process.arguments = arguments
        let out = Pipe()
        process.standardOutput = out
        process.standardError = Pipe()
        try process.run()
        let data = out.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        guard process.terminationStatus == 0 else { throw CocoaError(.executableLoad) }
        return String(decoding: data, as: UTF8.self)
    }

    /// Generates a key without passphrase unless one is given; returns the private key path.
    func generate(type: String, bits: Int? = nil, passphrase: String = "") throws -> String {
        let file = path("\(type)-\(UUID().uuidString)")
        var arguments = ["-q", "-t", type, "-N", passphrase, "-C", "test", "-f", file]
        if let bits { arguments += ["-b", String(bits)] }
        try keygen(arguments)
        return file
    }
}

struct HostKeyTrustTests {
    @Test func fingerprintMatchesOpenSSH() throws {
        let scratch = try KeygenScratch()
        let key = try scratch.generate(type: "ed25519")
        let line = try String(contentsOfFile: key + ".pub", encoding: .utf8)
        let blob = try #require(Data(base64Encoded: String(line.split(separator: " ")[1])))
        let expected = try scratch.keygen(["-l", "-E", "sha256", "-f", key + ".pub"]).split(separator: " ")[1]
        #expect(HostKeyTrust.fingerprint(blob: blob) == String(expected))
    }

    @Test func hostIDIsBracketedAndLowercased() {
        #expect(HostKeyTrust.hostID(HostEndpoint(host: "Mac.Local", port: 2222, user: "u")) == "[mac.local]:2222")
    }

    @Test func concurrentTrustKeepsBoth() async throws {
        let scratch = try KeygenScratch()
        let url = scratch.directory.appendingPathComponent("known_hosts.json")
        let store = FileHostKeyStore(url: url)
        try await withThrowingTaskGroup(of: Void.self) { group in
            for index in 0..<20 {
                group.addTask { try await store.trust("SHA256:fp\(index)", for: "[host\(index)]:22") }
            }
            try await group.waitForAll()
        }
        let saved = try JSONDecoder().decode([String: [String]].self, from: Data(contentsOf: url))
        #expect(saved.count == 20)
        let mode = try FileManager.default.attributesOfItem(atPath: url.path)[.posixPermissions] as? Int
        #expect(mode == 0o600)
    }

    @Test func forgetRemovesHost() async throws {
        let scratch = try KeygenScratch()
        let url = scratch.directory.appendingPathComponent("known_hosts.json")
        let store = FileHostKeyStore(url: url)
        try await store.trust("SHA256:a", for: "[h]:22")
        try await store.forget("[h]:22")
        #expect(try await FileHostKeyStore(url: url).fingerprints(for: "[h]:22").isEmpty)
    }
}
