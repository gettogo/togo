import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

/// Helper restarts change the wire epoch, even when the same conversation is replayed.
@MainActor @Suite(.serialized) struct ChatReopenLayoutTests {
    @Test(arguments: [false, true])
    func aHelperRestartKeepsTheReadersVisibleText(atFoot: Bool) async throws {
        let services = FakeHarnessServices(descriptor: ChatTests.claude)
        let first = AsyncThrowingStream<HarnessFeed, any Error>.makeStream()
        let initial = snapshot(epoch: "initial")
        let replay = snapshot(epoch: "restarted")
        services.feeds = [.init(stream: first.stream), .init(elements: [.opened(replay)], end: .hold),
                         .init(elements: [.opened(replay)], end: .hold)]
        first.continuation.yield(.opened(initial))
        let store = HarnessSessionStore(subject: ChatTests.subject, services: services, retry: (.milliseconds(5), .milliseconds(20)))
        store.launch()
        defer { store.stop(); first.continuation.finish() }
        try await until { store.phase == .live }
        let scene = try #require(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first)
        let previous = scene.keyWindow
        let host = UIHostingController(rootView: ChatView(store: store))
        let window = UIWindow(windowScene: scene)
        window.frame = CGRect(x: 0, y: 0, width: 800, height: 420)
        window.rootViewController = host
        window.makeKeyAndVisible()
        defer { window.isHidden = true; window.rootViewController = nil; previous?.makeKeyAndVisible() }
        host.view.frame = window.bounds
        host.view.layoutIfNeeded()
        try await Task.sleep(for: .milliseconds(500))
        let before = try mainScroll(host.view)
        if !atFoot {
            store.chatScroll.go(.row("reply-5-14"), reduce: true)
            try await Task.sleep(for: .milliseconds(500))
        }
        host.view.layoutIfNeeded()
        let viewport = visible(before)
        let reading = try #require(all(before).compactMap { $0 as? UITextView }.first {
            !$0.text.isEmpty && $0.text.contains("Response ") && $0.convert($0.bounds, to: before).intersects(viewport)
        }, "The fixture must expose an actual response before reconnecting; atFoot=\(atFoot), offset=\(before.contentOffset), size=\(before.contentSize)")
        let text = try #require(reading.text)
        let offset = reading.convert(reading.bounds, to: before).minY - viewport.minY
        first.continuation.finish(throwing: HelperError.closed)
        try await until { services.opens.count == 3 && store.timeline.epoch == "restarted" && store.phase == .live }
        try await Task.sleep(for: .milliseconds(500))
        host.view.layoutIfNeeded()
        let after = try mainScroll(host.view)
        let next = all(after).compactMap { $0 as? UITextView }.first { $0.text == text }
        let frame = next.map { $0.convert($0.bounds, to: after) } ?? .null
        let finalViewport = visible(after)
        #expect(frame.intersects(finalViewport), "Previously read response must remain on screen after reconnect; atFoot=\(atFoot)")
        #expect(abs(frame.minY - finalViewport.minY - offset) < 8, "Keep the position within a tall reply, not merely its row")
    }

    @Test(arguments: [false, true])
    func aWorkingStatusChangeDoesNotCauseAFarJump(atFoot: Bool) async throws {
        let services = FakeHarnessServices(descriptor: ChatTests.claude)
        let initial = snapshot(epoch: "initial")
        let stream = AsyncThrowingStream<HarnessFeed, any Error>.makeStream()
        services.feeds = [.init(stream: stream.stream)]
        stream.continuation.yield(.opened(initial))
        let store = HarnessSessionStore(subject: ChatTests.subject, services: services)
        store.launch()
        defer { store.stop(); stream.continuation.finish() }
        try await until { store.phase == .live }
        let scene = try #require(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first)
        let previous = scene.keyWindow
        let host = UIHostingController(rootView: ChatView(store: store))
        let window = UIWindow(windowScene: scene)
        window.frame = CGRect(x: 0, y: 0, width: 800, height: 420)
        window.rootViewController = host
        window.makeKeyAndVisible()
        defer { window.isHidden = true; window.rootViewController = nil; previous?.makeKeyAndVisible() }
        host.view.frame = window.bounds
        host.view.layoutIfNeeded()
        try await Task.sleep(for: .milliseconds(500))
        if !atFoot {
            store.chatScroll.go(.row("reply-5-14"), reduce: true)
            try await Task.sleep(for: .milliseconds(500))
        }
        let scroll = try mainScroll(host.view)
        let viewport = visible(scroll)
        let reading = try #require(all(scroll).compactMap { $0 as? UITextView }.last {
            $0.text.contains("Response ") && $0.convert($0.bounds, to: scroll).intersects(viewport)
        })
        let text = try #require(reading.text)
        let offset = reading.convert(reading.bounds, to: scroll).minY - viewport.minY
        let originalRows = store.rows
        var largestShift: CGFloat = 0
        var trace = ["start foot=\(atFoot), y=\(scroll.contentOffset.y), h=\(scroll.contentSize.height), row=\(text.prefix(30)), offset=\(offset)"]
        for (index, working) in [true, false].enumerated() {
            var summary = initial.mission.summary
            summary.working = working
            stream.continuation.yield(.note(HarnessNotification(
                sessionId: ChatTests.subject.sessionID, kind: .mission, ts: WireDate(.now), epoch: initial.epoch,
                mission: HarnessDelta(rev: index + 2, ops: [.init(op: .summary, summary: summary)])
            )))
            try await until { store.working == working }
            #expect((store.liveWork != nil) == working)
            #expect(store.rows == originalRows, "A status note must not change the conversation")
            for sample in 0..<24 {
                try await Task.sleep(for: .milliseconds(16))
                let current = try mainScroll(host.view)
                let matches = all(current).compactMap { $0 as? UITextView }.filter { $0.text == text }
                let row = matches.min {
                    abs($0.convert($0.bounds, to: current).minY - visible(current).minY - offset)
                    < abs($1.convert($1.bounds, to: current).minY - visible(current).minY - offset)
                }
                let frame = row.map { $0.convert($0.bounds, to: current) } ?? .null
                let shift = abs(frame.minY - visible(current).minY - offset)
                if sample == 0 || sample == 23 || shift > 64 && trace.count < 12 {
                    let rows = all(current).compactMap { $0 as? UITextView }.filter {
                        $0.convert($0.bounds, to: current).intersects(visible(current))
                    }.map { String($0.text.prefix(24)) }
                    trace.append("work=\(working), sample=\(sample), y=\(current.contentOffset.y), h=\(current.contentSize.height), rowY=\(frame.minY), matches=\(matches.count), shift=\(shift), visible=\(rows)")
                }
                largestShift = max(largestShift, shift)
            }
        }
        // One footer line may take room at the foot; no multi-screen offset change is allowed.
        #expect(largestShift <= 64, "Working status moved the reader by \(largestShift) points: \(trace.joined(separator: "; "))")
    }

    /// Matches the observed shape: six prompts, tool-heavy turns, and tall markdown replies.
    private func snapshot(epoch: String) -> HarnessOpenResult {
        var items: [HarnessItem] = []
        func append(_ body: HarnessItem.Body, id: String, turn: String) {
            items.append(.init(id: id, seq: items.count + 1, turnId: turn, at: HarnessSamples.at, body: body))
        }
        for turn in 0..<6 {
            let id = "turn-\(turn)"
            append(.user(.init(text: "Prompt \(turn)", queued: false, source: "desk")), id: "user-\(turn)", turn: id)
            for tool in 0..<43 {
                let call = "tool-\(turn)-\(tool)"
                append(.tool(.init(callId: call, name: "read", title: "Read a file", category: .read,
                                   state: .done, startedAt: HarnessSamples.at, endedAt: HarnessSamples.at)), id: call, turn: id)
            }
            for reply in 0..<16 {
                let count = reply.isMultiple(of: 4) ? 40 : 3
                let text = (0..<count).map { "Response \(turn).\(reply), paragraph \($0). This detailed explanation keeps its complete words and reading position across a reconnect." }.joined(separator: "\n\n")
                append(.assistant(.init(text: text, partial: false)), id: "reply-\(turn)-\(reply)", turn: id)
            }
            append(.turn(.init(state: .done, startedAt: HarnessSamples.at, endedAt: HarnessSamples.at, steps: 43, filesChanged: 0)), id: id, turn: id)
        }
        return .init(epoch: epoch, cursor: items.count, items: items, more: false,
                     mission: .init(rev: 1, summary: .init(), sections: []))
    }

    private func all(_ view: UIView) -> [UIView] { [view] + view.subviews.flatMap(all) }
    private func mainScroll(_ view: UIView) throws -> UIScrollView {
        try #require(all(view).compactMap { $0 as? UIScrollView }.first { !($0 is UITextView) && $0.bounds.height > 200 })
    }
    private func visible(_ scroll: UIScrollView) -> CGRect {
        CGRect(x: scroll.bounds.minX, y: scroll.bounds.minY + scroll.adjustedContentInset.top,
               width: scroll.bounds.width, height: scroll.bounds.height - scroll.adjustedContentInset.top - scroll.adjustedContentInset.bottom)
    }
    private func until(_ condition: () -> Bool) async throws {
        let deadline = ContinuousClock.now + .seconds(5)
        while !condition(), ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(10)) }
        try #require(condition())
    }
}
