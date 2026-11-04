import Foundation
@preconcurrency import LocalAuthentication
import Synchronization
import Testing
import TogoKit
@testable import TogoMosh

struct MoshTransportTests {
    @Test func udpSilenceFails() async throws {
        let endpoint = MoshEndpoint(host: "127.0.0.1", port: 60_999, key: "4NeCCgvZFe2RnPgrcU1PQw")
        let transport = MoshTransport(endpoint: endpoint, cols: 80, rows: 24, firstReplyTimeout: .seconds(1))
        let started = ContinuousClock.now
        await #expect(throws: MoshConnectError.noUDPReply(address: "127.0.0.1")) {
            try await transport.connect()
        }
        #expect(ContinuousClock.now - started < .seconds(4))
        var states: [TransportState] = []
        for await state in transport.state { states.append(state) }
        #expect(states.first == .connecting)
        #expect(states.last.map { if case .closed = $0 { true } else { false } } == true)
        for await _ in transport.incoming {}
    }

    static var integrationEnabled: Bool { ProcessInfo.processInfo.environment["TOGO_IT_MOSH"] == "1" }

    @Test(.enabled(if: integrationEnabled, "set TOGO_IT_MOSH=1, TOGO_IT_USER and TOGO_IT_KEY to run against localhost"))
    func moshAttachLocalhost() async throws {
        let (connection, profile) = try await localhostConnection()
        try await connection.connect()
        let socket = "togo-mosh-it"
        let tmux = FileManager.default.fileExists(atPath: "/opt/homebrew/bin/tmux") ? "/opt/homebrew/bin/tmux" : "tmux"
        let argv = [tmux, "-L", socket, "new-session", "-s", "m\(UInt32.random(in: 0...UInt32.max))", "printf ready; cat"]
        let transport = try await MoshFactory.make(connection: connection, profile: profile, argv: argv, cols: 80, rows: 24)
        let output = Mutex(Data())
        let reader = Task {
            for await chunk in transport.incoming { output.withLock { $0 += chunk } }
        }
        try await transport.connect()
        #expect(await eventually { output.withLock { $0.range(of: Data("ready".utf8)) != nil } })

        output.withLock { $0 = Data() }
        try await transport.send(Data("x".utf8))
        #expect(await eventually { output.withLock { $0.range(of: Data("x".utf8)) != nil } })

        try await transport.resize(cols: 100, rows: 30)
        let started = ContinuousClock.now
        await transport.close()
        #expect(ContinuousClock.now - started < .seconds(2))
        await reader.value
        _ = try? await connection.run(shellCommand("\(tmux) -L \(socket) kill-server"), timeout: .seconds(5))
        await connection.close()
    }

    private func eventually(_ condition: @Sendable () -> Bool) async -> Bool {
        let deadline = ContinuousClock.now + .seconds(10)
        while ContinuousClock.now < deadline {
            if condition() { return true }
            try? await Task.sleep(for: .milliseconds(50))
        }
        return condition()
    }

    private func localhostConnection() async throws -> (SSHConnection, HostProfile) {
        let env = ProcessInfo.processInfo.environment
        let user = try #require(env["TOGO_IT_USER"])
        let keyPath = NSString(string: try #require(env["TOGO_IT_KEY"])).expandingTildeInPath
        let secrets = MemorySecretStore()
        let biometrics = BiometricSession(evaluator: AlwaysAllow())
        let keys = KeychainKeyStore(secrets: secrets, biometrics: biometrics)
        let info = try await keys.importKey(openSSH: String(contentsOfFile: keyPath, encoding: .utf8), passphrase: nil, label: "it")
        let profile = HostProfile(
            id: UUID(), name: "localhost", endpoint: HostEndpoint(host: "127.0.0.1", user: user), auth: .key(id: info.id)
        )
        let hostKeys = FileHostKeyStore(
            url: FileManager.default.temporaryDirectory.appendingPathComponent("mosh-it-\(UUID().uuidString).json")
        )
        let connection = SSHConnection(
            profile: profile, keys: keys, secrets: secrets, biometrics: biometrics, hostKeys: hostKeys, prompter: TrustOnce()
        )
        return (connection, profile)
    }
}

private struct AlwaysAllow: BiometricEvaluator {
    func evaluate(_ context: LAContext, reason: String) async throws {}
}

/// Accepts the first key it is shown; the host-key store is fresh for every test run.
private struct TrustOnce: ConnectionPrompter {
    func answer(_ prompt: AuthPrompt) async -> AuthAnswer? { nil }
    func trustNewHostKey(host: String, keyType: String, fingerprint: String) async -> Bool { true }
}

struct MoshStatusLineTests {
    private func chunk(_ text: String) -> Data { Data(text.utf8) }

    @Test func moshNotificationBarMeansRoaming() {
        #expect(MoshTransport.isSilenceStatusLine(chunk("\u{1B}[1;1H\u{1B}[0;37;44mmosh: Last contact 7 seconds ago. [To quit: Ctrl-^ .]")))
        #expect(MoshTransport.isSilenceStatusLine(chunk("\u{1B}[0;37;44mmosh: Last contact 2 minutes ago.")))
    }

    @Test func outputMentioningLastContactIsNotRoaming() {
        #expect(!MoshTransport.isSilenceStatusLine(chunk("Last contact: 2024-01-01\r\n")))
        #expect(!MoshTransport.isSilenceStatusLine(chunk("grep 'mosh: Last contact 7 seconds ago.' notes.txt\r\n")))
        #expect(!MoshTransport.isSilenceStatusLine(chunk("\u{1B}[0mmosh: Last contact 7 seconds ago.")))
    }
}

struct MoshClientTests {
    @Test func closeDuringPendingSendNeverWritesToAReusedDescriptor() async throws {
        let client = try MoshClient(cols: 80, rows: 24)
        client.start(
            host: "127.0.0.1", port: 60_998, key: "4NeCCgvZFe2RnPgrcU1PQw", predict: "adaptive",
            deliver: { _ in }, exited: { _ in }
        )
        let paste = Data(repeating: UInt8(ascii: "a"), count: 32 << 20)
        let sending = Task { try await client.send(paste) }
        try await Task.sleep(for: .milliseconds(200))
        client.closeInput()
        // Whatever reuses the closed descriptor number must receive none of the paste.
        var reused: [Int32] = [0, 0]
        #expect(pipe(&reused) == 0)
        _ = fcntl(reused[0], F_SETFL, O_NONBLOCK)
        await #expect(throws: POSIXError(.EPIPE)) { try await sending.value }
        var byte: UInt8 = 0
        #expect(read(reused[0], &byte, 1) == -1)
        close(reused[0])
        close(reused[1])
    }
}
