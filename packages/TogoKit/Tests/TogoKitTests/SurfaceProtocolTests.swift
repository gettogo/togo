import Foundation
import Testing
@testable import TogoKit

struct SurfaceProtocolTests {
    private struct Exchange<Result: Decodable>: Decodable {
        struct Response: Decodable { var result: Result }
        var response: Response
    }

    private func chunk() throws -> SurfaceReadResult {
        try JSONDecoder().decode(Exchange<SurfaceReadResult>.self,
            from: HarnessFixtureTests.fixture("surface.read.json")).response.result
    }

    @Test func readsSharedSnapshotAndDoesNotCreateAcknowledgment() throws {
        let chunk = try chunk()
        var assembler = SurfaceSnapshotAssembler(generation: chunk.generation, kinds: ["col", "text", "editor", "button", "card"])
        let assembled = try assembler.append(chunk)
        let scene = try #require(assembled)
        #expect(scene.program == "omp")
        #expect(scene.views[0].document.focus == "input")
        #expect(scene.views[0].document.root.children[0].properties["sendable"] == .bool(true))
    }

    @Test func rejectsChangedSnapshotBetweenChunks() throws {
        var first = try chunk()
        let all = first.data
        first.data = Data(all.prefix(80))
        first.nextOffset = 80
        var assembler = SurfaceSnapshotAssembler(generation: first.generation, kinds: ["col", "text", "editor", "button", "card"])
        let assembled = try assembler.append(first)
        #expect(assembled == nil)
        var last = first
        last.snapshot = String(repeating: "d", count: 64)
        last.offset = 80
        last.data = Data(all.dropFirst(80))
        last.nextOffset = all.count
        #expect(throws: SurfaceProtocolError.invalidSnapshot) { try assembler.append(last) }
    }

    @Test func assemblesContiguousChunksBeforePublishing() throws {
        var first = try chunk()
        let all = first.data
        first.data = Data(all.prefix(80))
        first.nextOffset = 80
        var assembler = SurfaceSnapshotAssembler(generation: first.generation, kinds: ["col", "text", "editor", "button", "card"])
        let pending = try assembler.append(first)
        #expect(pending == nil)
        var last = first
        last.offset = 80
        last.data = Data(all.dropFirst(80))
        last.nextOffset = all.count
        let scene = try assembler.append(last)
        #expect(scene?.views.first?.document.focus == "input")
    }

    @Test func rejectsUnknownKindsAndDuplicateNodes() throws {
        let chunk = try chunk()
        #expect(throws: SurfaceProtocolError.unsupportedContent) {
            try SurfaceScene.decode(chunk.data, kinds: ["col", "text"])
        }
        let invalid = String(decoding: chunk.data, as: UTF8.self).replacingOccurrences(of: #""id":"input""#, with: #""id":"s:1""#)
        #expect(throws: SurfaceProtocolError.invalidSnapshot) {
            try SurfaceScene.decode(Data(invalid.utf8), kinds: ["col", "text", "editor", "button", "card"])
        }
    }

    @Test func rejectsPaddedOrOversizedChunkBeforeAssembly() throws {
        let raw = try HarnessFixtureTests.fixture("surface.read.json")
        var exchange = try #require(JSONSerialization.jsonObject(with: raw) as? [String: Any])
        var response = try #require(exchange["response"] as? [String: Any])
        var result = try #require(response["result"] as? [String: Any])
        result["data"] = "AA=="
        response["result"] = result
        exchange["response"] = response
        let padded = try JSONSerialization.data(withJSONObject: exchange)
        #expect(throws: (any Error).self) {
            try JSONDecoder().decode(Exchange<SurfaceReadResult>.self, from: padded)
        }
        var chunk = try chunk()
        chunk.total = SurfaceSnapshotAssembler.maxBytes + 1
        var assembler = SurfaceSnapshotAssembler(generation: chunk.generation, kinds: ["col", "editor"])
        #expect(throws: SurfaceProtocolError.invalidSnapshot) { try assembler.append(chunk) }
    }
}
