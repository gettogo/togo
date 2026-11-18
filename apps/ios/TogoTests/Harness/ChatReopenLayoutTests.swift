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

/// The queued status and its action stay in one footer at compact and wide widths.
@MainActor @Suite(.serialized) struct QueuedPromptLayoutTests {
    @Test(arguments: [240.0, 688.0])
    func aQueuedStatusAndTakeBackShareOneFooter(width: Double) {
        let host = UIHostingController(rootView: PromptBubble(text: "", note: ChatCopy.queued,
            cancelTitle: ComposerCopy.cancelQueued, onCancel: {}))
        let size = host.sizeThatFits(in: CGSize(width: width, height: 1_000))
        #expect(size.width <= width)
        #expect(size.height >= 44 && size.height < 50, "Queued and Take Back belong to one accessible footer row")
    }

}
