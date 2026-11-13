import Foundation
import SwiftUI
import Testing
import TogoKit
import UIKit
@testable import Togo

@MainActor struct ChatLoadingTests {
    @Test func cachedConversationShowsLoadingUntilItsReplacementArrives() async throws {
        let services = FakeHarnessServices(descriptor: ChatTests.claude)
        let first = AsyncThrowingStream<HarnessFeed, any Error>.makeStream()
        let second = AsyncThrowingStream<HarnessFeed, any Error>.makeStream()
        services.feeds = [.init(stream: first.stream), .init(stream: second.stream)]
        let opened = try HarnessFixtures.open()
        first.continuation.yield(.opened(opened))
        let store = HarnessSessionStore(subject: ChatTests.subject, services: services, retry: (.milliseconds(5), .milliseconds(20)))
        store.launch()
        defer { store.stop(); first.continuation.finish(); second.continuation.finish() }
        try await until { store.phase == .live }
        let cached = store.rows.map(\.id)
        #expect(!cached.isEmpty)
        first.continuation.yield(.note(HarnessNotification(
            sessionId: ChatTests.subject.sessionID, kind: .reset, ts: WireDate(.now), epoch: opened.epoch, reason: "history"
        )))
        try await until { services.opens.count == 2 }
        #expect(store.phase == .opening)
        #expect(store.rows.map(\.id) == cached)
        #expect(ChatLoadingState(store) == .reading)
        try recordLoading(store)
        var replacement = opened
        replacement.epoch = "reloaded"
        second.continuation.yield(.opened(replacement))
        try await until { store.timeline.epoch == "reloaded" && store.phase == .live }
        #expect(ChatLoadingState(store) == .none)
    }

    @Test func readingEarlierMessagesShowsProgressWithoutHidingCachedRows() async throws {
        let descriptor = HarnessDescriptor(adapter: ChatTests.claude.adapter, capabilities: [HarnessCapability.chat, HarnessCapability.history])
        let services = FakeHarnessServices(descriptor: descriptor)
        var opened = try HarnessFixtures.open()
        opened.more = true
        services.feeds = [.init(elements: [.opened(opened)], end: .hold)]
        let page = AsyncStream<Void>.makeStream()
        services.historyPage = {
            var events = page.stream.makeAsyncIterator()
            _ = await events.next()
            return HarnessPage(items: [], more: false)
        }
        let store = HarnessSessionStore(subject: ChatTests.subject, services: services)
        store.launch()
        defer { store.stop(); page.continuation.finish() }
        try await until { store.phase == .live }
        let cached = store.rows.map(\.id)
        let load = Task { await store.loadOlder() }
        try await until { store.loadingOlder }
        #expect(ChatLoadingState(store) == .reading)
        #expect(store.rows.map(\.id) == cached)
        page.continuation.yield(())
        await load.value
        #expect(ChatLoadingState(store) == .none)
    }

    @Test func loadingStatusFitsNarrowAndWidePanesAtAccessibilitySize() {
        for width: CGFloat in [340, 900] {
            let host = UIHostingController(rootView: ChatLoadingView(state: .reading, inline: true)
                .environment(\.dynamicTypeSize, .accessibility5))
            let size = host.sizeThatFits(in: CGSize(width: width, height: .greatestFiniteMagnitude))
            #expect(size.width <= width + 0.5)
            #expect(size.height > 0)
        }
    }

    private func recordLoading(_ store: HarnessSessionStore) throws {
        let scene = try #require(UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first)
        let previous = scene.keyWindow
        for scheme in [ColorScheme.light, .dark] {
            let host = UIHostingController(rootView: ChatView(store: store).environment(\.colorScheme, scheme))
            let window = UIWindow(windowScene: scene)
            window.frame = CGRect(x: 0, y: 0, width: 390, height: 700)
            window.rootViewController = host
            window.makeKeyAndVisible()
            host.view.frame = window.bounds
            host.view.layoutIfNeeded()
            let image = UIGraphicsImageRenderer(bounds: host.view.bounds).image { _ in
                host.view.drawHierarchy(in: host.view.bounds, afterScreenUpdates: true)
            }
            Attachment.record(image, named: "chat-loading-\(scheme == .light ? "light" : "dark")", as: .png)
            window.isHidden = true
        }
        previous?.makeKeyAndVisible()
    }

    private func until(_ condition: () -> Bool) async throws {
        let deadline = ContinuousClock.now + .seconds(3)
        while !condition(), ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(5)) }
        #expect(condition())
    }
}
