import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor @Suite(.serialized)
struct ChatFrameTransitionTests {
    enum Position: CaseIterable, Sendable { case foot, history }

    @Test(arguments: Position.allCases)
    func sendKeepsTheViewportPopulated(_ position: Position) async throws {
        try await measure(position) { scene in
            let composer = scene.composer
            let baseline = try #require(scene.sampler.samples.last?.composer?.height)
            composer.typed("First line of the draft\nSecond line\nThird line")
            try await scene.wait("the expanded draft") { ($0.composer?.height ?? 0) > baseline + 8 && $0.hasRows }
            let time = scene.sampler.mark()
            await composer.submit()
            let frames = try await scene.after(time, "the new prompt at the foot") {
                $0.visible[.row("sent-1")] != nil && $0.distanceFromFoot < 12
            }
            #expect(scene.services.sent.count == 1)
            #expect(composer.draft.isEmpty)
            if position == .foot {
                let maxGap = frames.map(\.distanceFromFoot).max() ?? .infinity
                #expect(maxGap <= 40, "A send at the foot must not jump into history; furthest gap \(maxGap) pt")
            }
        }
    }

    @Test(arguments: Position.allCases)
    func workingKeepsTheReadersPosition(_ position: Position) async throws {
        try await measure(position) { scene in
            let before = try #require(scene.sampler.samples.last)
            let anchor = try scene.anchor(in: before)
            let time = scene.sampler.mark()
            let note = HarnessNotification(sessionId: scene.store.subject.sessionID, kind: .mission,
                ts: .init(.now), epoch: "initial",
                mission: .init(rev: 2, ops: [.init(op: .summary, summary: .init(working: true))]))
            scene.first.yield(.note(note))
            let frames = try await scene.after(time, "Working mission applied") {
                scene.store.working && (position == .history || $0.visible[.working] != nil)
            }
            if position == .history {
                scene.expectStable(anchor, before: before, frames: frames)
            } else {
                let maxGap = frames.map(\.distanceFromFoot).max() ?? .infinity
                #expect(maxGap <= 40, "Working must keep following the foot; furthest gap \(maxGap) pt")
            }
        }
    }

    @Test(arguments: Position.allCases)
    func reconnectKeepsTheSameVisibleLeaf(_ position: Position) async throws {
        try await measure(position) { scene in
            let before = try #require(scene.sampler.samples.last)
            let anchor = try scene.anchor(in: before)
            let time = scene.sampler.mark()
            scene.first.finish(throwing: HelperError.closed)
            let frames = try await scene.after(time, "the replacement epoch and live feed") {
                $0.epoch == "restarted" && scene.store.phase == .live
            }
            #expect(scene.services.opens.count >= 2)
            scene.expectStable(anchor, before: before, frames: frames)
        }
    }

    private func measure(_ position: Position, _ run: (Scene) async throws -> Void) async throws {
        let initial = Self.snapshot(epoch: "initial")
        let reconnect = Self.snapshot(epoch: "restarted")
        let feed = AsyncThrowingStream<HarnessFeed, any Error>.makeStream()
        let services = FakeHarnessServices(descriptor: HarnessSamples.descriptor)
        services.sendSequenceBase = initial.cursor
        services.feeds = [.init(stream: feed.stream), .init(elements: [.opened(reconnect)], end: .hold),
                          .init(elements: [.opened(reconnect)], end: .hold)]
        let store = HarnessSessionStore(
            subject: .init(hostID: UUID(), sessionID: "chat-frames", capabilities: [Capability.harness]),
            services: services, retry: (.milliseconds(5), .milliseconds(20)), expectsChat: true
        )
        store.launch()
        defer { store.stop(); feed.continuation.finish() }
        feed.continuation.yield(.opened(initial))
        let deadline = ContinuousClock.now + .seconds(5)
        while store.phase != .live, .now < deadline { try await Task.sleep(for: .milliseconds(10)) }
        try #require(store.phase == .live, "The fixture must finish loading before its viewport is mounted")
        let composer = CommandLineModel(store: store)
        let view = ChatView(store: store)
            .safeAreaInset(edge: .bottom, spacing: 0) { ChatComposerBar(model: composer) }
            .environment(\.measuresChatFrames, true)
            .environment(\.loopsPaused, true)
        let host = try GeometryHost(view, width: 402, height: 780)
        defer { host.close() }
        let sampler = ChatFrameSampler(view: host.view, store: store)
        sampler.start()
        defer { sampler.stop(); sampler.attachTrace() }
        let scene = Scene(store: store, composer: composer, services: services, first: feed.continuation, sampler: sampler)
        try await scene.wait("loaded transcript at its foot") { $0.epoch == "initial" && $0.hasRows && $0.distanceFromFoot < 12 }
        if position == .history {
            try #require(sampler.moveIntoHistory(by: 900))
            try await scene.wait("settled history above the foot") { frame in
                guard frame.hasRows, !frame.pinned, frame.distanceFromFoot > 500 else { return false }
                let recent = sampler.samples.filter { $0.time <= frame.time && $0.time >= frame.time - 0.3 }
                guard let first = recent.first, frame.time - first.time > 0.2 else { return false }
                return recent.allSatisfy {
                    $0.hasRows && !$0.pinned && abs($0.distanceFromFoot - frame.distanceFromFoot) < 1
                }
            }
        }
        try await run(scene)
    }

