#if DEBUG
import Foundation
import Testing
import TogoKit
@testable import Togo

@MainActor struct StrictReplayTests {
    private let session = HarnessSubject(hostID: UUID(), sessionID: "s_recorded", capabilities: [Capability.harness])

    private func replay(_ tape: String) throws -> StrictReplayHarnessServices {
        try StrictReplayHarnessServices(jsonl: tape)
    }

    @Test func sameActionWithADifferentTargetDoesNotPass() async throws {
        let service = try replay("""
        {"jsonrpc":"2.0","id":1,"method":"harness.action","params":{"id":"s_recorded","action":"choose","target":"row-one","args":{"value":"A"}}}
        {"jsonrpc":"2.0","id":1,"result":{"result":"applied"}}
        """)
        await #expect(throws: (any Error).self) {
            _ = try await service.act(session, action: "choose", target: "row-two", args: ["value": .string("A")])
        }
        #expect(throws: (any Error).self) { try service.verifyConsumed() }
    }

    @Test func recordedRefusalIsReturnedAndConsumesTheAction() async throws {
        let service = try replay("""
        {"jsonrpc":"2.0","id":1,"method":"harness.action","params":{"id":"s_recorded","action":"choose","target":"row-one"}}
        {"jsonrpc":"2.0","id":1,"error":{"code":-32008,"message":"Refused","data":{"reason":"moved"}}}
        """)
        let expected = HelperError.rpc(.init(code: -32008, message: "Refused", data: .object(["reason": .string("moved")])))
        await #expect(throws: expected) {
            _ = try await service.act(session, action: "choose", target: "row-one", args: nil)
        }
        try service.verifyConsumed()
    }

    @Test func aLaterActionCannotSkipAnEarlierOne() async throws {
        let service = try replay("""
        {"jsonrpc":"2.0","id":1,"method":"harness.action","params":{"id":"s_recorded","action":"first"}}
        {"jsonrpc":"2.0","id":1,"result":{"result":"applied"}}
        {"jsonrpc":"2.0","id":2,"method":"harness.action","params":{"id":"s_recorded","action":"second"}}
        {"jsonrpc":"2.0","id":2,"result":{"result":"applied"}}
        """)
        #expect(throws: (any Error).self) { try service.verifyConsumed() }
        await #expect(throws: (any Error).self) {
            _ = try await service.act(session, action: "second", target: nil, args: nil)
        }
    }

    @Test func anUnrecordedSendCannotFabricateAnItem() async throws {
        let service = try replay("")
        await #expect(throws: (any Error).self) {
            _ = try await service.send(session, text: "Run the neutral check", mode: .now, images: [])
        }
        #expect(throws: (any Error).self) { try service.verifyConsumed() }
    }

    @Test func aReadCannotUseAResponseFromAFutureStep() async throws {
        let service = try replay("""
        {"jsonrpc":"2.0","id":1,"method":"harness.action","params":{"id":"s_recorded","action":"refresh"}}
        {"jsonrpc":"2.0","id":1,"result":{"result":"applied"}}
        {"jsonrpc":"2.0","id":2,"method":"harness.history","params":{"id":"s_recorded","before":5}}
        {"jsonrpc":"2.0","id":2,"result":{"items":[],"more":false}}
        """)
        await #expect(throws: (any Error).self) {
            _ = try await service.history(session, before: 5)
        }
        #expect(throws: (any Error).self) { try service.verifyConsumed() }
    }

    @Test(.timeLimit(.minutes(1))) func replayReopensWithTheRecordedEpochAndCursor() async throws {
        let service = try replay("""
        {"jsonrpc":"2.0","id":1,"method":"harness.open","params":{"id":"s_recorded","after":0}}
        {"jsonrpc":"2.0","id":1,"result":{"epoch":"e-first","cursor":5,"items":[],"more":false,"mission":{"rev":0,"summary":{"needsYou":0,"agents":0,"background":0,"working":false},"sections":[]}}}
        {"jsonrpc":"2.0","method":"replay.eof","params":{"id":"s_recorded"}}
        {"jsonrpc":"2.0","id":1,"method":"harness.open","params":{"id":"s_recorded","after":5}}
        {"jsonrpc":"2.0","id":1,"result":{"epoch":"e-second","cursor":7,"items":[],"more":false,"mission":{"rev":0,"summary":{"needsYou":0,"agents":0,"background":0,"working":false},"sections":[]}}}
        """)
        var first = service.feed(session, after: 0).makeAsyncIterator()
        guard case .opened(let start)? = try await first.next() else { Issue.record("Missing first open"); return }
        #expect(start.epoch == "e-first")
        var second = service.feed(session, after: 5).makeAsyncIterator()
        guard case .opened(let reopened)? = try await second.next() else { Issue.record("Missing reopen"); return }
        #expect(reopened.epoch == "e-second")
        #expect(reopened.cursor == 7)
        #expect(try await first.next() == nil)
        try service.verifyConsumed()
    }

    @Test func aFailedOpenCannotHideAnUnsupportedEvent() async throws {
        let service = try replay("""
        {"jsonrpc":"2.0","id":1,"method":"harness.open","params":{"id":"s_recorded","after":0}}
        {"jsonrpc":"2.0","id":1,"error":{"code":-32008,"message":"Refused"}}
        {"jsonrpc":"2.0","method":"unsupported.event","params":{}}
        """)
        var feed = service.feed(session, after: 0).makeAsyncIterator()
        do {
            _ = try await feed.next()
            Issue.record("Expected the failed open to end the feed with an error")
        } catch {
            // The failure must also be visible to the scenario's final verification.
        }
        #expect(throws: (any Error).self) { try service.verifyConsumed() }
    }
}
#endif
