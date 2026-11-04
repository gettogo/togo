import Foundation
import Testing
@testable import TogoKit

struct MoshBootstrapTests {
    static let key = "4NeCCgvZFe2RnPgrcU1PQw"

    @Test func parseConnectAmidNoise() throws {
        let stdout = Data("Welcome to the box\nwarning: setlocale: LC_ALL: cannot change locale\n\nMOSH CONNECT 60001 \(Self.key)\n".utf8)
        let endpoint = try MoshBootstrap.parse(
            stdout: stdout, stderr: Data("mosh-server (mosh 1.4.0)\n".utf8), status: 0, host: "10.0.0.2"
        )
        #expect(endpoint == MoshEndpoint(host: "10.0.0.2", port: 60001, key: Self.key))
    }

    @Test func serverMissingExit127() {
        #expect(throws: MoshBootstrapError.serverMissing("togo: mosh-server not found")) {
            try MoshBootstrap.parse(
                stdout: Data(), stderr: Data("togo: mosh-server not found\n".utf8), status: 127, host: "h"
            )
        }
    }

    @Test func malformedKeyRejected() {
        let stdout = Data("MOSH CONNECT 60001 \(Self.key.dropLast())\n".utf8)
        #expect(throws: MoshBootstrapError.noConnectLine("")) {
            try MoshBootstrap.parse(stdout: stdout, stderr: Data(), status: 0, host: "h")
        }
    }

    @Test func bootstrapCommandQuotesArgv() throws {
        let argv = ["tmux", "-u", "-L", "togo", "attach-session", "-t", "=s_x'y"]
        let command = MoshBootstrap.command(argv: argv, serverPath: nil, ports: nil)
        #expect(command.hasPrefix("/bin/sh -c '"))
        let args = try runWithStub(command, stubPath: nil)
        let separator = try #require(args.firstIndex(of: "--"))
        #expect(Array(args[(separator + 1)...]) == argv)
        for flag in ["-s", "-c", "256", "-l", "LANG=en_US.UTF-8"] {
            #expect(args[..<separator].contains(flag))
        }
    }

    @Test func bootstrapCommandSurvivesHostileArgvUnderAnyLoginShell() throws {
        let argv = ["printf", "a'b \"c\" $(id) ;\n\\n\\\\ é", "\\", "tail\n"]
        let command = MoshBootstrap.command(argv: argv, serverPath: nil, ports: nil)
        #expect(!command.contains("\n"))
        for shell in ["/bin/sh", "/bin/zsh", "/bin/csh", "/bin/tcsh"] where FileManager.default.fileExists(atPath: shell) {
            let args = try runWithStub(command, stubPath: nil, loginShell: shell)
            let separator = try #require(args.firstIndex(of: "--"))
            #expect(Array(args[(separator + 1)...]) == argv, "login shell \(shell)")
        }
    }

    @Test func bootstrapCommandWithPortsAndPath() throws {
        let dir = try scratchDirectory()
        let stub = dir.appendingPathComponent("opt/x/mosh-server").path
        let command = MoshBootstrap.command(argv: ["true"], serverPath: stub, ports: PortRange(lower: 60000, upper: 60010))
        let args = try runWithStub(command, stubPath: stub)
        let separator = try #require(args.firstIndex(of: "--"))
        let flags = Array(args[..<separator])
        let portFlag = try #require(flags.firstIndex(of: "-p"))
        #expect(flags[portFlag + 1] == "60000:60010")
    }

    @Test func bootstrapCommandReportsMissingServer() throws {
        let command = MoshBootstrap.command(argv: ["true"], serverPath: "/nonexistent/mosh-server", ports: nil)
        let (_, stderr, status) = try run(["/bin/sh", "-c", command], path: "/usr/bin:/bin")
        #expect(status == 127)
        #expect(throws: MoshBootstrapError.serverMissing("togo: mosh-server not found")) {
            try MoshBootstrap.parse(stdout: Data(), stderr: stderr, status: status, host: "h")
        }
    }

    @Test func endpointRedactsKey() {
        let endpoint = MoshEndpoint(host: "h", port: 60001, key: Self.key)
        #expect(!String(describing: endpoint).contains(Self.key))
        #expect(!String(reflecting: endpoint).contains(Self.key))
    }

    // MARK: Helpers

    private func scratchDirectory() throws -> URL {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("mosh-bootstrap-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir
    }

    /// Runs `command` the way sshd does (`<login shell> -c <command>`) with a stub
    /// mosh-server that prints its arguments NUL-separated.
    private func runWithStub(_ command: String, stubPath: String?, loginShell: String = "/bin/sh") throws -> [String] {
        let dir = try scratchDirectory()
        let stub = stubPath.map { URL(fileURLWithPath: $0) } ?? dir.appendingPathComponent("bin/mosh-server")
        try FileManager.default.createDirectory(at: stub.deletingLastPathComponent(), withIntermediateDirectories: true)
        try "#!/bin/sh\nprintf '%s\\0' \"$@\"\n".write(to: stub, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: stub.path)
        let path = stub.deletingLastPathComponent().path + ":/usr/bin:/bin"
        let (stdout, stderr, status) = try run([loginShell, "-c", command], path: path)
        #expect(status == 0, "stderr: \(String(decoding: stderr, as: UTF8.self))")
        var parts = String(decoding: stdout, as: UTF8.self).split(separator: "\0", omittingEmptySubsequences: false).map(String.init)
        if parts.last == "" { parts.removeLast() }
        return parts
    }

    private func run(_ argv: [String], path: String) throws -> (Data, Data, Int32) {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: argv[0])
        process.arguments = Array(argv.dropFirst())
        process.environment = ["PATH": path, "HOME": NSTemporaryDirectory(), "LANG": "en_US.UTF-8"]
        let out = Pipe(), err = Pipe()
        process.standardOutput = out
        process.standardError = err
        try process.run()
        let stdout = out.fileHandleForReading.readDataToEndOfFile()
        let stderr = err.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        return (stdout, stderr, process.terminationStatus)
    }
}
