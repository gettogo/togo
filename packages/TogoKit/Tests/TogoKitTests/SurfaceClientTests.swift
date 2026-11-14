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
