import Foundation
import Synchronization
import Testing
@testable import TogoKit

final class ScriptedPrompter: ConnectionPrompter {
    let trustAnswer: Bool
    let password: String?
    let trustCalls = Mutex(0)

    init(trust: Bool, password: String? = nil) {
        trustAnswer = trust
        self.password = password
    }

    func answer(_ prompt: AuthPrompt) async -> AuthAnswer? {
        password.map(AuthAnswer.secret)
    }

    func trustNewHostKey(host: String, keyType: String, fingerprint: String) async -> Bool {
        trustCalls.withLock { $0 += 1 }
        return trustAnswer
    }
}

/// Needs Remote Login on this Mac and the owner's dedicated test key:
/// TOGO_IT_SSH=1 TOGO_IT_USER=<user> TOGO_IT_KEY=~/.ssh/togo_it
struct SSHLocalhostTests {
    struct Setup {
        let connection: SSHConnection
        let prompter: ScriptedPrompter
    }

    static var enabled: Bool { ProcessInfo.processInfo.environment["TOGO_IT_SSH"] == "1" }

    private func setup(hostKeys: FileHostKeyStore? = nil) async throws -> Setup {
        let env = ProcessInfo.processInfo.environment
        let user = try #require(env["TOGO_IT_USER"])
        let keyPath = NSString(string: try #require(env["TOGO_IT_KEY"])).expandingTildeInPath
        let secrets = MemorySecretStore()
        let biometrics = BiometricSession(evaluator: CountingEvaluator())
        let keys = KeychainKeyStore(secrets: secrets, biometrics: biometrics)
        let info = try await keys.importKey(openSSH: String(contentsOfFile: keyPath, encoding: .utf8), passphrase: nil, label: "it")
        let profile = HostProfile(
            id: UUID(), name: "localhost", endpoint: HostEndpoint(host: "127.0.0.1", user: user), auth: .key(id: info.id)
        )
        let scratch = try KeygenScratch()
        let store = hostKeys ?? FileHostKeyStore(url: scratch.directory.appendingPathComponent("hosts.json"))
        let prompter = ScriptedPrompter(trust: true)
        let connection = SSHConnection(
            profile: profile, keys: keys, secrets: secrets, biometrics: biometrics, hostKeys: store, prompter: prompter
        )
        return Setup(connection: connection, prompter: prompter)
    }

    @Test(.enabled(if: enabled, "set TOGO_IT_SSH=1, TOGO_IT_USER and TOGO_IT_KEY to run against localhost"))
    func localhostKeyAuth() async throws {
        let scratch = try KeygenScratch()
        let store = FileHostKeyStore(url: scratch.directory.appendingPathComponent("hosts.json"))
        let first = try await setup(hostKeys: store)
        try await first.connection.connect()
        #expect(first.prompter.trustCalls.withLock { $0 } == 1)
        #expect(await first.connection.remoteAddress == "127.0.0.1")
        let result = try await first.connection.run("echo ok; echo err >&2; exit 3", timeout: .seconds(10))
        #expect(result.stdout == Data("ok\n".utf8))
        #expect(result.stderr == Data("err\n".utf8))
        #expect(result.status == 3)

        let helper = try await first.connection.openExec(shellCommand("exec \"$HOME/.togo/bin/togo\" serve --stdio"))
        let hello = #"{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol":1,"client":{"name":"togo-it","version":"0"},"capabilities":[]}}"#
        try await helper.write(Frame.encode(Data(hello.utf8)))
        var decoder = FrameDecoder()
        var reply: Data?
        for await chunk in helper.incoming {
            if let frame = try decoder.append(chunk).first {
                reply = frame
                break
            }
        }
        let text = String(decoding: try #require(reply), as: UTF8.self)
        #expect(text.contains(#""id":1"#) && text.contains("result"))
        await helper.close()
        await first.connection.close()

        let second = try await setup(hostKeys: store)
        try await second.connection.connect()
        #expect(second.prompter.trustCalls.withLock { $0 } == 0)
        await second.connection.close()
    }

    @Test(.enabled(if: enabled, "set TOGO_IT_SSH=1 to run against localhost"))
    func changedHostKeyBlocksWithoutPrompt() async throws {
        let scratch = try KeygenScratch()
        let store = FileHostKeyStore(url: scratch.directory.appendingPathComponent("hosts.json"))
        try await store.trust("SHA256:stale", for: "[127.0.0.1]:22")
        let setup = try await setup(hostKeys: store)
        do {
            try await setup.connection.connect()
            Issue.record("connected despite a changed host key")
        } catch let SSHError.hostKeyChanged(host, expected, presented) {
            #expect(host == "[127.0.0.1]:22")
            #expect(expected == ["SHA256:stale"])
            #expect(presented.hasPrefix("SHA256:"))
        }
        #expect(setup.prompter.trustCalls.withLock { $0 } == 0)
    }

    @Test(.enabled(if: enabled, "set TOGO_IT_SSH=1 to run against localhost"))
    func ptyTransportEchoesAndResizes() async throws {
        let setup = try await setup()
        let transport = SSHTransport(
            connection: setup.connection, argv: ["sh", "-c", "printf %s \"it's\"; sleep 1; stty size"], cols: 80, rows: 24
        )
        try await transport.connect()
        try await transport.resize(cols: 100, rows: 30)
        var output = Data()
        for await chunk in transport.incoming { output += chunk }
        let text = String(decoding: output, as: UTF8.self)
        #expect(text.hasPrefix("it's"))
        #expect(text.contains("30 100"))
        var last: TransportState?
        for await state in transport.state { last = state }
        #expect(last == .closed(.remoteExited(status: 0)))
        await setup.connection.close()
    }

    @Test(.enabled(if: enabled, "set TOGO_IT_SSH=1 to run against localhost"))
    func rejectedHostKeyThrows() async throws {
        let env = ProcessInfo.processInfo.environment
        let secrets = MemorySecretStore()
        let biometrics = BiometricSession(evaluator: CountingEvaluator())
        let keys = KeychainKeyStore(secrets: secrets, biometrics: biometrics)
        let info = try await keys.importKey(
            openSSH: String(contentsOfFile: NSString(string: env["TOGO_IT_KEY"] ?? "").expandingTildeInPath, encoding: .utf8),
            passphrase: nil, label: "it"
        )
        let scratch = try KeygenScratch()
        let connection = SSHConnection(
            profile: HostProfile(id: UUID(), name: "l", endpoint: HostEndpoint(host: "127.0.0.1", user: env["TOGO_IT_USER"] ?? ""), auth: .key(id: info.id)),
            keys: keys, secrets: secrets, biometrics: biometrics,
            hostKeys: FileHostKeyStore(url: scratch.directory.appendingPathComponent("h.json")),
            prompter: ScriptedPrompter(trust: false)
        )
        await #expect(throws: SSHError.hostKeyRejected) { try await connection.connect() }
    }
}
