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
        let command = ETFactory.launchCommand("true", tag: "abc")
        #expect(command.contains("command -v etterminal"))
        #expect(command.contains("/opt/homebrew/bin/etterminal"))
    }

    /// etterminal runs under a tagged name, with the bootstrap's stdin, and the link is gone after.
    @Test func launchRunsETTerminalUnderItsTag() throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("et-launch-\(UUID().uuidString)")
        let bin = dir.appendingPathComponent("bin")
        try FileManager.default.createDirectory(at: bin, withIntermediateDirectories: true)
        let stub = bin.appendingPathComponent("etterminal")
        try "#!/bin/sh\nread line\necho \"$0 $1 $line\"\n".write(to: stub, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: stub.path)
        let bootstrap = "echo 'XXXabc/key_xterm' | \(ETFactory.bootstrapOptions.etterminalPath) --verbose=0"
        let output = try Self.shell(ETFactory.launchCommand(bootstrap, tag: "t1234"), path: bin.path, tmp: dir.path)
        #expect(output == "\(dir.path)/togo-et-t1234 --verbose=0 XXXabc/key_xterm\n")
        #expect(!FileManager.default.fileExists(atPath: dir.appendingPathComponent("togo-et-t1234").path))
    }

    @Test func stopEndsOnlyTheTaggedETTerminal() throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("et-stop-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let (ours, other) = (ETFactory.newTag(), ETFactory.newTag())
        var running: [Process] = []
        for tag in [ours, other] {
            let link = dir.appendingPathComponent("togo-et-\(tag)")
            try FileManager.default.createSymbolicLink(atPath: link.path, withDestinationPath: "/bin/sleep")
            let process = Process()
            process.executableURL = link
            process.arguments = ["30"]
            try process.run()
            running.append(process)
        }
        defer { running.forEach { $0.terminate() } }
        _ = try Self.shell(ETFactory.stopCommand(tag: ours), path: "/usr/bin:/bin", tmp: dir.path)
        running[0].waitUntilExit()
        #expect(running[0].terminationReason == .uncaughtSignal)
        #expect(running[1].isRunning)
    }

    private static func shell(_ command: String, path: String, tmp: String) throws -> String {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/sh")
        process.arguments = ["-c", command]
        process.environment = ["PATH": path + ":/usr/bin:/bin", "TMPDIR": tmp, "HOME": tmp]
        let out = Pipe()
        process.standardOutput = out
        try process.run()
        let data = out.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        return String(decoding: data, as: UTF8.self)
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
