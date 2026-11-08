import ETBootstrap
import Foundation
import Testing
@testable import TogoKit

struct ETTransportTests {
    static let passkey = Data("Zq7Kp2Lm9Xw4Rt8Vb3Nc6Hd1Fg5Js0Ya".utf8)

    @Test func passkeyRedacted() async throws {
        let endpoint = ETEndpoint(host: "build.local", port: 2022, clientID: "XXXabcdefghijklm", passkey: Self.passkey)
        let secret = String(decoding: Self.passkey, as: UTF8.self)
        #expect(!endpoint.description.contains(secret))
        #expect(!String(reflecting: endpoint).contains(secret))
        #expect(endpoint.description.contains("build.local:2022"))

        let transport = try ETTransport(endpoint: endpoint, argv: ["tmux"], cols: 80, rows: 24)
        #expect(!transport.description.contains(secret))
        #expect(!String(reflecting: transport).contains(secret))
    }

    @Test func attachCommandExecsQuotedArgv() {
        let argv = ["tmux", "attach", "-t", "my work"]
        #expect(ETFactory.attachInput(argv) == Data("exec \(shellQuote(argv))\n".utf8))
    }

    @Test func bootstrapLooksPastPath() {
        let path = ETFactory.bootstrapOptions.etterminalPath
        #expect(path.contains("command -v etterminal"))
        #expect(path.contains("/opt/homebrew/bin/etterminal"))
    }

    /// Needs Remote Login, the dedicated test key and a running `etserver` on this Mac:
    /// TOGO_IT_ET=1 TOGO_IT_USER=<user> TOGO_IT_KEY=~/.ssh/togo_it
    @Test(.enabled(if: ProcessInfo.processInfo.environment["TOGO_IT_ET"] == "1",
                   "set TOGO_IT_ET=1, TOGO_IT_USER and TOGO_IT_KEY with etserver running on this Mac"))
    func etAttachLocalhost() async throws {
        let env = ProcessInfo.processInfo.environment
        let user = try #require(env["TOGO_IT_USER"])
        let keyPath = NSString(string: try #require(env["TOGO_IT_KEY"])).expandingTildeInPath
        let secrets = MemorySecretStore()
        let biometrics = BiometricSession(evaluator: CountingEvaluator())
        let keys = KeychainKeyStore(secrets: secrets, biometrics: biometrics)
        let info = try await keys.importKey(openSSH: String(contentsOfFile: keyPath, encoding: .utf8), passphrase: nil, label: "it")
        let profile = HostProfile(
            id: UUID(), name: "localhost", endpoint: HostEndpoint(host: "127.0.0.1", user: user),
            auth: .key(id: info.id), etAvailable: true
        )
        let scratch = try KeygenScratch()
        let connection = SSHConnection(
            profile: profile, keys: keys, secrets: secrets, biometrics: biometrics,
            hostKeys: FileHostKeyStore(url: scratch.directory.appendingPathComponent("hosts.json")),
            prompter: ScriptedPrompter(trust: true)
        )
        try await connection.connect()
        let name = "togo-et-\(UUID().uuidString.prefix(8))"
        _ = try await connection.run(shellQuote(["tmux", "new-session", "-d", "-s", name, "printf ready; cat"]), timeout: .seconds(10))
        defer { Task { _ = try? await connection.run(shellQuote(["tmux", "kill-session", "-t", name]), timeout: .seconds(5)) } }

        let transport = try await ETFactory.make(
            AttachRequest(profile: profile, connection: connection, argv: ["tmux", "attach", "-t", name], cols: 80, rows: 24)
        )
        try await transport.connect()
        var seen = ""
        func waitFor(_ text: String) async {
            for await chunk in transport.incoming {
                seen += String(decoding: chunk, as: UTF8.self)
                if seen.contains(text) { return }
            }
        }
        await waitFor("ready")
        try await transport.send(Data("x".utf8))
        await waitFor("readyx")
        try await transport.resize(cols: 100, rows: 30)
        let started = ContinuousClock.now
        await transport.close()
        #expect(ContinuousClock.now - started < .seconds(2))
        await connection.close()
    }
}
