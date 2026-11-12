import Foundation
import Synchronization
import Testing
@testable import TogoKit

/// Stands in for the remote shell: records each command and answers from a script.
private final class FakeHost: Sendable {
    private let log = Mutex<[String]>([])
    private let answer: @Sendable (String) -> (String, Int32)

    init(_ answer: @escaping @Sendable (String) -> (String, Int32)) {
        self.answer = answer
    }

    var commands: [String] { log.withLock { $0 } }

    func source(helperFound: (@Sendable (String) -> Void)? = nil) -> PlainTmuxSource {
        PlainTmuxSource(host: "work-mac", now: { Date(timeIntervalSince1970: 100) }, helperFound: helperFound) {
            PlainTmuxDial(
                run: { command in
                    self.log.withLock { $0.append(command) }
                    let (out, status) = self.answer(command)
                    return (Data(out.utf8), status)
                },
                hangUp: {}
            )
        }
    }
}

private let hostileNames = [
    "plain", "with space", "it's", #"say "hi""#, "$(touch pwned)", "`touch pwned`", "a;b|c&d",
    "line\nbreak", "tab\there", "-s", "naïve 🙂",
]

struct PlainTmuxSourceTests {
    private static func listing(_ names: [String]) -> String {
        names.enumerated().map { "$\($0.offset)\t" + hex($0.element + "\n") + "\t1\t0\t" + hex("/tmp\n") + "\n" }.joined()
    }

    private static func hex(_ text: String) -> String { text.utf8.map { String(format: "%02x", $0) }.joined() }

    @Test func parsesListOutput() {
        let out = [
            "$0\t" + Self.hex("dev server\n") + "\t1700000000\t1\t" + Self.hex("/Users/me/app\n"),
            "$1\t" + Self.hex("it's \"quoted\"\n") + "\t1700000100\t0\t" + Self.hex("/tmp\n"),
            "$2\t" + Self.hex("has\ttab\n") + "\t1700000200\t2\t" + Self.hex("/srv\n"),
            "broken line",
            "$x\t00\t1\t0\t00",
        ].joined(separator: "\n") + "\n"
        let sessions = PlainTmux.parse(out, now: Date(timeIntervalSince1970: 5))

        #expect(sessions.map(\.name) == ["dev server", "it's \"quoted\"", "has\ttab"])
        #expect(sessions.map(\.cwd) == ["/Users/me/app", "/tmp", "/srv"])
        #expect(sessions.map(\.attachedClients) == [1, 0, 2])
        #expect(sessions[0].createdAt == Date(timeIntervalSince1970: 1_700_000_000))
        #expect(sessions.allSatisfy { !$0.managed && $0.agent == "shell" && $0.status == .idle && $0.bib == nil })
    }

    @Test func parseRejectsMalformedHexAndExtraFields() {
        let good = Self.hex("real\n")
        let rows = [
            "$0\t\(good)\t1\t0\t\(Self.hex("/a\n"))",
            "$1\t\(good.uppercased())\t1\t0\t00",
            "$2\t\(good)0\t1\t0\t00",
            "$3\tzz\t1\t0\t00",
            "$4\t\(good)\t1\t0\tff",
            "$5\t\(good)\t1\t0\t00\textra",
            "$6\t\(good)\t1\t0",
            "$7\t0a\t1\t0\t00",
            "$8\t\(good)\t1\t0\t+f",
        ]
        let sessions = PlainTmux.parse(rows.joined(separator: "\n"), now: Date())
        #expect(sessions.count == 1)
        #expect(sessions.first?.cwd == "/a")
        #expect(PlainTmux.name(forID: "u_" + Self.hex("j").uppercased()) == nil)
        #expect(PlainTmux.name(forID: "u_ff") == nil)
    }

    private static let tmuxPath = "/opt/homebrew/bin/tmux"

