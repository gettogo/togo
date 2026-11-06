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
            stopDaemon(home: sandbox.appending(path: "home"))
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

        let snapshot = try await client.subscribe(foreground: false)
        #expect(snapshot.sessions.map(\.id) == [created.id])
        #expect(snapshot.approvals.isEmpty)
        #expect(try await client.approvals(sessionId: created.id).isEmpty)
        let muted = try await client.setMute(id: created.id, muted: true)
        #expect(muted.muted)
        let mutedEvent = try await firstEvent(client.notifications, after: snapshot.seq) {
            if case .session(let session) = $0.event { session.muted } else { false }
        }
        #expect(mutedEvent?.sessionId == created.id)
        try await client.keepalive()

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
            Issue.record("helper binary missing at \(binary.path): run `make build` (or `make togokit-test`, which does), or set TOGO_BIN to a built togo")
            throw CancellationError()
        }
        return binary
    }

    /// The first event matching `predicate`, or nil if none arrives within five seconds.
    private func firstEvent(
        _ stream: AsyncStream<EventEnvelope>, after seq: Int,
        where predicate: @escaping @Sendable (EventEnvelope) -> Bool
    ) async throws -> EventEnvelope? {
        try await withThrowingTaskGroup(of: EventEnvelope?.self) { group in
            group.addTask {
                for await event in stream {
                    #expect(event.seq > seq)
                    if predicate(event) { return event }
                }
                return nil
            }
            group.addTask {
                try await Task.sleep(for: .seconds(5))
                return nil
            }
            let first = try await group.next() ?? nil
            group.cancelAll()
            return first
        }
    }

    /// `serve --stdio` starts a detached daemon for the TOGO_HOME, which outlives the
    /// test unless stopped. The daemon is the process holding its lock file open.
    private func stopDaemon(home: URL) {
        let lsof = Process()
        let output = Pipe()
        lsof.executableURL = URL(filePath: "/usr/sbin/lsof")
        lsof.arguments = ["-t", home.appending(path: "run/daemon.lock").path]
        lsof.standardOutput = output
        lsof.standardError = FileHandle.nullDevice
        guard (try? lsof.run()) != nil else { return }
        let pids = String(decoding: output.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
            .split(whereSeparator: \.isNewline).compactMap { pid_t($0) }
        lsof.waitUntilExit()
        for pid in pids { kill(pid, SIGTERM) }
        let deadline = ContinuousClock.now + .seconds(3)
        while pids.contains(where: { kill($0, 0) == 0 }), ContinuousClock.now < deadline {
            usleep(50_000)
        }
        for pid in pids where kill(pid, 0) == 0 { kill(pid, SIGKILL) }
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
