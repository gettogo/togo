import Foundation
import Testing
@testable import TogoKit

struct PlainSessionAttachTests {
    @Test func endedRealTmuxSessionReportsGoneBeforeAnotherAttach() async throws {
        let binary = try #require(PlainTmux.standardTmuxPaths.first { FileManager.default.isExecutableFile(atPath: $0) })
        let root = FileManager.default.temporaryDirectory.appending(path: "plain-attach-\(UUID())")
        let label = "togo-probe-\(UUID())"
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        let wrapper = root.appending(path: "tmux")
        try ("#!/bin/sh\nexec " + shellQuote([binary, "-L", label]) + " \"$@\"\n")
            .write(to: wrapper, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: wrapper.path)
        let path = root.path + ":/usr/bin:/bin"
        defer {
            _ = try? Self.status(shellQuote([wrapper.path, "kill-server"]), path: path)
            try? FileManager.default.removeItem(at: root)
        }
        let name = "probe space '$()'"
        #expect(try Self.status(shellQuote([wrapper.path, "new-session", "-d", "-s", name, "sleep 30"]), path: path) == 0)
        let id = PlainTmux.id(forName: name)
        let argv = try await SessionAttach.plainAttachArgv(sessionID: id) { try Self.status($0, path: path) }
        #expect(argv == PlainTmux.attachArgv(name: name))
        #expect(try Self.status(shellQuote([wrapper.path, "kill-session", "-t", "=" + name]), path: path) == 0)
        do {
            _ = try await SessionAttach.plainAttachArgv(sessionID: id) { try Self.status($0, path: path) }
            Issue.record("Ended session must not be attached again")
        } catch let error as HelperError {
            #expect(error.isSessionGone)
        }
    }

    @Test func unexpectedProbeFailureDoesNotClaimTheSessionEnded() async {
        await #expect(throws: SSHError.channelClosed(exitStatus: 255)) {
            _ = try await SessionAttach.plainAttachArgv(sessionID: PlainTmux.id(forName: "test")) { _ in 255 }
        }
    }

    private static func status(_ command: String, path: String) throws -> Int32 {
        let process = Process()
        process.executableURL = URL(filePath: "/bin/sh")
        process.arguments = ["-c", command]
        process.environment = ["PATH": path]
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice
        try process.run()
        process.waitUntilExit()
        return process.terminationStatus
    }
}
