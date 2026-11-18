import Foundation
import Synchronization
import Testing
import TogoKit

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

    /// A client says when its channel has ended, so a caller holding one that closed while
    /// idle can open another before it sends anything.
    @Test func aClosedClientSaysItHasEnded() async {
        let client = HelperClient(channel: PipeChannel())
        #expect(await !client.hasEnded)
        await client.close()
        #expect(await client.hasEnded)
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
