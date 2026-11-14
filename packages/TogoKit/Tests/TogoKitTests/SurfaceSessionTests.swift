import Foundation
import Testing
@testable import TogoKit

@MainActor struct SurfaceSessionTests {
    @MainActor final class Connection: SurfaceConnection {
        var next: SurfaceScene?
        var acknowledgments: [Int64] = []
        var closed = false
        var closing = false
        var closeGate: AsyncStream<Void>?
        var inputs = 0
        let lease: SurfaceLease

        init() throws {
            struct Exchange<T: Decodable>: Decodable {
                struct Response: Decodable { var result: T }
                var response: Response
            }
            lease = try JSONDecoder().decode(Exchange<SurfaceLease>.self,
                from: HarnessFixtureTests.fixture("surface.open.json")).response.result
            let chunk = try JSONDecoder().decode(Exchange<SurfaceReadResult>.self,
                from: HarnessFixtureTests.fixture("surface.read.json")).response.result
            next = try SurfaceScene.decode(chunk.data, kinds: ["col", "text", "editor", "button", "card"])
        }

        func surfaceOpen(id: String, options: SurfaceOptions) async throws -> SurfaceLease { lease }
        func surfaceSnapshot(_ lease: SurfaceLease, revision: UInt64, kinds: Set<String>) async throws -> SurfaceScene? {
            defer { next = nil }
            return next
        }
        func surfaceInput(_ lease: SurfaceLease, surface: String, instance: UInt64, event: JSONValue, expectedText: String?) async throws -> SurfaceDispatchResult {
            inputs += 1
            return SurfaceDispatchResult(dispatched: true)
        }
        func surfaceAck(_ lease: SurfaceLease, view: SurfaceView) async throws { acknowledgments.append(view.document.sequence) }
        func surfaceClose(_ lease: SurfaceLease) async throws {}
        func close() async {
            closing = true
            if let closeGate { for await _ in closeGate { break } }
            closed = true
        }
    }

    @Test func receivingDoesNotAcknowledgeAndCancellationClosesTheConnection() async throws {
        let connection = try Connection()
        let session = SurfaceSession(id: connection.lease.id, makeConnection: { connection }, pollInterval: .milliseconds(10))
        let task = Task { await session.run(options: options) }
        try await wait { session.scene != nil }
        #expect(connection.acknowledgments.isEmpty)
        let scene = try #require(session.scene)
        await session.displayed(revision: scene.revision, generation: connection.lease.generation)
        await session.displayed(revision: scene.revision, generation: connection.lease.generation)
        #expect(connection.acknowledgments == [1])
        task.cancel()
        await task.value
        #expect(connection.closed)
        #expect(session.phase == .suspended)
    }

    @Test func dispatchedInputWaitsForTheProducerAndIsNeverRetried() async throws {
        let connection = try Connection()
        let session = SurfaceSession(id: connection.lease.id, makeConnection: { connection }, pollInterval: .milliseconds(10), pendingTimeout: .milliseconds(30))
        let task = Task { await session.run(options: options) }
        defer { task.cancel() }
        try await wait { session.scene != nil }
        await session.input(surface: "s:1", node: "input", event: ["ev": .string("send"), "text": .string("/resume")], expectedText: "")
        #expect(session.pending != nil)
        try await wait { session.notice == .unconfirmed }
        #expect(connection.inputs == 1)
        #expect(session.pending == nil)
        task.cancel()
        await task.value
    }

    @Test func changedProducerTextSettlesAnEdit() async throws {
        let connection = try Connection()
        let initial = try #require(connection.next)
        let session = SurfaceSession(id: connection.lease.id, makeConnection: { connection }, pollInterval: .milliseconds(10))
        let task = Task { await session.run(options: options) }
        defer { task.cancel() }
        try await wait { session.scene != nil }
        await session.input(surface: "s:1", node: "input", event: ["ev": .string("edit"), "text": .string("hello"),
            "from": .number(0), "to": .number(0), "len": .number(0), "cursor": .number(5)])
        var next = initial
        next.revision += 1
        next.views[0].document.root.children[0].properties["text"] = .string("hello")
        connection.next = next
        try await wait { session.pending == nil }
        #expect(session.notice == nil)
        task.cancel()
        await task.value
    }

    @Test func anOldStopCannotSuspendANewRun() async throws {
        let first = try Connection()
        let second = try Connection()
        let (gate, continuation) = AsyncStream<Void>.makeStream()
        first.closeGate = gate
        var opens = 0
        let session = SurfaceSession(id: first.lease.id, makeConnection: {
            opens += 1
            return opens == 1 ? first : second
        }, pollInterval: .milliseconds(10))
        let oldRun = Task { await session.run(options: options) }
        try await wait { session.scene != nil }
        let stopping = Task { await session.stop() }
        try await wait { first.closing }
        let newRun = Task { await session.run(options: options) }
        await Task.yield()
        continuation.yield(())
        continuation.finish()
        await stopping.value
        await oldRun.value
        try await wait { opens == 2 && session.scene != nil }
        #expect(session.phase == .active)
        newRun.cancel()
        await newRun.value
    }

    private var options: SurfaceOptions {
        SurfaceOptions(kinds: ["col", "text", "editor", "button", "card"], cols: 80, dark: true, reduceMotion: false)
    }

    private func wait(_ condition: @MainActor () -> Bool) async throws {
        let deadline = ContinuousClock.now + .seconds(3)
        while !condition() {
            guard ContinuousClock.now < deadline else { throw SurfaceProtocolError.invalidSnapshot }
            try await Task.sleep(for: .milliseconds(5))
        }
    }
}
