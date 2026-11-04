import Foundation
@preconcurrency import Citadel
import Testing
@testable import TogoKit

/// Host key trust and transport teardown against an in-process server with a fixed key,
/// so they run in every `swift test`.
struct SSHHostKeyServerTests {
    private static func fingerprint(seed: UInt8) throws -> String {
        PublicKeyLine.fingerprint(for: try SSHServerTests.hostKey(seed).publicKey)
    }

    @Test func unknownHostKeyAsksThenStores() async throws {
        let (server, port) = try await SSHServerTests.startServer(seed: 4, name: "s")
        let endpoint = HostEndpoint(host: "127.0.0.1", port: port, user: "u")
        let store = FileHostKeyStore(url: SSHServerTests.scratchURL())

        let (first, firstPrompter) = SSHServerTests.connection(endpoint, hostKeys: store)
        try await first.connect()
        await first.close()
        #expect(firstPrompter.trustCalls.withLock { $0 } == 1)
        #expect(try await store.fingerprints(for: HostKeyTrust.hostID(endpoint)) == [try Self.fingerprint(seed: 4)])

        let (second, secondPrompter) = SSHServerTests.connection(endpoint, hostKeys: store)
        try await second.connect()
        await second.close()
        #expect(secondPrompter.trustCalls.withLock { $0 } == 0)
        try await server.close()
    }

    @Test func rejectedHostKeyThrows() async throws {
        let (server, port) = try await SSHServerTests.startServer(seed: 5, name: "s")
        let endpoint = HostEndpoint(host: "127.0.0.1", port: port, user: "u")
        let store = FileHostKeyStore(url: SSHServerTests.scratchURL())
        let (connection, _) = SSHServerTests.connection(endpoint, trust: false, hostKeys: store)
        await #expect(throws: SSHError.hostKeyRejected) { try await connection.connect() }
        #expect(try await store.fingerprints(for: HostKeyTrust.hostID(endpoint)).isEmpty)
        try await server.close()
    }

    @Test func changedHostKeyBlocksWithoutPrompt() async throws {
        let (server, port) = try await SSHServerTests.startServer(seed: 6, name: "s")
        let endpoint = HostEndpoint(host: "127.0.0.1", port: port, user: "u")
        let store = FileHostKeyStore(url: SSHServerTests.scratchURL())
        let old = try Self.fingerprint(seed: 7)
        try await store.trust(old, for: HostKeyTrust.hostID(endpoint))
        let (connection, prompter) = SSHServerTests.connection(endpoint, hostKeys: store)
        await #expect(throws: SSHError.hostKeyChanged(
            host: HostKeyTrust.hostID(endpoint), expected: [old], presented: try Self.fingerprint(seed: 6)
        )) { try await connection.connect() }
        #expect(prompter.trustCalls.withLock { $0 } == 0)
        try await server.close()
    }

    @Test func corruptTrustStoreBlocksConnect() async throws {
        let (server, port) = try await SSHServerTests.startServer(seed: 8, name: "s")
        let endpoint = HostEndpoint(host: "127.0.0.1", port: port, user: "u")
        let url = SSHServerTests.scratchURL()
        try Data("{not json".utf8).write(to: url)
        let store = FileHostKeyStore(url: url)
        let (connection, prompter) = SSHServerTests.connection(endpoint, hostKeys: store)
        await #expect(throws: HostKeyStoreError.unreadable(url.path)) { try await connection.connect() }
        #expect(prompter.trustCalls.withLock { $0 } == 0)
        await #expect(throws: HostKeyStoreError.self) { try await store.trust("SHA256:x", for: "[h]:22") }
        #expect(try Data(contentsOf: url) == Data("{not json".utf8))
        #expect(try Data(contentsOf: url.appendingPathExtension("corrupt")) == Data("{not json".utf8))
        try await server.close()
    }

    @Test func transportStopsReadingWhenConsumerFinishes() async throws {
        signal(SIGPIPE, SIG_IGN)
        let exec = EndlessExec()
        let (server, port) = try await SSHServerTests.startServer(seed: 9, name: "s", exec: exec)
        let (connection, _) = SSHServerTests.connection(HostEndpoint(host: "127.0.0.1", port: port, user: "u"))
        let transport = SSHTransport(connection: connection, argv: ["yes"], cols: 80, rows: 24)
        try await transport.connect()
        for await _ in transport.incoming { break }
        transport.incoming.finish()
        var last: TransportState?
        for await state in transport.state { last = state }
        #expect(last == .closed(.requested))
        await connection.close()
        try await server.close()
    }
}

/// Writes output until the channel goes away, like `yes`.
private final class EndlessExec: ExecDelegate {
    private struct Context: ExecCommandContext {
        func terminate() async throws {}
    }

    func start(command: String, outputHandler: ExecOutputHandler) async throws -> any ExecCommandContext {
        let output = outputHandler.stdoutPipe.fileHandleForWriting
        Task.detached {
            let chunk = Data(repeating: UInt8(ascii: "y"), count: 4096)
            while (try? output.write(contentsOf: chunk)) != nil {}
        }
        return Context()
    }

    func setEnvironmentValue(_ value: String, forKey key: String) async throws {}
}
