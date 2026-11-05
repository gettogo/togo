import Foundation
import Testing
@testable import TogoKit

struct HelperSourceTests {
    static let capabilities = ["session.v1", "events.v1", "approvals.v1"]

    /// A source on a pipe whose far end has answered hello and one subscribe.
    static func subscribed(clock: ManualClock) async throws -> (PipeChannel, HelperSource) {
        let channel = PipeChannel()
        let source = HelperSource(host: "work-mac", clock: clock) {
            HelperDial(channel: channel, hangUp: {})
        }
        async let accepted: Void = channel.acceptHello(capabilities: capabilities)
        _ = try await source.open()
        await accepted
        let snapshot = Task { try await source.snapshot() }
        let request = try #require(await channel.nextRequest())
        #expect(request.method == "events.subscribe")
        await channel.reply(to: request.id, result: #"{"seq":0,"sessions":[],"approvals":[]}"#)
        _ = try await snapshot.value
        return (channel, source)
    }

    @Test func keepaliveStopsAfterClose() async throws {
        let clock = ManualClock()
        let (_, source) = try await Self.subscribed(clock: clock)
        #expect(await eventually { clock.pending == [HelperLink.keepaliveInterval] })

        await source.close()
        #expect(await stays { !clock.pending.isEmpty })
        // A snapshot after close must not start a keepalive either.
        await #expect(throws: HelperError.closed) { try await source.snapshot() }
        #expect(await stays { !clock.pending.isEmpty })
    }

    @Test func keepaliveEndsWhenHelperGoesAway() async throws {
        let clock = ManualClock()
        let (channel, source) = try await Self.subscribed(clock: clock)
        #expect(await eventually { clock.pending == [HelperLink.keepaliveInterval] })

        await channel.close()
        for await _ in source.events {}
        clock.advance(HelperLink.keepaliveInterval)
        #expect(await stays { !clock.pending.isEmpty })
        await source.close()
    }
}
