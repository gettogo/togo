import Foundation
import Synchronization
import Testing
import TogoKit

/// A pipe whose writes wait while `slow` is set, as a large upload does on weak cellular.
private final class SlowPipe: ExecChannel {
    let pipe = PipeChannel()
    let slow = Mutex(false)
    var incoming: ByteStream { pipe.incoming }

    func write(_ data: Data) async throws {
        while slow.withLock({ $0 }) { try? await Task.sleep(for: .milliseconds(5)) }
        try await pipe.write(data)
    }

    func close() async { await pipe.close() }
}

struct HelperClientQueueTests {
    /// A call waiting behind a slow frame is not timed out for the wait: its timeout counts
    /// from when its own frame is written.
    @Test func timeoutCountsFromTheWrite() async throws {
        let channel = SlowPipe()
        let client = HelperClient(channel: channel, callTimeout: .milliseconds(150))
        async let accepted: Void = channel.pipe.acceptHello()
        _ = try await client.hello(client: HelperClient.testClient)
        await accepted

        channel.slow.withLock { $0 = true }
        let list = Task { try await client.listSessions() }
        // Longer than the call timeout, shorter than the queued allowance.
        try await Task.sleep(for: .milliseconds(250))
        channel.slow.withLock { $0 = false }
        let request = try #require(await channel.pipe.nextRequest())
        await channel.pipe.reply(to: request.id, result: #"{"sessions":[]}"#)
        #expect(try await list.value.isEmpty)
    }

    @Test func aFrameThatNeverGoesOutStillTimesOut() async throws {
        let channel = SlowPipe()
        let client = HelperClient(channel: channel, callTimeout: .milliseconds(30))
        async let accepted: Void = channel.pipe.acceptHello()
        _ = try await client.hello(client: HelperClient.testClient)
        await accepted

        channel.slow.withLock { $0 = true }
        await #expect(throws: HelperError.timeout(method: "session.list")) { try await client.listSessions() }
        channel.slow.withLock { $0 = false }
    }
}
