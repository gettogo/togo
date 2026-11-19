import Foundation
import Testing
@testable import TogoKit

/// Two fresh TogoKit SSH clients talk to a real helper and a private tmux server.
struct LoopbackHelperTests {
    @Test(.enabled(if: ProcessInfo.processInfo.environment["TOGO_LOOPBACK_E2E"] == "1",
                   "Set TOGO_LOOPBACK_E2E=1 and TOGO_BIN to a built helper"), .timeLimit(.minutes(1)))
    func aRestartKeepsTheSessionAndOnlyOnePhoneCanAnswer() async throws {
        let sandbox = try LoopbackHelperHome()
        defer { sandbox.cleanUp() }
        let records = FileHostKeyStore(url: sandbox.root.appending(path: "hosts.json"))
        let sshd = try LocalSSHD(home: sandbox)
        defer { sandbox.stop(sshd.process) }
        try #require(await eventually(.seconds(3)) {
            sshd.process.isRunning && LoopbackSocket.connect(port: sshd.endpoint.port) != nil
        })
        try await exercise(sandbox, sshd: sshd, hosts: records)
    }

    private func exercise(_ home: LoopbackHelperHome, sshd: LocalSSHD, hosts: FileHostKeyStore) async throws {
        let daemon = try home.launch(["daemon"])
        defer { home.stop(daemon) }
        try #require(await eventually(.seconds(8)) { home.ready(daemon) })
        let id = try await withPhone(sshd, hosts: hosts, trusts: 1) { client in
            let created = try await client.createSession(.init(agent: "shell", cwd: home.root.path))
            try #require(try await client.listSessions().map(\.id) == [created.id])
            home.stop(daemon)
            try #require(await eventually(.seconds(5)) { await client.hasEnded })
            return created.id
        }

        let replacement = try home.launch(["daemon"])
        defer { home.stop(replacement) }
        try #require(await eventually(.seconds(8)) { home.ready(replacement) })
        try await withPhone(sshd, hosts: hosts, trusts: 0) { first in
            try await withPhone(sshd, hosts: hosts, trusts: 0) { second in
                try #require(try await first.listSessions().map(\.id) == [id])
                try await answerOnce(home, id: id, first: first, second: second)
                try await first.killSession(id: id)
                #expect(try await second.listSessions().isEmpty)
            }
        }
    }

    private func withPhone<T>(_ sshd: LocalSSHD, hosts: FileHostKeyStore, trusts: Int,
                              run: (HelperClient) async throws -> T) async throws -> T {
        let secrets = MemorySecretStore()
        let biometrics = BiometricSession(evaluator: CountingEvaluator())
        let keys = KeychainKeyStore(secrets: secrets, biometrics: biometrics)
        let key = try await keys.importKey(openSSH: String(contentsOf: sshd.clientKey, encoding: .utf8),
                                           passphrase: nil, label: "loopback")
        let profile = HostProfile(id: UUID(), name: "loopback", endpoint: sshd.endpoint, auth: .key(id: key.id))
        let prompts = ScriptedPrompter(trust: true)
        let connection = SSHConnection(profile: profile, keys: keys, secrets: secrets, biometrics: biometrics,
                                       hostKeys: hosts, prompter: prompts)
        do {
            try await connection.connect()
            let channel = try await connection.openExec("togo-test-helper")
            let client = HelperClient(channel: channel, callTimeout: .seconds(5))
            do {
                let hello = try await client.hello(client: HelperClient.testClient)
                try #require(hello.capabilities.contains(Capability.approvals))
                #expect(prompts.trustCalls.withLock { $0 } == trusts)
                let result = try await run(client)
                await client.close()
                await connection.close()
                return result
            } catch {
                await client.close()
                throw error
            }
        } catch {
            await connection.close()
            throw error
        }
    }

    private func answerOnce(_ home: LoopbackHelperHome, id: String,
                            first: HelperClient, second: HelperClient) async throws {
        // A real ask opens only when a phone is reachable or a device can be notified.
        _ = try await first.subscribe(foreground: true)
        _ = try await second.subscribe(foreground: true)
        let output = Pipe()
        let ask = try home.launch(["ask", "--require-remote", "--timeout", "20", "Run the test workspace check?"],
                                  extra: ["TOGO_SESSION": id], stdout: output)
        defer { home.stop(ask) }
        try #require(await eventually(.seconds(8)) { (try? await first.approvals(sessionId: id).count) == 1 })
        let request = try #require(try await first.approvals(sessionId: id).first)
        try #require(try await second.approvals(sessionId: id).map(\.id) == [request.id])

        async let approved = attempt(first, id: request.id, decision: .approve)
        async let denied = attempt(second, id: request.id, decision: .deny)
        let outcomes = try await [approved, denied]
        try #require(outcomes.filter { $0 }.count == 1, "One answer must win")
        try #require(await eventually(.seconds(5)) { !ask.isRunning })
        let words = String(decoding: output.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
            .trimmingCharacters(in: .whitespacesAndNewlines)
        #expect(words == (outcomes[0] ? "Yes" : "No"))
        #expect(ask.terminationStatus == (outcomes[0] ? 0 : 1))
        #expect(try await first.approvals(sessionId: id).isEmpty)
        #expect(try await second.approvals(sessionId: id).isEmpty)
    }

    private func attempt(_ client: HelperClient, id: String, decision: Decision) async throws -> Bool {
        do {
            try await client.answer(requestId: id, decision: decision)
            return true
        } catch HelperError.conflict {
            return false
        } catch let error as HelperError where error.isSessionGone {
            return false
        }
    }
}

