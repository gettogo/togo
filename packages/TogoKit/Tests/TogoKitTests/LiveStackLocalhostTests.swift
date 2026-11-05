import Foundation
import Testing
import TogoKit

/// The pieces the app runs on, end to end against this Mac: a monitor over the helper lists a
/// session the helper made, and a terminal link attaches to it. The helper runs through a
/// wrapper that gives it its own TOGO_HOME, tmux label and socket directory, so it never
/// touches ~/.togo or the `togo` label.
///
/// Needs Remote Login, the owner's test key and a built helper:
/// TOGO_IT_SSH=1 TOGO_IT_USER=<user> TOGO_IT_KEY=~/.ssh/togo_it, then `make build`.
struct LiveStackLocalhostTests {
    static var enabled: Bool { ProcessInfo.processInfo.environment["TOGO_IT_SSH"] == "1" }

    @Test(
        .enabled(if: enabled, "set TOGO_IT_SSH=1, TOGO_IT_USER and TOGO_IT_KEY to run against localhost"),
        .timeLimit(.minutes(1))
    )
    func monitorListsSessionAndTerminalAttaches() async throws {
        let sandbox = URL(filePath: "/tmp/togo-live-\(UUID().uuidString.prefix(8))")
        let label = "togo-live-\(ProcessInfo.processInfo.processIdentifier)"
        defer { cleanUp(sandbox: sandbox, label: label) }
        let wrapper = try makeWrapper(in: sandbox, label: label)

        let env = try await environment(scratch: sandbox)
        let user = try #require(ProcessInfo.processInfo.environment["TOGO_IT_USER"])
        let keyID = try await importKey(into: env)
        let profile = HostProfile(
            id: UUID(), name: "localhost", endpoint: HostEndpoint(host: "127.0.0.1", user: user), auth: .key(id: keyID)
        )
        let record = HostRecord(id: profile.id, profile: profile, symbol: "laptop", helperPath: wrapper.path)
        // Adding the machine trusts its host key; the monitor itself never asks.
        let onboarding = env.connection(for: profile)
        try await onboarding.connect()
        await onboarding.close()

        let monitor = HostMonitor(
            record: record, factory: { HelperSource(record: $0, env: env) }, path: StaticPath()
        )
        await monitor.start()
        let first = try #require(await snapshot(from: monitor.snapshots) { $0.reach == .online })
        #expect(first.helper != .missing)
        #expect(first.capabilities.contains(Capability.events))

        let created = try #require(try await monitor.perform(.create(CreateSessionParams(agent: "shell", cwd: sandbox.path))))
        let listed = try #require(await snapshot(from: monitor.snapshots) { $0.sessions.contains { $0.id == created.id } })
        #expect(listed.sessions.first { $0.id == created.id }?.bib == "01")

        let link = TerminalLink(
            connect: SessionAttach.connect(profile: profile, sessionID: created.id, env: env, helperPath: wrapper.path),
            path: StaticPath(), cols: 80, rows: 24
        )
        await link.start()
        var states = link.states.makeAsyncIterator()
        var live = false
        while let state = await states.next() {
            if case .live = state { live = true; break }
            if case .failed(let reason) = state { Issue.record("link failed: \(reason)"); break }
        }
        #expect(live)
        await link.send(Data("echo togo-live-ok\r".utf8))
        var output = ""
        let deadline = ContinuousClock.now + .seconds(10)
        while !output.contains("togo-live-ok\r\n"), ContinuousClock.now < deadline, let chunk = await link.read() {
            output += String(decoding: chunk, as: UTF8.self)
        }
        #expect(output.contains("togo-live-ok"))
        await link.stop()

        _ = try await monitor.perform(.kill(created.id))
        await monitor.stop()
    }

    private func environment(scratch: URL) async throws -> SessionEnvironment {
        let secrets = MemorySecretStore()
        let biometrics = BiometricSession(evaluator: CountingEvaluator())
        return SessionEnvironment(
            keys: KeychainKeyStore(secrets: secrets, biometrics: biometrics), secrets: secrets, biometrics: biometrics,
            hostKeys: FileHostKeyStore(url: scratch.appending(path: "known_hosts.json")),
            prompter: ScriptedPrompter(trust: true), connector: Connector(factories: [.ssh])
        )
    }

    private func importKey(into env: SessionEnvironment) async throws -> String {
        let path = NSString(string: try #require(ProcessInfo.processInfo.environment["TOGO_IT_KEY"])).expandingTildeInPath
        let key = try await env.keys.importKey(openSSH: String(contentsOfFile: path, encoding: .utf8), passphrase: nil, label: "it")
        return key.id
    }

    /// The helper record names this script, as it would name a helper outside PATH.
    private func makeWrapper(in sandbox: URL, label: String) throws -> URL {
        let repo = URL(filePath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        let binary = repo.appending(path: "helper/bin/togo")
        try #require(FileManager.default.isExecutableFile(atPath: binary.path), "run make build first")
        try FileManager.default.createDirectory(at: sandbox.appending(path: "tmux"), withIntermediateDirectories: true)
        let script = sandbox.appending(path: "togo")
        let body = """
        #!/bin/sh
        unset TMUX
        export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
        export TOGO_HOME='\(sandbox.appending(path: "home").path)'
        export TOGO_TMUX_LABEL='\(label)'
        export TMUX_TMPDIR='\(sandbox.appending(path: "tmux").path)'
        exec '\(binary.path)' "$@"

        """
        try body.write(to: script, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: script.path)
        return script
    }

    private func snapshot(
        from stream: AsyncStream<HostSnapshot>, where match: @escaping @Sendable (HostSnapshot) -> Bool
    ) async -> HostSnapshot? {
        await withTaskGroup(of: HostSnapshot?.self) { group in
            group.addTask {
                for await snapshot in stream where match(snapshot) { return snapshot }
                return nil
            }
            group.addTask {
                try? await Task.sleep(for: .seconds(20))
                return nil
            }
            let first = await group.next() ?? nil
            group.cancelAll()
            return first
        }
    }

    /// Stops the helper's daemon and tmux server for this sandbox only.
    private func cleanUp(sandbox: URL, label: String) {
        run("/usr/bin/env", [
            "PATH=/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin", "TMUX_TMPDIR=\(sandbox.appending(path: "tmux").path)",
            "tmux", "-L", label, "kill-server",
        ])
        let lock = sandbox.appending(path: "home/run/daemon.lock").path
        let pids = run("/usr/sbin/lsof", ["-t", lock]).split(whereSeparator: \.isNewline).compactMap { pid_t($0) }
        for pid in pids { kill(pid, SIGTERM) }
        try? FileManager.default.removeItem(at: sandbox)
    }

    @discardableResult
    private func run(_ executable: String, _ arguments: [String]) -> String {
        let process = Process()
        let output = Pipe()
        process.executableURL = URL(filePath: executable)
        process.arguments = arguments
        process.standardOutput = output
        process.standardError = FileHandle.nullDevice
        guard (try? process.run()) != nil else { return "" }
        let text = String(decoding: output.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
        process.waitUntilExit()
        return text
    }
}

/// A network that never changes.
private struct StaticPath: NetworkPathSource {
    var changes: AsyncStream<Void> { AsyncStream { _ in } }
}
