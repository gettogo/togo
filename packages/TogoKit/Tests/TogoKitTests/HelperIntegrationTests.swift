import Foundation
import Testing
import TogoKit

/// Runs the real helper over pipes. It gets its own TOGO_HOME, tmux label and
/// TMUX_TMPDIR so it never touches the `togo` label, the default socket or ~/.togo.
struct HelperIntegrationTests {
    @Test func realHelperOverPipes() async throws {
        let binary = try helperBinary()
        // tmux socket paths must fit in sun_path (104 bytes on macOS), which the
        // per-user temporary directory can exceed, so the sandbox lives under /tmp.
        let sandbox = URL(filePath: "/tmp/togo-it-\(UUID().uuidString.prefix(8))")
        let label = "togo-it-\(ProcessInfo.processInfo.processIdentifier)"
        try FileManager.default.createDirectory(at: sandbox.appending(path: "tmux"), withIntermediateDirectories: true)
        var environment = ProcessInfo.processInfo.environment
        environment["TOGO_HOME"] = sandbox.appending(path: "home").path
        environment["TOGO_TMUX_LABEL"] = label
        environment["TMUX_TMPDIR"] = sandbox.appending(path: "tmux").path
        environment["TMUX"] = nil
        defer {
            stopTmux(label: label, environment: environment)
            try? FileManager.default.removeItem(at: sandbox)
        }

        let channel = try ProcessChannel(executable: binary, arguments: ["serve", "--stdio"], environment: environment)
        let client = HelperClient(channel: channel)
        let hello = try await client.hello(client: HelperClient.testClient)
        #expect(hello.capabilities.contains("capture.v1"))

        let created = try await client.createSession(CreateSessionParams(agent: "shell", cwd: sandbox.path))
        let sessions = try await client.listSessions()
        #expect(sessions.map(\.id) == [created.id])
        #expect(sessions.first?.bib == "01")

        let argv = try await client.attachCommand(id: created.id)
        #expect(argv.count > 1 && argv[1] == "-u")
        _ = try await client.capture(id: created.id, lines: 10, join: true)

        try await client.killSession(id: created.id)
        #expect(try await client.listSessions().isEmpty)
        await client.close()
    }

    private func helperBinary() throws -> URL {
        if let path = ProcessInfo.processInfo.environment["TOGO_BIN"] {
            return URL(filePath: path)
        }
        // Tests/TogoKitTests/<file> -> packages/TogoKit -> repo root.
        let repo = URL(filePath: #filePath).deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent().deletingLastPathComponent()
        let binary = repo.appending(path: "helper/bin/togo")
        guard FileManager.default.isExecutableFile(atPath: binary.path) else {
            Issue.record("helper binary missing at \(binary.path): run make build")
            throw CancellationError()
        }
        return binary
    }

    private func stopTmux(label: String, environment: [String: String]) {
        let process = Process()
        process.executableURL = URL(filePath: "/usr/bin/env")
        process.arguments = ["tmux", "-L", label, "kill-server"]
        process.environment = environment
        process.standardError = FileHandle.nullDevice
        try? process.run()
        process.waitUntilExit()
    }
}

/// The helper's stdin and stdout as an `ExecChannel`, standing in for SSH.
private final class ProcessChannel: ExecChannel {
    let incoming = ByteStream()
    private let process = Process()
    private let input = Pipe()
    private let output = Pipe()

    init(executable: URL, arguments: [String], environment: [String: String]) throws {
        process.executableURL = executable
        process.arguments = arguments
        process.environment = environment
        process.standardInput = input
        process.standardOutput = output
        process.standardError = FileHandle.nullDevice
        try process.run()
        let handle = output.fileHandleForReading
        let incoming = incoming
        // availableData blocks; a detached task keeps that off the caller and ends at EOF.
        Task.detached {
            while true {
                let chunk = handle.availableData
                guard !chunk.isEmpty else { break }
                guard await incoming.write(chunk) else { break }
            }
            incoming.finish()
        }
    }

    func write(_ data: Data) async throws {
        try input.fileHandleForWriting.write(contentsOf: data)
    }

    func close() async {
        try? input.fileHandleForWriting.close()
        process.waitUntilExit()
        incoming.finish()
    }
}