    /// tmux lists control characters and backslashes in a name escaped (`\n`, `\\`), so the
    /// listed name differs from the created one for those; the row must still be exactly one.
    private static let sessionTable: [(name: String, listed: String, leaf: String)] = [
        ("plain", "plain", "a"), ("a\n$9\ttogo-x", "a\\n$9\\ttogo-x", "b"), ("b\\x", "b\\\\x", "c"),
        ("c\\n", "c\\\\n", "d"), ("t\tab", "t\\tab", "e"), ("it's \"q\" $(id)", "it's \"q\" $(id)", "f"),
    ]

    @Test(arguments: ["/bin/sh", "/bin/dash"])
    func hostileNamesAndPathsGiveOneRowEach(shell: String) throws {
        guard FileManager.default.isExecutableFile(atPath: Self.tmuxPath),
              FileManager.default.isExecutableFile(atPath: shell) else { return }
        // A private TMUX_TMPDIR keeps the server off the user's sockets; /tmp keeps the path short enough for a unix socket.
        let socketDirectory = "/tmp/pt-\(UUID().uuidString.prefix(8))"
        try FileManager.default.createDirectory(atPath: socketDirectory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(atPath: socketDirectory) }
        let environment = ["PATH": "/usr/bin:/bin:/opt/homebrew/bin", "TMUX_TMPDIR": socketDirectory]
        func run(_ executable: String, _ args: [String]) throws -> (Data, Int32) {
            let process = Process(), pipe = Pipe()
            process.executableURL = URL(fileURLWithPath: executable)
            process.arguments = args
            process.environment = environment
            process.standardOutput = pipe
            try process.run()
            let data = pipe.fileHandleForReading.readDataToEndOfFile()
            process.waitUntilExit()
            return (data, process.terminationStatus)
        }
        let base = try Self.emptyDirectory()
        defer { try? FileManager.default.removeItem(at: base) }
        defer { _ = try? run(Self.tmuxPath, ["kill-server"]) }
        for (name, _, leaf) in Self.sessionTable {
            // The directory name itself carries a forged row.
            let directory = base.appendingPathComponent(leaf + "\n$9\tforged\t1\t0\t/p\ttab")
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
            #expect(try run(Self.tmuxPath, ["new-session", "-d", "-s", name, "-c", directory.path]).1 == 0)
        }

        let (data, status) = try run(shell, ["-c", PlainTmux.listScript])
        let sessions = PlainTmux.parse(String(decoding: data, as: UTF8.self), now: Date())
        #expect(status == 0)
        #expect(sessions.map(\.name).sorted() == Self.sessionTable.map(\.listed).sorted())
        for (name, listed, leaf) in Self.sessionTable {
            let row = try #require(sessions.first { $0.name == listed }, "\(name.debugDescription)")
            #expect(row.cwd.hasSuffix("/" + leaf + "\n$9\tforged\t1\t0\t/p\ttab"), "\(name.debugDescription)")
            #expect(row.id == PlainTmux.id(forName: listed))
            // Only names that need no escaping can be addressed by their listed form.
            if listed == name {
                #expect(try run(Self.tmuxPath, ["has-session", "-t", "=" + listed]).1 == 0)
            }
        }
    }

    @Test func parseDropsForgedAndContinuationLines() {
        let forged = "$9\t" + Self.hex("forged\n") + "\t1\t0\t00"
        let out = "$0\t" + Self.hex("real\n") + "\t1\t0\t" + Self.hex("/a\n" + forged + "\n") + "\n/tmp/continuation\n\t\t\n"
        #expect(PlainTmux.parse(out, now: Date()).map(\.name) == ["real"])
    }

    @Test func idIsHexOfName() {
        #expect(PlainTmux.id(forName: "dev") == "u_646576")
        for name in hostileNames {
            #expect(PlainTmux.name(forID: PlainTmux.id(forName: name)) == name)
        }
        #expect(PlainTmux.name(forID: "u_6") == nil)
        #expect(PlainTmux.name(forID: "s_646576") == nil)
    }

    @Test func newArgvQuotesName() throws {
        let directory = try Self.fakeTmuxDirectory()
        for name in hostileNames {
            let seen = try Self.run(shellQuote(PlainTmux.newArgv(name: name)), path: directory)
            #expect(seen == ["new-session", "-d", "-s", name])
        }
        let withCwd = try Self.run(shellQuote(PlainTmux.newArgv(name: "x", cwd: "/a b/$(c)")), path: directory)
        #expect(withCwd == ["new-session", "-d", "-s", "x", "-c", "/a b/$(c)"])
        #expect(!FileManager.default.fileExists(atPath: "pwned"))
    }

