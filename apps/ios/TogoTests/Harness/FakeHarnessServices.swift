import Foundation
import TogoKit
@testable import Togo

@MainActor final class FakeHarnessServices: HarnessServices {
    var terminalFactory: (@MainActor () async throws -> any TerminalNativeConnection)?
    func terminalConnection(_ s: HarnessSubject) async throws -> any TerminalNativeConnection {
        guard let terminalFactory else { throw HarnessUnavailable() }
        return try await terminalFactory()
    }

    var nativeConnection: (any SurfaceConnection)?
    func surfaceConnection(_ s: HarnessSubject) async throws -> any SurfaceConnection {
        guard let nativeConnection else { throw HarnessUnavailable() }
        return nativeConnection
    }

    var sentImages: [[String]] = []
    var uploads: [String] = []

    func upload(_ s: HarnessSubject, name: String, data: Data) async throws -> String {
        uploads.append(name)
        return "/u/\(name)"
    }

    func image(_ s: HarnessSubject, path: String) async throws -> Data { Data() }

    struct FeedScript {
        enum End { case hold, fail, refuse }
        var stream: AsyncThrowingStream<HarnessFeed, any Error>

        init(stream: AsyncThrowingStream<HarnessFeed, any Error>) {
            self.stream = stream
        }

        init(elements: [HarnessFeed], end: End) {
            stream = AsyncThrowingStream { continuation in
                for element in elements { continuation.yield(element) }
                switch end {
                case .hold: break
                case .fail: continuation.finish(throwing: HelperError.closed)
                case .refuse: continuation.finish(throwing: HelperError.refused("fallback"))
                }
            }
        }
    }

    var descriptor: HarnessDescriptor
    var feeds: [FeedScript] = []
    var sendSequenceBase = 100
    var sendFailures = 0
    var beforeSendResult: (@MainActor () async throws -> Void)?
    var actionError: (any Error)?
    var interruptResult = HarnessActionResult(result: .applied)
    var actionResult = HarnessActionResult(result: .applied)
    var beforeActionResult: (@MainActor () async -> Void)?
    private(set) var actions: [String] = []
    private(set) var describes = 0
    private(set) var opens: [Int] = []
    private(set) var sent: [(text: String, mode: HarnessSendParams.Mode)] = []

    init(descriptor: HarnessDescriptor) {
        self.descriptor = descriptor
    }

    func describe(_ s: HarnessSubject) async throws -> HarnessDescriptor {
        describes += 1
        return descriptor
    }

    func feed(_ s: HarnessSubject, after: Int) -> AsyncThrowingStream<HarnessFeed, any Error> {
        opens.append(after)
        guard !feeds.isEmpty else { return AsyncThrowingStream { _ in } }
        return feeds.removeFirst().stream
    }

    /// Answers `history`; the default is an empty last page.
    var historyPage: @MainActor () async -> HarnessPage = { HarnessPage(items: [], more: false) }

    func history(_ s: HarnessSubject, before: Int) async throws -> HarnessPage { await historyPage() }

    func screen(_ s: HarnessSubject, id: String, query: [String: String]?) async throws -> HarnessScreen {
        throw HarnessUnavailable()
    }

    func act(_ s: HarnessSubject, action: String, target: String?, args: [String: JSONValue]?) async throws -> HarnessActionResult {
        actions.append(action)
        if let actionError { throw actionError }
        await beforeActionResult?()
        return actionResult
    }

    func send(_ s: HarnessSubject, text: String, mode: HarnessSendParams.Mode, images: [String]) async throws -> HarnessItem {
        if sendFailures > 0 {
            sendFailures -= 1
            throw HelperError.closed
        }
        sent.append((text, mode))
        sentImages.append(images)
        try await beforeSendResult?()
        let id = "sent-\(sent.count)"
        return HarnessItem(id: id, seq: sendSequenceBase + sent.count, turnId: id, at: WireDate(.now),
                           body: .user(.init(text: text, queued: false, source: "phone")))
    }

    func interrupt(_ s: HarnessSubject) async throws -> HarnessActionResult { interruptResult }

    func complete(_ s: HarnessSubject, kind: HarnessCompleteParams.Kind, query: String) async throws -> [HarnessCompletion] {
        []
    }

    func locate(_ s: HarnessSubject, anchor: HarnessAnchor) async throws -> HarnessLocation {
        HarnessLocation(found: true, linesFromBottom: Int(26 - anchor.line), needle: anchor.needle)
    }
}
