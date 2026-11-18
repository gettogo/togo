import Foundation
import Testing
import TogoTerminal
import UIKit

/// Real parser queries must not type into the agent behind native chat.
@MainActor struct TerminalReplyTests {
    @MainActor private final class Fixture {
        let engine = SwiftTermEngine(settings: .init(), theme: BuiltInThemes.all[0])
        let controller: TerminalViewController
        var bytes = Data()

        init() {
            controller = TerminalViewController(engine: engine)
            engine.onInput = { [weak self] in self?.bytes.append($0) }
        }

        func query() {
            engine.feed(Array("\u{1b}[c\u{1b}[>c\u{1b}[>q".utf8)[...])
        }

        func settle() async throws { try await Task.sleep(for: .milliseconds(200)) }
    }

    @Test func startupAndCoveredChatNeverAnswerDeviceQueries() async throws {
        let f = Fixture()
        f.query()
        try await f.settle()
        #expect(f.bytes.isEmpty) // Before the host's first isCovered assignment.
        f.controller.isCovered = true
        f.query()
        try await f.settle()
        #expect(f.bytes.isEmpty)
    }

    @Test func explicitUserInputStillWorksWhileChatCoversTheTerminal() async throws {
        let f = Fixture()
        f.controller.isCovered = true
        f.query()
        f.engine.paste("kept draft")
        try await f.settle()
        #expect(f.bytes == Data("kept draft".utf8))
    }

    @Test func firstUncoveredAssignmentRestoresRealTerminalReplies() async throws {
        let f = Fixture()
        f.controller.isCovered = false // Same as its initial value: must still open the gate.
        f.query()
        try await f.settle()
        let replies = String(decoding: f.bytes, as: UTF8.self)
        #expect(replies.contains("SwiftTerm"))
        #expect(replies.contains("\u{1b}[?"))
    }

    @Test func coveringBeforeQueuedDeliveryDropsRepliesWithoutReplayingThem() async throws {
        let f = Fixture()
        f.controller.isCovered = false
        f.query()
        f.controller.isCovered = true
        try await f.settle()
        #expect(f.bytes.isEmpty)
        f.controller.isCovered = false
        try await f.settle()
        #expect(f.bytes.isEmpty)
        f.query()
        try await f.settle()
        #expect(!f.bytes.isEmpty)
        f.bytes = Data()
        f.controller.viewDidDisappear(false)
        f.query()
        try await f.settle()
        #expect(f.bytes.isEmpty)
    }
}
