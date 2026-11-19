import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

/// A helper-exported scenario and its wire tape have the same resource basename.
private struct EngineReplayManifest: Decodable {
    let scenarioId: String
    let agent: String
    let adapter: String
    let sessionId: String
    let helperRevision: String
}

@MainActor @Suite(.serialized)
struct EngineReplayLayoutTests {
    @Test
    func renderedConversationSurvivesHelperRestart() async throws {
        let bundle = Bundle(for: EngineReplayBundleMarker.self)
        let manifestURL = try #require(bundle.url(forResource: "engine-restart", withExtension: "json"),
                                       "Include the exported engine restart manifest in the test bundle")
        let manifest = try JSONDecoder().decode(EngineReplayManifest.self, from: Data(contentsOf: manifestURL))
        let tapeURL = try #require(bundle.url(forResource: "engine-restart", withExtension: "jsonl"))
        let jsonl = try String(contentsOf: tapeURL, encoding: .utf8)
        let tape = try StrictReplayTape(jsonl: jsonl)
        let expected = try checkpoint(from: tape)
        let service = try StrictReplayHarnessServices(jsonl: jsonl)
        let store = HarnessSessionStore(subject: .init(hostID: UUID(), sessionID: manifest.sessionId,
            capabilities: [Capability.harness]), services: service, retry: (.milliseconds(5), .milliseconds(20)), expectsChat: true)
        store.launch()
        defer { store.stop() }
        let layout = ScreenSize.iPhone17
        let safe = layout.safeArea
        let content = ChatView(store: store)
            .padding(EdgeInsets(top: safe.top, leading: safe.left, bottom: safe.bottom, trailing: safe.right))
            .frame(width: layout.size.width, height: layout.size.height)
            .environment(\.horizontalSizeClass, layout.sizeClass)
            .environment(\.dynamicTypeSize, .large)
            .environment(\.measuresChatFrames, true)
            .environment(\.loopsPaused, true)
        let host = try GeometryHost(content, width: layout.size.width, height: layout.size.height)
        defer { host.close() }
        let sampler = ChatFrameSampler(view: host.view, store: store)
        sampler.start()
        defer { sampler.stop() }

        let limit = ContinuousClock.now + .seconds(5)
        while service.verdict != "complete" || store.phase != .live || sampler.samples.last?.visible[.row(expected.reply)] == nil {
            guard .now < limit, !service.verdict.hasPrefix("failed") else { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        try service.verifyConsumed()
        try #require(store.phase == .live)
        try #require(store.timeline.epoch == expected.epoch)
        try #require(store.timeline.cursor == expected.cursor)
        try #require(store.descriptor?.adapter?.id == manifest.adapter)
        try #require(expected.itemIDs.isSubset(of: Set(store.timeline.items.map(\.id))),
                     "An item from the real engine open was silently dropped")
        let frame = try #require(sampler.samples.last)
        try #require(frame.visible[.row(expected.reply)] != nil && frame.visible[.leaf(expected.reply, 0)] != nil,
                     "The replayed reply did not render in the chat viewport")
        let bounds = host.window.bounds.inset(by: safe).insetBy(dx: -1, dy: -1)
        try #require(bounds.contains(frame.viewport), "The transcript viewport must respect the modeled safe area")
        let evidence = try JSONEncoder().encode(Evidence(
            scenarioId: manifest.scenarioId, helperRevision: manifest.helperRevision,
            key: "\(manifest.agent)/app/\(manifest.adapter):conversation:chat:iPhone17:default"))
        Attachment.record(Array(evidence), named: "engine-replay-proof.json")
    }

    private func checkpoint(from tape: StrictReplayTape) throws -> (epoch: String, cursor: Int, itemIDs: Set<String>, reply: String) {
        let opens = tape.exchanges.filter { $0.method == "harness.open" }
        try #require(opens.count >= 3)
        let last = try #require(opens.last)
        guard case .value(.object(let body)) = last.reply,
              case .value(.object(let initial)) = opens[0].reply,
              case .string(let oldEpoch) = initial["epoch"], case .string(let epoch) = body["epoch"],
              case .number(let cursor) = body["cursor"], case .array(let items) = body["items"] else {
            throw HarnessUnavailable()
        }
        try #require(oldEpoch != epoch, "The helper must have produced a replacement epoch")
        let itemIDs = try items.map { item -> String in
            guard case .object(let fields) = item, case .string(let id) = fields["id"] else {
                throw HarnessUnavailable()
            }
            return id
        }
        let lastReply = try #require(items.reversed().first { item in
            if case .object(let row) = item { return row["kind"] == .string("assistant") }
            return false
        })
        guard case .object(let row) = lastReply, case .string(let reply) = row["id"],
              let exactCursor = Int(exactly: cursor) else { throw HarnessUnavailable() }
        return (epoch, exactCursor, Set(itemIDs), reply)
    }

    private struct Evidence: Encodable {
        var scenarioId: String
        var helperRevision: String
        var key: String
    }

}

private final class EngineReplayBundleMarker {}
