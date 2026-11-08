import Foundation
import Synchronization
import Testing
import TogoKit

struct HelperClientTests {
    @Test func callsBeforeHelloThrowNotReady() async {
        let client = HelperClient(channel: PipeChannel())
        await #expect(throws: HelperError.notReady) { try await client.listSessions() }
        await #expect(throws: HelperError.notReady) { try await client.capture(id: "s_1", lines: 10, join: true) }
    }

    @Test func outOfOrderResponsesMatchById() async throws {
        let (channel, client) = try await readyClient()
        async let first = client.renameSession(id: "s_1", name: "first")
        async let second = client.renameSession(id: "s_2", name: "second")
        let requests = [await channel.nextRequest(), await channel.nextRequest()].compactMap(\.self)
        #expect(requests.count == 2)
        // Answer the later id first; each reply echoes the name its request asked for.
        for request in requests.sorted(by: { $0.id > $1.id }) {
            guard case .string(let name)? = request.params["name"], case .string(let id)? = request.params["id"] else {
                Issue.record("rename request without id and name")
                continue
            }
            await channel.reply(to: request.id, result: sessionJSON(id: id, name: name))
        }
        #expect(try await first.name == "first")
        #expect(try await second.name == "second")
    }

    @Test func timeoutThenLateResponseIgnored() async throws {
        let (channel, client) = try await readyClient(callTimeout: .milliseconds(50))
        await #expect(throws: HelperError.timeout(method: "session.list")) { try await client.listSessions() }
        let late = try #require(await channel.nextRequest())
        await channel.reply(to: late.id, result: #"{"sessions":[\#(sessionJSON(id: "s_late", name: "late"))]}"#)

        let sessions = Task { try await client.listSessions() }
        let next = try #require(await channel.nextRequest())
        #expect(next.id != late.id)
        await channel.reply(to: next.id, result: #"{"sessions":[]}"#)
        #expect(try await sessions.value.isEmpty)
    }

    @Test func rpcErrorSurfaces() async throws {
        let (channel, client) = try await readyClient()
        let rename = Task { try await client.renameSession(id: "main", name: "x") }
        let request = try #require(await channel.nextRequest())
        await channel.fail(request.id, code: RPCErrorCode.notManaged, message: "not_managed")
        await #expect(throws: HelperError.rpc(RPCErrorObject(code: -32007, message: "not_managed"))) {
            try await rename.value
        }
    }

    @Test func oversizedFrameClosesClient() async throws {
        let (channel, client) = try await readyClient()
        let sessions = Task { try await client.listSessions() }
        _ = try #require(await channel.nextRequest())
        await channel.sendRaw(Data([0x00, 0x10, 0x00, 0x01]))
        await #expect(throws: HelperError.closed) { try await sessions.value }
        await #expect(throws: HelperError.closed) { try await client.agents() }
        #expect(channel.isClosed)
    }

    @Test func channelEOFFailsPendingCalls() async throws {
        let (channel, client) = try await readyClient()
        let sessions = Task { try await client.listSessions() }
        _ = try #require(await channel.nextRequest())
        channel.incoming.finish()
        await #expect(throws: HelperError.closed) { try await sessions.value }
        await #expect(throws: HelperError.closed) { try await client.recentDirs() }
    }

    @Test func helloRejectsOtherProtocolVersion() async throws {
        let channel = PipeChannel()
        let client = HelperClient(channel: channel)
        async let hello = client.hello(client: HelperClient.testClient)
        let request = try #require(await channel.nextRequest())
        await channel.reply(to: request.id, result: #"""
            {"protocol":2,"helper":{"version":"x","os":"linux","arch":"amd64","hostname":"h"},"capabilities":[]}
            """#)
        do {
            _ = try await hello
            Issue.record("hello accepted protocol 2")
        } catch HelperError.rpc(let error) {
            #expect(error.code == RPCErrorCode.unsupportedVersion)
            #expect(error.helperIsAhead, "protocol 2 is newer than this app's 1")
        }
        await #expect(throws: HelperError.notReady) { try await client.listSessions() }
    }

    /// A refusal that carries the helper's own words shows them; without, the reason's sentence.
    @Test func refusalShowsTheHelpersReason() {
        let words = HelperError.rpc(RPCErrorObject(
            code: RPCErrorCode.refused, message: "refused",
            data: .object(["reason": .string("fallback"), "message": .string("That mode is not in this session's cycle; back in Plan.")])
        ))
        #expect(words.localizedDescription == "That mode is not in this session's cycle; back in Plan.")
        let admin = HelperError.rpc(RPCErrorObject(
            code: RPCErrorCode.refused, message: "refused", data: .object(["reason": .string("needs_admin_setup"), "message": .string(" ")])
        ))
        #expect(admin.localizedDescription == "This needs a one-time setup on the machine first.")
    }

    @Test func versionSkewSaysWhichSideIsBehind() {
        func skew(_ supported: [Double]) -> RPCErrorObject {
            RPCErrorObject(
                code: RPCErrorCode.unsupportedVersion, message: "unsupported_version",
                data: .object(["supported": .array(supported.map(JSONValue.number))])
            )
        }
        #expect(skew([2]).helperIsAhead)
        #expect(skew([2, 3]).helperIsAhead)
        #expect(!skew([0]).helperIsAhead)
        #expect(!skew([]).helperIsAhead)
        #expect(!RPCErrorObject(code: RPCErrorCode.unsupportedVersion, message: "unsupported_version").helperIsAhead)
    }

    @Test func captureRequiresCapability() async throws {
        let (_, client) = try await readyClient(capabilities: ["session.v1", "agents.v1", "fs.v1"])
        await #expect(throws: HelperError.unsupported(capability: "capture.v1")) {
            try await client.capture(id: "s_1", lines: 10, join: true)
        }
    }

    @Test func resultOfWrongShapeIsMalformed() async throws {
        let (channel, client) = try await readyClient()
        let dirs = Task { try await client.recentDirs() }
        let request = try #require(await channel.nextRequest())
        await channel.reply(to: request.id, result: #"{"dirs":"not a list"}"#)
        await #expect(throws: HelperError.malformedResponse) { try await dirs.value }
    }

    @Test func serveCommandRunsUnderSh() throws {
        #expect(HelperCommand.serveStdio.hasPrefix("/bin/sh -c '"))
        let home = FileManager.default.temporaryDirectory.appending(path: "togo-serve-\(UUID().uuidString)")
        defer { try? FileManager.default.removeItem(at: home) }
        let bin = home.appending(path: ".togo/bin")
        try FileManager.default.createDirectory(at: bin, withIntermediateDirectories: true)
        let stub = bin.appending(path: "togo")
        try Data("#!/bin/sh\necho \"$@\"\n".utf8).write(to: stub)
        try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: stub.path)

        let process = Process()
        process.executableURL = URL(filePath: "/bin/sh")
        process.arguments = ["-c", HelperCommand.serveStdio]
        process.environment = ["HOME": home.path, "PATH": "/usr/bin:/bin"]
        let output = Pipe()
        process.standardOutput = output
        try process.run()
        process.waitUntilExit()
        let printed = String(decoding: output.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
        #expect(printed == "serve --stdio\n")
        #expect(process.terminationStatus == 0)
    }

    private func readyClient(
        callTimeout: Duration = .seconds(5),
        capabilities: [String] = ["session.v1", "agents.v1", "fs.v1", "capture.v1"]
    ) async throws -> (PipeChannel, HelperClient) {
        let channel = PipeChannel()
        let client = HelperClient(channel: channel, callTimeout: callTimeout)
        async let accepted: Void = channel.acceptHello(capabilities: capabilities)
        let hello = try await client.hello(client: HelperClient.testClient)
        await accepted
        #expect(hello.capabilities == capabilities)
        return (channel, client)
    }

    private func sessionJSON(id: String, name: String) -> String {
        #"""
        {"id":"\#(id)","bib":"01","name":"\#(name)","agent":"shell","cwd":"/tmp",\#
        "createdAt":"2026-11-04T10:00:00Z","status":"idle","activity":null,"attachedClients":0,"managed":true}
        """#
    }
}

struct HelperClientWriteTests {
    @Test func writesNeverOverlap() async throws {
        let channel = AnsweringChannel()
        let client = HelperClient(channel: channel)
        _ = try await client.hello(client: HelperClient.testClient)
        try await withThrowingTaskGroup(of: [String].self) { group in
            for _ in 0..<50 {
                group.addTask { try await client.recentDirs() }
            }
            for try await dirs in group { #expect(dirs == ["/tmp"]) }
        }
        #expect(channel.peakConcurrentWrites == 1)
    }

    @Test func writeFailureClosesClient() async throws {
        let channel = AnsweringChannel()
        let client = HelperClient(channel: channel)
        _ = try await client.hello(client: HelperClient.testClient)
        channel.failWrites()
        await #expect(throws: HelperError.closed) { try await client.recentDirs() }
        await #expect(throws: HelperError.closed) { try await client.agents() }
    }

    @Test func callAfterCloseThrowsClosed() async throws {
        let channel = AnsweringChannel()
        let client = HelperClient(channel: channel)
        _ = try await client.hello(client: HelperClient.testClient)
        await client.close()
        await #expect(throws: HelperError.closed) { try await client.recentDirs() }
    }

    @Test func notificationIsIgnored() async throws {
        let channel = PipeChannel()
        let ready = HelperClient(channel: channel)
        async let accepted: Void = channel.acceptHello()
        _ = try await ready.hello(client: HelperClient.testClient)
        await accepted
        let dirs = Task { try await ready.recentDirs() }
        let request = try #require(await channel.nextRequest())
        await channel.sendJSON(#"{"jsonrpc":"2.0","method":"session.changed","params":{}}"#)
        await channel.reply(to: request.id, result: #"{"dirs":["/a"]}"#)
        #expect(try await dirs.value == ["/a"])
    }

    @Test func cancelledCallEndsBeforeTimeout() async throws {
        let channel = PipeChannel()
        let helper = HelperClient(channel: channel, callTimeout: .seconds(30))
        async let accepted: Void = channel.acceptHello()
        _ = try await helper.hello(client: HelperClient.testClient)
        await accepted
        let call = Task { try await helper.recentDirs() }
        _ = try #require(await channel.nextRequest())
        call.cancel()
        await #expect(throws: CancellationError.self) { try await call.value }
    }
}

/// Answers every request itself and records how many `write` calls overlap. Each write
/// suspends partway, as an SSH channel does when its window is full.
private final class AnsweringChannel: ExecChannel {
    let incoming = ByteStream()
    private let state = Mutex((inFlight: 0, peak: 0, failing: false))
    private let decoder = Mutex(FrameDecoder())

    var peakConcurrentWrites: Int { state.withLock { $0.peak } }

    func failWrites() {
        state.withLock { $0.failing = true }
    }

    func write(_ data: Data) async throws {
        let failing = state.withLock { state in
            state.inFlight += 1
            state.peak = max(state.peak, state.inFlight)
            return state.failing
        }
        defer { state.withLock { $0.inFlight -= 1 } }
        if failing { throw CancellationError() }
        try await Task.sleep(for: .milliseconds(1))
        for frame in try decoder.withLock({ try $0.append(data) }) {
            let request = try JSONDecoder().decode(AnsweredRequest.self, from: frame)
            let result = request.method == "hello"
                ? #"{"protocol":1,"helper":{"version":"t","os":"darwin","arch":"arm64","hostname":"h"},"capabilities":[]}"#
                : #"{"dirs":["/tmp"]}"#
            _ = await incoming.write(try Frame.encode(Data(#"{"jsonrpc":"2.0","id":\#(request.id),"result":\#(result)}"#.utf8)))
        }
    }

    func close() async {
        incoming.finish()
    }
}

private struct AnsweredRequest: Decodable {
    let id: Int
    let method: String
}

extension HelperClientTests {
    static let gitCapabilities = ["session.v1", "agents.v1", "fs.v1", "git.v1", "gitwrite.v1"]

    @Test func stageSendsPathsAndReturnsFreshStatus() async throws {
        let (channel, client) = try await readyClient(capabilities: Self.gitCapabilities)
        let stage = Task { try await client.gitStage(id: "s_abcdefghij", paths: ["a b.txt"]) }
        let request = try #require(await channel.nextRequest())
        #expect(request.method == "git.stage")
        #expect(request.params["paths"] == .array([.string("a b.txt")]))
        #expect(request.params["seen"] == nil)
        await channel.reply(to: request.id, result: #"""
            {"repo":true,"root":"/p","branch":"main","ahead":0,"behind":0,\#
            "files":[{"path":"a b.txt","origPath":null,"index":"M","worktree":"."}]}
            """#)
        #expect(try await stage.value.files.first?.index == "M")
    }

    @Test func discardFromFilesSendsWhatWasSeen() async throws {
        let (channel, client) = try await readyClient(capabilities: Self.gitCapabilities)
        let file = GitStatus.File(path: "App.swift", origPath: nil, index: ".", worktree: "M", fingerprint: "blob:ab12")
        let discard = Task { try await client.gitDiscard(id: "s_abcdefghij", files: [file]) }
        let request = try #require(await channel.nextRequest())
        #expect(request.method == "git.discard")
        #expect(request.params["seen"] == .object([
            "App.swift": .object(["status": .string(".M"), "fingerprint": .string("blob:ab12")]),
        ]))
        await channel.reply(to: request.id, result: #"{"repo":true,"root":"/p","branch":"main","ahead":0,"behind":0,"files":[]}"#)
        #expect(try await discard.value.files.isEmpty)
    }

    @Test func conflictReasonMapsFromData() async throws {
        let (channel, client) = try await readyClient(capabilities: Self.gitCapabilities)
        let stage = Task { try await client.gitStage(id: "s_abcdefghij", paths: ["a.txt"]) }
        let request = try #require(await channel.nextRequest())
        await channel.sendJSON(#"""
            {"jsonrpc":"2.0","id":\#(request.id),"error":{"code":-32006,"message":"conflict","data":{"reason":"stale"}}}
            """#)
        do {
            _ = try await stage.value
            Issue.record("stale stage succeeded")
        } catch let error as HelperError {
            #expect(error == .conflict("stale"))
            #expect(error.gitConflict == .stale)
        }
    }

    /// The agent edits the file again after the phone read status: still M, new content.
    @Test func repeatEditDiscardIsStale() async throws {
        let (channel, client) = try await readyClient(capabilities: Self.gitCapabilities)
        let seen = GitStatus.File(path: "App.swift", origPath: nil, index: ".", worktree: "M", fingerprint: "blob:1111")
        let discard = Task { try await client.gitDiscard(id: "s_abcdefghij", files: [seen]) }
        let request = try #require(await channel.nextRequest())
        await channel.sendJSON(#"""
            {"jsonrpc":"2.0","id":\#(request.id),"error":{"code":-32006,"message":"conflict","data":{"reason":"stale"}}}
            """#)
        await #expect(throws: HelperError.conflict("stale")) { try await discard.value }
    }

    @Test func commitReturnsNewHead() async throws {
        let (channel, client) = try await readyClient(capabilities: Self.gitCapabilities)
        let commit = Task { try await client.gitCommit(GitCommitParams(id: "s_abcdefghij", message: "Fix", amend: false)) }
        let request = try #require(await channel.nextRequest())
        #expect(request.method == "git.commit")
        #expect(request.params["message"] == .string("Fix"))
        let hash = String(repeating: "c", count: 40)
        await channel.reply(to: request.id, result: #"{"hash":"\#(hash)","short":"ccccccc"}"#)
        #expect(try await commit.value == GitCommitResult(hash: hash, short: "ccccccc"))
    }

    @Test func gitWritesNeedTheirCapability() async throws {
        let (_, client) = try await readyClient(capabilities: ["session.v1", "agents.v1", "fs.v1", "git.v1"])
        let missing = HelperError.unsupported(capability: "gitwrite.v1")
        await #expect(throws: missing) { try await client.gitStage(id: "s_abcdefghij", paths: ["a"]) }
        await #expect(throws: missing) { try await client.gitUnstage(id: "s_abcdefghij", paths: ["a"]) }
        await #expect(throws: missing) { try await client.gitDiscard(id: "s_abcdefghij", paths: ["a"]) }
        await #expect(throws: missing) {
            try await client.gitCommit(GitCommitParams(id: "s_abcdefghij", message: "m", amend: false))
        }
        // An older helper would ignore commit and diff the working tree instead.
        await #expect(throws: missing) { try await client.gitDiff(GitDiffParams(id: "s_abcdefghij", commit: "HEAD")) }
    }

    @Test func listDirAtRefSendsIdAndRef() async throws {
        let (channel, client) = try await readyClient(capabilities: Self.gitCapabilities)
        let list = Task { try await client.listDir(path: "Sources", showHidden: false, sessionID: "s_abcdefghij", ref: "HEAD") }
        let request = try #require(await channel.nextRequest())
        #expect(request.method == "fs.listDir")
        #expect(request.params == [
            "path": .string("Sources"), "showHidden": .bool(false), "id": .string("s_abcdefghij"), "ref": .string("HEAD"),
        ])
        await channel.reply(to: request.id, result: #"{"path":"Sources","entries":[{"name":"App","dir":true}]}"#)
        #expect(try await list.value.entries == [DirEntry(name: "App", dir: true)])
    }

    @Test func repoFolderSendsRootAndNeedsWrites() async throws {
        let (channel, client) = try await readyClient(capabilities: Self.gitCapabilities)
        let list = Task {
            try await client.listRepoDir(path: "apps/tools", showHidden: true, includeFiles: true, sessionID: "s_abcdefghij", ref: nil)
        }
        let request = try #require(await channel.nextRequest())
        #expect(request.method == "fs.listDir")
        #expect(request.params == [
            "path": .string("apps/tools"), "showHidden": .bool(true), "includeFiles": .bool(true),
            "id": .string("s_abcdefghij"), "root": .bool(true),
        ])
        await channel.reply(to: request.id, result: #"{"path":"/r/apps/tools","entries":[]}"#)
        #expect(try await list.value.entries.isEmpty)

        let (_, readOnly) = try await readyClient(capabilities: ["session.v1", "agents.v1", "fs.v1", "git.v1"])
        let missing = HelperError.unsupported(capability: "gitwrite.v1")
        await #expect(throws: missing) {
            try await readOnly.listRepoDir(path: "a", showHidden: false, sessionID: "s_abcdefghij", ref: "HEAD")
        }
        await #expect(throws: missing) {
            try await readOnly.readFile(FsReadParams(id: "s_abcdefghij", path: "a", root: true))
        }
    }
}

extension HelperClientTests {
    @Test func badWritePathsNeverSent() async throws {
        let (channel, client) = try await readyClient(capabilities: Self.gitCapabilities)
        for path in ["a\u{0}b", String(repeating: "a", count: 4097), String(repeating: "é", count: 2048) + "a", "newpkg/", ""] {
            do {
                _ = try await client.gitDiscard(id: "s_abcdefghij", paths: [path])
                Issue.record("\(path.utf8.count)-byte path was accepted")
            } catch HelperError.rpc(let error) {
                #expect(error.code == RPCErrorCode.invalidParams)
            }
        }
        try await expectNextRequestIsSessionList(channel, client)
    }

    @Test func longestWritePathIsSent() async throws {
        let (channel, client) = try await readyClient(capabilities: Self.gitCapabilities)
        let path = String(repeating: "a", count: 4096)
        let stage = Task { try await client.gitStage(id: "s_abcdefghij", paths: [path]) }
        let request = try #require(await channel.nextRequest())
        #expect(request.params["paths"] == .array([.string(path)]))
        await channel.reply(to: request.id, result: #"{"repo":true,"root":"/p","branch":"main","ahead":0,"behind":0,"files":[]}"#)
        _ = try await stage.value
    }

    @Test func amendWithoutHeadOrWithBadHashNeverSent() async throws {
        let (channel, client) = try await readyClient(capabilities: Self.gitCapabilities)
        for params in [
            GitCommitParams(id: "s_abcdefghij", message: "Fix", amend: true),
            GitCommitParams(id: "s_abcdefghij", message: "Fix", amend: true, head: String(repeating: "a", count: 41)),
            GitCommitParams(id: "s_abcdefghij", message: "Fix", amend: false, head: String(repeating: "A", count: 40)),
        ] {
            do {
                _ = try await client.gitCommit(params)
                Issue.record("commit \(params) was accepted")
            } catch HelperError.rpc(let error) {
                #expect(error.code == RPCErrorCode.invalidParams)
            }
        }
        try await expectNextRequestIsSessionList(channel, client)
    }

    @Test func amendWithFullHashIsSent() async throws {
        let (channel, client) = try await readyClient(capabilities: Self.gitCapabilities)
        let head = String(repeating: "0", count: 64)
        let commit = Task { try await client.gitCommit(GitCommitParams(id: "s_abcdefghij", message: "Fix", amend: true, head: head)) }
        let request = try #require(await channel.nextRequest())
        #expect(request.params["head"] == .string(head))
        await channel.reply(to: request.id, result: #"{"hash":"\#(head)","short":"0000000"}"#)
        _ = try await commit.value
    }

    private func expectNextRequestIsSessionList(_ channel: PipeChannel, _ client: HelperClient) async throws {
        let list = Task { try await client.listSessions() }
        let next = try #require(await channel.nextRequest())
        #expect(next.method == "session.list")
        await channel.reply(to: next.id, result: #"{"sessions":[]}"#)
        _ = try await list.value
    }
}

extension HelperClientTests {
    @Test func stageWithSeenSendsIt() async throws {
        let (channel, client) = try await readyClient(capabilities: Self.gitCapabilities)
        let seen = ["a.txt": GitSeen(status: ".M", fingerprint: "blob:ab12")]
        let stage = Task { try await client.gitStage(id: "s_abcdefghij", paths: ["a.txt"], seen: seen) }
        let request = try #require(await channel.nextRequest())
        #expect(request.params["seen"] == .object([
            "a.txt": .object(["status": .string(".M"), "fingerprint": .string("blob:ab12")]),
        ]))
        await channel.reply(to: request.id, result: #"{"repo":true,"root":"/p","branch":"main","ahead":0,"behind":0,"files":[]}"#)
        _ = try await stage.value
    }

    @Test func partialWriteCarriesChangedPaths() async throws {
        let (channel, client) = try await readyClient(capabilities: Self.gitCapabilities)
        let discard = Task { try await client.gitDiscard(id: "s_abcdefghij", paths: ["a.txt", "b.txt"]) }
        let request = try #require(await channel.nextRequest())
        await channel.sendJSON(#"""
            {"jsonrpc":"2.0","id":\#(request.id),"error":{"code":-32006,"message":"conflict",\#
            "data":{"reason":"partial","changed":["a.txt"]}}}
            """#)
        do {
            _ = try await discard.value
            Issue.record("partial discard succeeded")
        } catch let error as HelperError {
            #expect(error == .partialWrite(changed: ["a.txt"]))
            #expect(error.gitConflict == .partial)
            #expect(error.localizedDescription.contains("a.txt"))
        }
    }

    @Test func racedCommitCarriesHash() async throws {
        let (channel, client) = try await readyClient(capabilities: Self.gitCapabilities)
        let commit = Task { try await client.gitCommit(GitCommitParams(id: "s_abcdefghij", message: "Fix", amend: false)) }
        let request = try #require(await channel.nextRequest())
        let hash = "1234567" + String(repeating: "f", count: 33)
        await channel.sendJSON(#"""
            {"jsonrpc":"2.0","id":\#(request.id),"error":{"code":-32006,"message":"conflict",\#
            "data":{"reason":"raced","hash":"\#(hash)"}}}
            """#)
        do {
            _ = try await commit.value
            Issue.record("raced commit succeeded")
        } catch let error as HelperError {
            #expect(error == .racedCommit(hash: hash))
            #expect(error.gitConflict == .raced)
            #expect(error.localizedDescription.contains("1234567"))
            #expect(!error.localizedDescription.contains("12345678"))
        }
    }
}

extension HelperClientTests {
    @Test func listDirCanAskForFiles() async throws {
        let (channel, client) = try await readyClient(capabilities: Self.gitCapabilities)
        let disk = Task { try await client.listDir(path: "/p", showHidden: true, includeFiles: true) }
        let first = try #require(await channel.nextRequest())
        #expect(first.params == ["path": .string("/p"), "showHidden": .bool(true), "includeFiles": .bool(true)])
        await channel.reply(to: first.id, result: #"{"path":"/p","entries":[]}"#)
        _ = try await disk.value
        let tree = Task {
            try await client.listDir(path: "src", showHidden: false, includeFiles: true, sessionID: "s_abcdefghij", ref: "HEAD")
        }
        let second = try #require(await channel.nextRequest())
        #expect(second.params["includeFiles"] == .bool(true))
        #expect(second.params["ref"] == .string("HEAD"))
        await channel.reply(to: second.id, result: #"{"path":"/p/src","entries":[]}"#)
        _ = try await tree.value
    }
}