    private static func snapshot(epoch: String) -> HarnessOpenResult {
        var items: [HarnessItem] = []
        for turn in 0..<6 {
            let id = "turn-\(turn)"
            items.append(.init(id: "prompt-\(turn)", seq: items.count + 1, turnId: id, at: HarnessSamples.at,
                               body: .user(.init(text: "Inspect behavior \(turn).", queued: false, source: "desk"))))
            for reply in 0..<16 {
                let count = reply.isMultiple(of: 4) ? 24 : 3
                let text = (0..<count).map {
                    "Response \(turn).\(reply), paragraph \($0). Trace how the original conversation is read before opening the next page."
                }.joined(separator: "\n\n")
                items.append(.init(id: "reply-\(turn)-\(reply)", seq: items.count + 1, turnId: id, at: HarnessSamples.at,
                                   body: .assistant(.init(text: text, partial: false))))
            }
            items.append(.init(id: id, seq: items.count + 1, turnId: id, at: HarnessSamples.at,
                               body: .turn(.init(state: .done, startedAt: HarnessSamples.at, endedAt: HarnessSamples.at,
                                                 steps: 0, filesChanged: 0))))
        }
        return .init(epoch: epoch, cursor: items.count, items: items, more: false,
                     mission: .init(rev: 1, summary: .init(), sections: []))
    }
}

private extension ChatFrameTransitionTests {
    @MainActor struct Scene {
        var store: HarnessSessionStore
        var composer: CommandLineModel
        var services: FakeHarnessServices
        var first: AsyncThrowingStream<HarnessFeed, any Error>.Continuation
        var sampler: ChatFrameSampler

        func wait(_ label: String, _ ready: (ChatFrameSampler.Sample) -> Bool) async throws {
            let deadline = ContinuousClock.now + .seconds(5)
            while .now < deadline {
                let samples = sampler.samples.suffix(3)
                if samples.count == 3, samples.allSatisfy(ready) { return }
                try await Task.sleep(for: .milliseconds(10))
            }
            let last = sampler.samples.last
            Issue.record("Did not observe \(label), last epoch \(last?.epoch ?? "none"), foot gap \(last?.distanceFromFoot ?? -1), parts \(last?.visible.count ?? 0)")
            throw HarnessUnavailable()
        }

        func after(_ time: CFTimeInterval, _ label: String,
                   _ ready: (ChatFrameSampler.Sample) -> Bool) async throws -> [ChatFrameSampler.Sample] {
            try await wait(label) {
                $0.time >= time + 0.55 && ready($0) && sampler.samples.lazy.filter { $0.time >= time }.count >= 6
            }
            let frames = sampler.samples.filter { $0.time >= time }
            try #require(frames.count >= 6, "The test must measure a transition over multiple display ticks")
            let empty = frames.count { !$0.hasRows }
            #expect(empty == 0, "\(empty) of \(frames.count) frames had no visible chat rows")
            let overlap = frames.count {
                guard let composer = $0.composer else { return true }
                return $0.viewport.maxY > composer.minY + 2
            }
            #expect(overlap == 0, "\(overlap) frames lost the composer or drew the chat under it")
            return frames
        }

        func anchor(in frame: ChatFrameSampler.Sample) throws -> ChatFramePart {
            let center = frame.viewport.midY
            let leaves = frame.visible.filter { if case .leaf = $0.key { return true }; return false }
            return try #require(leaves.min(by: {
                abs($0.value.midY - center) < abs($1.value.midY - center)
            })?.key, "The baseline must contain a visible reply leaf")
        }

        func expectStable(_ anchor: ChatFramePart, before: ChatFrameSampler.Sample, frames: [ChatFrameSampler.Sample]) {
            guard let origin = before.offset(of: anchor) else { Issue.record("Starting leaf disappeared"); return }
            let offsets = frames.compactMap { $0.offset(of: anchor) }
            #expect(offsets.count == frames.count, "Keep the same leaf on screen throughout the transition")
            if let maximum = offsets.map({ abs($0 - origin) }).max() {
                #expect(maximum <= 8, "The visible reply leaf moved \(maximum) pt")
            }
        }
    }
}
