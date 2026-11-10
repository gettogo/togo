import Foundation
import Testing
@testable import TogoKit

struct HelperSourceTests {
    static let capabilities = ["session.v1", "events.v1", "approvals.v1"]

    /// A source on a pipe whose far end has answered hello and one subscribe.
    static func subscribed(
        clock: ManualClock, callTimeout: Duration = .seconds(10)
    ) async throws -> (PipeChannel, HelperSource) {
        let channel = PipeChannel()
        let source = HelperSource(host: "work-mac", clock: clock, callTimeout: callTimeout) {
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

    /// The Mac slept or the network stopped passing traffic: TCP stays open, so only
    /// unanswered pings show it. Two in a row end the events, which makes the monitor reconnect.
    @Test func unansweredKeepalivesEndTheEvents() async throws {
        let clock = ManualClock()
        let (channel, source) = try await Self.subscribed(clock: clock, callTimeout: .milliseconds(50))
        #expect(await clock.runSleep(HelperLink.keepaliveInterval))
        let first = try #require(await channel.nextRequest())
        #expect(first.method == "events.keepalive")
        // The second ping goes out at once, without another interval.
        let second = try #require(await channel.nextRequest())
        #expect(second.method == "events.keepalive")
        for await _ in source.events {}
        // The events end as the client shuts down; the channel closes right after.
        #expect(await eventually { channel.isClosed })
    }

    @Test func oneAnsweredKeepaliveKeepsTheConnection() async throws {
        let clock = ManualClock()
        let (channel, source) = try await Self.subscribed(clock: clock, callTimeout: .milliseconds(200))
        #expect(await clock.runSleep(HelperLink.keepaliveInterval))
        _ = try #require(await channel.nextRequest())
        let second = try #require(await channel.nextRequest())
        await channel.reply(to: second.id, result: "{}")
        #expect(await eventually { clock.pending == [HelperLink.keepaliveInterval] })
        #expect(!channel.isClosed)
        await source.close()
    }

    /// A call that times out is followed by a ping; a dead connection is closed.
    @Test func timedOutCallOnADeadConnectionEndsTheEvents() async throws {
        let clock = ManualClock()
        let (channel, source) = try await Self.subscribed(clock: clock, callTimeout: .milliseconds(50))
        await #expect(throws: HelperError.timeout(method: "session.kill")) { _ = try await source.call(.kill("s1")) }
        for await _ in source.events {}
        // The events end as the client shuts down; the channel closes right after.
        #expect(await eventually { channel.isClosed })
    }

    @Test func timedOutCallOnALiveConnectionKeepsIt() async throws {
        let clock = ManualClock()
        let (channel, source) = try await Self.subscribed(clock: clock, callTimeout: .milliseconds(300))
        let kill = Task { try await source.call(.kill("s1")) }
        let request = try #require(await channel.nextRequest())
        #expect(request.method == "session.kill")
        let ping = try #require(await channel.nextRequest())
        #expect(ping.method == "events.keepalive")
        await channel.reply(to: ping.id, result: "{}")
        _ = try? await kill.value
        #expect(await stays { channel.isClosed })
        await source.close()
    }
}

struct HelperLivenessTests {
    @Test func timeoutsInARowAreDead() async {
        let clock = ManualClock()
        let watching = Task { await HelperLiveness.watch(clock: clock) { throw HelperError.timeout(method: "events.keepalive") } }
        #expect(await clock.runSleep(HelperLiveness.interval))
        #expect(await watching.value == .dead)
    }

    @Test func aCloseIsReportedAsClosed() async {
        let clock = ManualClock()
        let watching = Task { await HelperLiveness.watch(clock: clock) { throw HelperError.closed } }
        #expect(await clock.runSleep(HelperLiveness.interval))
        #expect(await watching.value == .closed)
    }

    @Test func anErrorReplyProvesTheHelperAlive() async {
        let clock = ManualClock()
        let watching = Task {
            await HelperLiveness.watch(clock: clock) { throw HelperError.rpc(RPCErrorObject(code: -32000, message: "busy")) }
        }
        #expect(await clock.runSleep(HelperLiveness.interval))
        #expect(await clock.runSleep(HelperLiveness.interval))
        watching.cancel()
        #expect(await watching.value == nil)
    }

    @Test func anOldHelperCannotBePinged() async {
        let clock = ManualClock()
        let watching = Task { await HelperLiveness.watch(clock: clock) { throw HelperError.unsupported(capability: "events.v1") } }
        #expect(await clock.runSleep(HelperLiveness.interval))
        #expect(await watching.value == .unsupported)
    }
}

/// A channel that ends before hello with a given exit status, as a helper that did not run.
private final class EndedChannel: ExecChannel {
    let incoming = ByteStream()
    let exitStatus: Int32?
    init(status: Int32?) {
        exitStatus = status
        incoming.finish()
    }
    func write(_ data: Data) async throws { throw SSHError.channelClosed(exitStatus: exitStatus) }
    func close() async {}
}

struct HelperStartTests {
    static func open(status: Int32?) async throws -> HelperPresence {
        let source = HelperSource(host: "mini", clock: ManualClock()) {
            HelperDial(channel: EndedChannel(status: status), hangUp: {})
        }
        defer { Task { await source.close() } }
        return try await source.open().0
    }

    @Test func notFoundReadsAsMissing() async throws {
        #expect(try await Self.open(status: 127) == .missing)
        #expect(try await Self.open(status: nil) == .missing)
    }

    /// Installed but the daemon would not start: a distinct, retryable failure, not "not installed".
    @Test func aHelperThatStoppedReadsAsNotStarted() async {
        await #expect(throws: HelperSourceError.didNotStart(host: "mini", status: 1)) { _ = try await Self.open(status: 1) }
    }
}
