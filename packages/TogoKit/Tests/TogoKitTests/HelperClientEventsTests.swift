import Foundation
import Testing
import TogoKit

struct HelperClientEventsTests {
    static let allCapabilities = [
        "session.v1", "agents.v1", "fs.v1", "capture.v1", "events.v1", "approvals.v1", "mute.v1", "upload.v1",
    ]

    @Test func eventsBeforeSnapshotSeqDropped() async throws {
        let (channel, client) = try await readyClient()
        let subscribe = Task { try await client.subscribe(foreground: true) }
        let request = try #require(await channel.nextRequest())
        #expect(request.method == "events.subscribe")
        #expect(request.params["foreground"] == .bool(true))
        await channel.reply(to: request.id, result: #"{"seq":5,"sessions":[],"approvals":[]}"#)
        for seq in [4, 5, 6] { await channel.sendJSON(Self.notify(seq: seq)) }

        #expect(try await subscribe.value.seq == 5)
        try await routeEarlierFrames(channel, client)
        await client.close()
        #expect(await collect(client.notifications).map(\.seq) == [6])
    }

    @Test func resubscribeKeepsOneStream() async throws {
        let (channel, client) = try await readyClient()
        var iterator = client.notifications.makeAsyncIterator()
        for snapshotSeq in [2, 8] {
            try await subscribe(channel, client, snapshotSeq: snapshotSeq)
            await channel.sendJSON(Self.notify(seq: snapshotSeq + 1))
            #expect(await iterator.next()?.seq == snapshotSeq + 1)
        }
        await client.close()
        #expect(await iterator.next() == nil)
    }

    @Test func bufferedEventsInNewSnapshotDropped() async throws {
        let (channel, client) = try await readyClient()
        try await subscribe(channel, client, snapshotSeq: 0)
        // Nobody reads the stream yet, so these sit in its buffer.
        for seq in [1, 2, 3] { await channel.sendJSON(Self.notify(seq: seq)) }
        try await routeEarlierFrames(channel, client)
        try await subscribe(channel, client, snapshotSeq: 3)
        await channel.sendJSON(Self.notify(seq: 4))
        try await routeEarlierFrames(channel, client)
        await client.close()
        #expect(await collect(client.notifications).map(\.seq) == [4])
    }

    @Test func eventRacingResubscribeResponseDropped() async throws {
        let (channel, client) = try await readyClient()
        try await subscribe(channel, client, snapshotSeq: 0)
        await channel.sendJSON(Self.notify(seq: 1))
        let resubscribe = Task { try await client.subscribe(foreground: true) }
        let request = try #require(await channel.nextRequest())
        // Routed after the request went out but before its response: the new snapshot has it.
        await channel.sendJSON(Self.notify(seq: 2))
        await channel.reply(to: request.id, result: #"{"seq":2,"sessions":[],"approvals":[]}"#)
        await channel.sendJSON(Self.notify(seq: 3))
        #expect(try await resubscribe.value.seq == 2)
        try await routeEarlierFrames(channel, client)
        await client.close()
        #expect(await collect(client.notifications).map(\.seq) == [3])
    }

    @Test func readerBlockedBeforeResubscribeSkipsStaleEvents() async throws {
        let (channel, client) = try await readyClient()
        try await subscribe(channel, client, snapshotSeq: 0)
        await channel.sendJSON(Self.notify(seq: 1))
        try await routeEarlierFrames(channel, client)
        var iterator = client.notifications.makeAsyncIterator()
        #expect(await iterator.next()?.seq == 1)
        for seq in [2, 3] { await channel.sendJSON(Self.notify(seq: seq)) }
        try await routeEarlierFrames(channel, client)
        try await subscribe(channel, client, snapshotSeq: 3)
        await channel.sendJSON(Self.notify(seq: 4))
        #expect(await iterator.next()?.seq == 4)
        await client.close()
    }

    @Test func otherNotificationsIgnored() async throws {
        let (channel, client) = try await readyClient()
        await channel.sendJSON(#"{"jsonrpc":"2.0","method":"log","params":{"seq":1}}"#)
        await channel.sendJSON(#"{"jsonrpc":"2.0","method":"event","params":{"seq":"x"}}"#)
        await channel.sendJSON(Self.notify(seq: 1))
        try await routeEarlierFrames(channel, client)
        await client.close()
        #expect(await collect(client.notifications).map(\.seq) == [1])
    }

    @Test func answerSendsDecisionWireForm() async throws {
        let (channel, client) = try await readyClient()
        let answer = Task { try await client.answer(requestId: "r_abcdefghij", decision: .option(2)) }
        let request = try #require(await channel.nextRequest())
        #expect(request.method == "approval.answer")
        #expect(request.params["decision"] == .string("option:2"))
        await channel.reply(to: request.id, result: #"{"state":"answered"}"#)
        try await answer.value
    }

    @Test func answerConflictMapsToConflict() async throws {
        let (channel, client) = try await readyClient()
        let answer = Task { try await client.answer(requestId: "r_abcdefghij", decision: .approve) }
        let request = try #require(await channel.nextRequest())
        await channel.sendJSON(#"""
            {"jsonrpc":"2.0","id":\#(request.id),"error":{"code":-32006,"message":"conflict","data":{"reason":"answered","by":"desk"}}}
            """#)
        await #expect(throws: HelperError.conflict("answered")) { try await answer.value }
    }

    @Test func uploadOverLimitNeverSent() async throws {
        let (channel, client) = try await readyClient()
        let params = UploadParams(sessionId: "s_abcdefghij", name: "big.png", data: Data(count: 700_001))
        await #expect(throws: HelperError.tooLarge(700_001)) { try await client.upload(params) }
        let list = Task { try await client.listSessions() }
        let next = try #require(await channel.nextRequest())
        #expect(next.method == "session.list")
        await channel.reply(to: next.id, result: #"{"sessions":[]}"#)
        _ = try await list.value
    }

    @Test func callsNeedTheirCapability() async throws {
        let (_, client) = try await readyClient(capabilities: ["session.v1", "agents.v1", "fs.v1"])
        await #expect(throws: HelperError.unsupported(capability: "events.v1")) { try await client.subscribe(foreground: true) }
        await #expect(throws: HelperError.unsupported(capability: "approvals.v1")) { try await client.approvals(sessionId: nil) }
        await #expect(throws: HelperError.unsupported(capability: "mute.v1")) { try await client.setMute(id: "s_1", muted: true) }
        let upload = UploadParams(sessionId: "s_1", name: "a.txt", data: Data("a".utf8))
        await #expect(throws: HelperError.unsupported(capability: "upload.v1")) { try await client.upload(upload) }
    }

    @Test func setMuteReturnsSession() async throws {
        let (channel, client) = try await readyClient()
        let mute = Task { try await client.setMute(id: "s_abcdefghij", muted: true) }
        let request = try #require(await channel.nextRequest())
        #expect(request.params == ["id": .string("s_abcdefghij"), "muted": .bool(true)])
        await channel.reply(to: request.id, result: #"""
            {"id":"s_abcdefghij","bib":"01","name":"Shell","agent":"shell","cwd":"/tmp",\#
            "createdAt":"2026-11-03T12:00:00Z","status":"idle","activity":null,"attachedClients":0,\#
            "managed":true,"muted":true,"meta":{"model":null,"contextPercent":null,"costUsd":null}}
            """#)
        #expect(try await mute.value.muted)
    }

    @Test func notificationsFinishOnClose() async throws {
        let (channel, client) = try await readyClient()
        let drained = Task { await collect(client.notifications) }
        channel.incoming.finish()
        #expect(await drained.value.isEmpty)
        await #expect(throws: HelperError.closed) { try await client.listSessions() }
    }

    // MARK: - Helpers

    private static func notify(seq: Int) -> String {
        #"""
        {"jsonrpc":"2.0","method":"event","params":{"seq":\#(seq),"ts":"2026-11-03T12:00:00Z",\#
        "kind":"notify","sessionId":null,"title":"t","body":"b","level":"info"}}
        """#
    }

    private func subscribe(_ channel: PipeChannel, _ client: HelperClient, snapshotSeq: Int) async throws {
        let subscribe = Task { try await client.subscribe(foreground: true) }
        let request = try #require(await channel.nextRequest())
        await channel.reply(to: request.id, result: #"{"seq":\#(snapshotSeq),"sessions":[],"approvals":[]}"#)
        _ = try await subscribe.value
    }

    /// Frames are read in order, so once a later call is answered every frame sent
    /// before its reply has been routed.
    private func routeEarlierFrames(_ channel: PipeChannel, _ client: HelperClient) async throws {
        let list = Task { try await client.listSessions() }
        let request = try #require(await channel.nextRequest())
        await channel.reply(to: request.id, result: #"{"sessions":[]}"#)
        _ = try await list.value
    }

    private func collect(_ stream: AsyncStream<EventEnvelope>) async -> [EventEnvelope] {
        var events: [EventEnvelope] = []
        for await event in stream { events.append(event) }
        return events
    }

    private func readyClient(
        capabilities: [String] = allCapabilities
    ) async throws -> (PipeChannel, HelperClient) {
        let channel = PipeChannel()
        let client = HelperClient(channel: channel, callTimeout: .seconds(5))
        async let accepted: Void = channel.acceptHello(capabilities: capabilities)
        _ = try await client.hello(client: HelperClient.testClient)
        await accepted
        return (channel, client)
    }
}
