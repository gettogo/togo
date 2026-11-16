import Foundation
import Synchronization
import Testing
@testable import TogoKit

struct ForwardResponseTests {
    @Test func refusedUpstreamDoesNotCountAsCallbackDelivery() async throws {
        let count = Mutex(0)
        let forward = LocalForward(opener: FakeOpener(.fail(DirectTCPIPError.refused)), target: LocalForwardTests.target,
                                   preferredLocalPort: nil, onResponse: { count.withLock { $0 += 1 } })
        let port = try await forward.start()
        let url = try #require(URL(string: "http://127.0.0.1:\(port)/callback"))
        _ = try? await URLSession(configuration: .ephemeral).data(from: url)
        await forward.stop()
        #expect(count.withLock { $0 } == 0)
    }

    @Test func responseIsReportedAfterTheRemoteReturnsBytes() async throws {
        let count = Mutex(0)
        let forward = LocalForward(opener: FakeOpener(.open(.http("response"))), target: LocalForwardTests.target,
                                   preferredLocalPort: nil, onResponse: { count.withLock { $0 += 1 } })
        let port = try await forward.start()
        let url = try #require(URL(string: "http://127.0.0.1:\(port)/callback"))
        let (body, _) = try await URLSession(configuration: .ephemeral).data(from: url)
        #expect(String(decoding: body, as: UTF8.self) == "response")
        await forward.stop()
        #expect(count.withLock { $0 } == 1)
    }
}
