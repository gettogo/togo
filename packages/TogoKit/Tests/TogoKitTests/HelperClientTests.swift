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
        }
        await #expect(throws: HelperError.notReady) { try await client.listSessions() }
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