    @Test func attachAndKillPassNameVerbatim() throws {
        let directory = try Self.fakeTmuxDirectory()
        for name in hostileNames {
            #expect(try Self.run(shellQuote(PlainTmux.attachArgv(name: name)), path: directory) == ["attach-session", "-t", "=" + name])
            #expect(try Self.run(shellQuote(PlainTmux.killArgv(name: name)), path: directory) == ["kill-session", "-t", "=" + name])
        }
    }

    @Test func refusesKillOfForeignSession() async throws {
        let host = FakeHost { command in command.contains("list-sessions") ? ("mine\t1\t/tmp\t0\n", 0) : ("/usr/bin/tmux", 0) }
        let source = host.source()
        _ = try await source.open()

        await #expect(throws: PlainTmuxError.foreignSession) { try await source.call(.kill(PlainTmux.id(forName: "mine"))) }
        await #expect(throws: PlainTmuxError.foreignSession) {
            try await source.call(.rename(PlainTmux.id(forName: "mine"), "togo-9"))
        }
        #expect(PlainTmuxError.foreignSession.localizedDescription == "This session was started outside Togo.")
        #expect(!host.commands.contains { $0.contains("kill-session") || $0.contains("rename-session") })

        _ = try await source.call(.kill(PlainTmux.id(forName: "togo-1")))
        #expect(host.commands.last?.contains("kill-session") == true)
    }

    /// What `HelperProbe.script` prints on a machine that has the helper.
    private static let helperProbeOutput = "togo.path=/opt/homebrew/bin/togo\ntogo.version=togo 1.0.1\ntmux=tmux 3.5a\n"

    @Test func reportsAHelperInstalledSinceTheMachineWasSaved() async throws {
        let found = Mutex<[String]>([])
        let host = FakeHost { command in
            if command.contains("togo.path") { return (Self.helperProbeOutput, 0) }
            return ("/usr/bin/tmux", 0)
        }
        let source = host.source(helperFound: { path in found.withLock { $0.append(path) } })
        _ = try await source.open()

        #expect(found.withLock { $0 } == ["/opt/homebrew/bin/togo"])
        #expect(host.commands.filter { $0.contains("togo.path") }.count == 1, "probed once per connection")
    }

    @Test func reportsAnOldHelperToo() async throws {
        let found = Mutex<[String]>([])
        let host = FakeHost { command in
            command.contains("togo.path") ? ("togo.path=/Users/sam/.togo/bin/togo\ntogo.version=\n", 0) : ("/usr/bin/tmux", 0)
        }
        let source = host.source(helperFound: { path in found.withLock { $0.append(path) } })
        _ = try await source.open()

        #expect(found.withLock { $0 } == ["/Users/sam/.togo/bin/togo"])
    }

    @Test func staysQuietWhileTheHelperIsStillMissing() async throws {
        let found = Mutex<[String]>([])
        let host = FakeHost { command in
            command.contains("togo.path") ? ("tmux=tmux 3.5a\nos=Linux\n", 0) : ("/usr/bin/tmux", 0)
        }
        let source = host.source(helperFound: { path in found.withLock { $0.append(path) } })
        let (presence, _) = try await source.open()

        #expect(found.withLock { $0 }.isEmpty)
        #expect(presence == .unknown)
        #expect(host.commands.filter { $0.contains("togo.path") }.count == 1)
    }

    @Test func aFailedHelperProbeChangesNothing() async throws {
        let found = Mutex<[String]>([])
        let host = FakeHost { command in command.contains("togo.path") ? (Self.helperProbeOutput, 1) : ("/usr/bin/tmux", 0) }
        let source = host.source(helperFound: { path in found.withLock { $0.append(path) } })
        _ = try await source.open()

        #expect(found.withLock { $0 }.isEmpty)
    }

    @Test func doesNotProbeWithoutAListener() async throws {
        let host = FakeHost { _ in ("/usr/bin/tmux", 0) }
        _ = try await host.source().open()

        #expect(!host.commands.contains { $0.contains("togo.path") })
    }

    @Test func noTmuxReportsMissing() async throws {
        let host = FakeHost { command in command.contains("list-sessions") ? ("", 127) : ("", 0) }
        let source = host.source()
        let (presence, capabilities) = try await source.open()

        #expect(presence == .missing)
        #expect(capabilities.isEmpty)
        await #expect(throws: PlainTmuxError.noTmux(host: "work-mac")) { try await source.snapshot() }
        #expect(PlainTmuxError.noTmux(host: "work-mac").localizedDescription
            == "tmux is not installed on work-mac; Togo can still open a plain shell.")

        let directory = try Self.emptyDirectory()
        let marker = directory.appendingPathComponent("shell-ran")
        let stub = directory.appendingPathComponent("shell")
        try "#!/bin/sh\necho \"$@\" > '\(marker.path)'\n".write(to: stub, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: stub.path)
        _ = try Self.run(
            shellQuote(PlainTmux.attachArgv(name: "x", tmuxCandidates: [])), path: directory, expectTmux: false
        )
        #expect(try String(contentsOf: marker, encoding: .utf8) == "-l\n")
    }

    @Test func createPicksLowestFreeName() async throws {
        let listing = Mutex(Self.listing(["togo-1", "togo-3", "other"]))
        let host = FakeHost { command in
            if command.contains("list-sessions") { return (listing.withLock { $0 }, 0) }
            if command.contains("new-session") { listing.withLock { $0 = Self.listing(["togo-1", "togo-2", "togo-3", "other"]) } }
            return ("/usr/bin/tmux", 0)
        }
        let source = host.source()
        _ = try await source.open()

        let created = try await source.call(.create(CreateSessionParams()))
        #expect(created?.name == "togo-2")
        #expect(created?.id == PlainTmux.id(forName: "togo-2"))
        await #expect(throws: PlainTmuxError.nameTaken("other")) {
            try await source.call(.create(CreateSessionParams(name: "other")))
        }
    }

    @Test func listScriptIsOneLineUnderBinSh() {
        #expect(!PlainTmux.listScript.contains("\n"))
        #expect(PlainTmux.listCommand.hasPrefix("/bin/sh -c '"))
        #expect(PlainTmux.listScript.contains("command -v tmux"))
        #expect(PlainTmux.listScript.contains("/opt/homebrew/bin/tmux"))
        #expect(PlainTmux.listScript.hasSuffix("done; true"))
    }

    // MARK: - Running real shell commands against a fake tmux

    /// A directory whose `tmux` writes its arguments NUL-separated to `$FAKE_TMUX_OUT`.
    private static func fakeTmuxDirectory() throws -> URL {
        let directory = try emptyDirectory()
        let script = directory.appendingPathComponent("tmux")
        try "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\0' \"$a\" >> \"$FAKE_TMUX_OUT\"; done\n".write(to: script, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: script.path)
        return directory
    }

    private static func emptyDirectory() throws -> URL {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("plain-tmux-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        return directory
    }

    /// Runs `command` through `/bin/sh -c`, as the remote login shell would hand it over.
    private static func run(_ command: String, path directory: URL, expectTmux: Bool = true) throws -> [String] {
        let output = directory.appendingPathComponent("out-\(UUID().uuidString)")
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/sh")
        process.arguments = ["-c", command]
        process.environment = [
            "PATH": (expectTmux ? directory.path + ":" : "") + "/usr/bin:/bin", "FAKE_TMUX_OUT": output.path, "SHELL": loginShellStub(in: directory),
        ]
        try process.run()
        process.waitUntilExit()
        if !expectTmux { return [] }
        let data = try Data(contentsOf: output)
        return String(decoding: data, as: UTF8.self).split(separator: "\0", omittingEmptySubsequences: false).dropLast().map(String.init)
    }

    private static func loginShellStub(in directory: URL) -> String {
        directory.appendingPathComponent("shell").path
    }
}