private struct LoopbackHelperHome: Sendable {
    let root: URL
    let binary: URL
    let environment: [String: String]

    init() throws {
        let path = try #require(ProcessInfo.processInfo.environment["TOGO_BIN"])
        try #require(FileManager.default.isExecutableFile(atPath: path), "TOGO_BIN must be executable")
        binary = URL(filePath: path).standardizedFileURL
        root = URL(filePath: "/tmp/togo-loop-\(UUID().uuidString.prefix(8))")
        let tmux = root.appending(path: "tmux").path
        let loginHome = root.appending(path: "user").path
        environment = [
            "PATH": "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin",
            "HOME": loginHome, "SHELL": "/bin/sh", "LANG": "en_US.UTF-8",
            "TOGO_HOME": root.appending(path: "state").path,
            "TOGO_TMUX_LABEL": "togo-loopback", "TMUX_TMPDIR": tmux,
            "TOGO_TEST_AWAKE_OFF": "1",
        ]
        let files = FileManager.default
        try files.createDirectory(atPath: root.path, withIntermediateDirectories: false,
                                  attributes: [.posixPermissions: 0o700])
        do {
            for path in [tmux, loginHome] { try files.createDirectory(atPath: path, withIntermediateDirectories: false) }
        } catch {
            try? files.removeItem(at: root)
            throw error
        }
    }

    func launch(_ arguments: [String], extra: [String: String] = [:], stdin: Pipe? = nil,
                stdout: Pipe? = nil, stderr: Pipe? = nil) throws -> Process {
        let child = Process()
        child.executableURL = binary
        child.arguments = arguments
        child.environment = environment.merging(extra) { _, new in new }
        child.currentDirectoryURL = root
        child.standardInput = stdin.map { $0 as Any } ?? FileHandle.nullDevice
        child.standardOutput = stdout.map { $0 as Any } ?? FileHandle.nullDevice
        child.standardError = stderr.map { $0 as Any } ?? FileHandle.nullDevice
        try child.run()
        return child
    }

    func ready(_ daemon: Process) -> Bool {
        daemon.isRunning && FileManager.default.fileExists(atPath: root.appending(path: "state/run/togo.sock").path)
    }

    func stop(_ child: Process) {
        guard child.isRunning else { return }
        child.terminate()
        let limit = ContinuousClock.now + .seconds(3)
        while child.isRunning && .now < limit { Thread.sleep(forTimeInterval: 0.02) }
        if child.isRunning { kill(child.processIdentifier, SIGKILL) }
    }

    func cleanUp() {
        let tmux = Process()
        tmux.executableURL = URL(filePath: "/usr/bin/env")
        tmux.arguments = ["tmux", "-L", "togo-loopback", "kill-server"]
        tmux.environment = environment
        tmux.standardError = FileHandle.nullDevice
        if (try? tmux.run()) != nil { tmux.waitUntilExit() }
        try? FileManager.default.removeItem(at: root)
    }
}

/// A private OpenSSH instance accepts only its fresh key and one forced command.
private struct LocalSSHD {
    let endpoint: HostEndpoint
    let process: Process
    let clientKey: URL

    init(home: LoopbackHelperHome) throws {
        let root = home.root
        let hostKey = root.appending(path: "host-key")
        clientKey = root.appending(path: "client-key")
        for key in [hostKey, clientKey] {
            let generator = Process()
            generator.executableURL = URL(filePath: "/usr/bin/ssh-keygen")
            generator.arguments = ["-q", "-t", "ed25519", "-N", "", "-f", key.path]
            generator.standardInput = FileHandle.nullDevice
            try generator.run()
            generator.waitUntilExit()
            try #require(generator.terminationStatus == 0)
        }
        let port = LoopbackSocket.freePort()
        let user = NSUserName()
        endpoint = HostEndpoint(host: "127.0.0.1", port: port, user: user)
        let env = home.environment.sorted { $0.key < $1.key }.map { "\($0.key)=\($0.value)" }
        let helper = shellQuote(["/usr/bin/env", "-i"] + env + [home.binary.path, "serve", "--stdio"])
        let config = root.appending(path: "sshd_config")
        try """
        ListenAddress 127.0.0.1
        Port \(port)
        PidFile \(root.appending(path: "sshd.pid").path)
        HostKey \(hostKey.path)
        AuthorizedKeysFile \(clientKey.path).pub
        AuthenticationMethods publickey
        StrictModes no
        UsePAM no
        PasswordAuthentication no
        KbdInteractiveAuthentication no
        AllowUsers \(user)
        AllowTcpForwarding no
        X11Forwarding no
        AllowAgentForwarding no
        PermitTTY no
        PermitUserEnvironment no
        PermitUserRC no
        ForceCommand exec \(helper)

        """.write(to: config, atomically: true, encoding: .utf8)
        process = Process()
        process.executableURL = URL(filePath: "/usr/sbin/sshd")
        process.arguments = ["-D", "-e", "-f", config.path]
        process.standardInput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        try process.run()
    }
}
