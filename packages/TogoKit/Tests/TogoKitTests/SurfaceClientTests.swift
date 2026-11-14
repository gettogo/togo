import Foundation
import Testing
@testable import TogoKit

struct SurfaceClientTests {
    @Test func olderHelperIsRejectedBeforeOpening() async throws {
        let channel = PipeChannel()
        let client = HelperClient(channel: channel)
        async let accepted: Void = channel.acceptHello()
        _ = try await client.hello(client: HelperClient.testClient)
        await accepted
        await #expect(throws: HelperError.unsupported(capability: "surface.v1")) {
            try await client.surfaceOpen(id: "s_abcdefghij", options: options)
        }
        await client.close()
    }

    @Test func openAndSnapshotStayOnTheLeaseConnection() async throws {
        let channel = PipeChannel()
        let client = HelperClient(channel: channel)
        async let accepted: Void = channel.acceptHello(capabilities: ["surface.v1"])
        _ = try await client.hello(client: HelperClient.testClient)
        await accepted
        let opening = Task { try await client.surfaceOpen(id: "s_abcdefghij", options: options) }
        let request = try #require(await channel.nextRequest())
        #expect(request.method == "surface.open")
        await channel.reply(to: request.id, result: try result("surface.open.json"))
        let lease = try await opening.value
        let reading = Task { try await client.surfaceSnapshot(lease, kinds: Set(options.kinds)) }
        let read = try #require(await channel.nextRequest())
        #expect(read.method == "surface.read")
        #expect(read.params["lease"] == .string(lease.token))
        await channel.reply(to: read.id, result: try result("surface.read.json"))
        let scene = try await reading.value
        #expect(scene?.views.first?.document.focus == "input")
        await client.close()
    }

    @Test func pendingBeforeDispatchWaitsWithoutReplayingAnUncertainAction() async throws {
        let (channel, client, lease) = try await openedClient()
        let operation = Task {
            try await client.surfaceInput(lease, surface: "s:1", instance: 1,
                event: .object(["ev": .string("action"), "sf": .string("s:1"), "id": .string("submit"), "act": .string("submit")]))
        }
        let first = try #require(await channel.nextRequest())
        await channel.fail(first.id, code: RPCErrorCode.refused, message: "pending", data: #"{"reason":"pending"}"#)
        try await Task.sleep(for: .milliseconds(120))
        // A barrier makes the missing retry fail explicitly instead of hanging.
        let barrier = Task { try await client.keepalive() }
        let second = try #require(await channel.nextRequest())
        #expect(second.method == "surface.input")
        if second.method == "surface.input" {
            #expect(second.params == first.params)
            await channel.reply(to: second.id, result: #"{"dispatched":true}"#)
            #expect(try await operation.value.dispatched)
        }
        await client.close()
        _ = try? await operation.value
        _ = try? await barrier.value
    }

    @Test func uncertainDispatchFailureIsNeverRetried() async throws {
        let (channel, client, lease) = try await openedClient()
        let operation = Task {
            try await client.surfaceInput(lease, surface: "s:1", instance: 1,
                event: .object(["ev": .string("action"), "sf": .string("s:1"), "id": .string("submit"), "act": .string("submit")]))
        }
        let first = try #require(await channel.nextRequest())
        await channel.fail(first.id, code: RPCErrorCode.refused, message: "unavailable", data: #"{"reason":"unavailable"}"#)
        await #expect(throws: HelperError.self) { try await operation.value }
        let barrier = Task { try await client.keepalive() }
        let next = try #require(await channel.nextRequest())
        #expect(next.method == "events.keepalive")
        await client.close()
        _ = try? await barrier.value
    }

    @Test func aDisplayReceiptWaitsForTheDurableDocument() async throws {
        let (channel, client, lease) = try await openedClient()
        struct Exchange: Decodable { struct Response: Decodable { var result: SurfaceReadResult }; var response: Response }
        let chunk = try JSONDecoder().decode(Exchange.self, from: HarnessFixtureTests.fixture("surface.read.json")).response.result
        let view = try #require(SurfaceScene.decode(chunk.data, kinds: Set(options.kinds)).views.first)
        let operation = Task { try await client.surfaceAck(lease, view: view) }
        let first = try #require(await channel.nextRequest())
        await channel.fail(first.id, code: RPCErrorCode.refused, message: "pending", data: #"{"reason":"pending"}"#)
        try await Task.sleep(for: .milliseconds(120))
        let barrier = Task { try await client.keepalive() }
        let second = try #require(await channel.nextRequest())
        #expect(second.method == "surface.ack")
        if second.method == "surface.ack" {
            #expect(second.params == first.params)
            await channel.reply(to: second.id, result: "{}")
            try await operation.value
        }
        await client.close()
        _ = try? await operation.value
        _ = try? await barrier.value
    }

    private func openedClient() async throws -> (PipeChannel, HelperClient, SurfaceLease) {
        let channel = PipeChannel()
        let client = HelperClient(channel: channel)
        async let accepted: Void = channel.acceptHello(capabilities: ["surface.v1", "events.v1"])
        _ = try await client.hello(client: HelperClient.testClient)
        await accepted
        let opening = Task { try await client.surfaceOpen(id: "s_abcdefghij", options: options) }
        let request = try #require(await channel.nextRequest())
        await channel.reply(to: request.id, result: try result("surface.open.json"))
        return (channel, client, try await opening.value)
    }

    private var options: SurfaceOptions {
        SurfaceOptions(kinds: ["col", "text", "editor", "button", "card"], cols: 80, dark: true, reduceMotion: false)
    }

    private func result(_ name: String) throws -> String {
        let exchange = try JSONDecoder().decode(JSONValue.self, from: HarnessFixtureTests.fixture(name))
        guard case .object(let fields) = exchange, case .object(let response)? = fields["response"], let result = response["result"] else {
            throw SurfaceProtocolError.invalidSnapshot
        }
        return String(decoding: try JSONEncoder().encode(result), as: UTF8.self)
    }
}
