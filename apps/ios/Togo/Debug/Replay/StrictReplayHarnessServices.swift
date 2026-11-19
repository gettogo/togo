#if DEBUG
import Foundation
import TogoKit

/// Test-only adapter at the HarnessServices boundary. Exact fixture IDs are used in this
/// runner. Binding to UI launch identities and additional service seams is separate work.
@MainActor final class StrictReplayHarnessServices: HarnessServices {
    private typealias Feed = AsyncThrowingStream<HarnessFeed, any Error>
    private let run: StrictReplayRun
    private var feeds: [String: (token: UUID, continuation: Feed.Continuation)] = [:]

    init(jsonl: String) throws {
        run = try StrictReplayRun(tape: StrictReplayTape(jsonl: jsonl))
    }

    static func load(_ name: String) -> StrictReplayHarnessServices? {
        guard !name.isEmpty, let file = Bundle.main.url(forResource: name, withExtension: "jsonl"),
              let text = try? String(contentsOf: file, encoding: .utf8),
              let service = try? StrictReplayHarnessServices(jsonl: text), service.verdict == "pending" else { return nil }
        return service
    }

    var verdict: String { run.verdict }

    func verifyConsumed() throws { try run.verifyConsumed() }

    func describe(_ s: HarnessSubject) async throws -> HarnessDescriptor {
        try call("harness.describe", HarnessSessionParams(id: s.sessionID))
    }

    func feed(_ s: HarnessSubject, after: Int) -> AsyncThrowingStream<HarnessFeed, any Error> {
        let (stream, continuation) = Feed.makeStream()
        let id = s.sessionID
        do {
            guard feeds[id] == nil else { throw unavailable("harness.open") }
            let exchange = try run.take("harness.open", params: HarnessOpenParams(id: id, after: after))
            let events = exchange.beforeReply + exchange.afterReply
            // A refusal opens no feed, but events for another feed or an invalid event
            // still need to be processed before returning the recorded error.
            if case .error = exchange.reply { try deliver(events) }
            let opened: HarnessOpenResult = try run.value(exchange)
            let token = UUID()
            feeds[id] = (token, continuation)
            continuation.onTermination = { [weak self] _ in
                Task { @MainActor in
                    if self?.feeds[id]?.token == token { self?.feeds[id] = nil }
                }
            }
            // The service boundary yields the open result before any buffered notifications.
            continuation.yield(.opened(opened))
            try deliver(events)
        } catch {
            continuation.finish(throwing: error)
        }
        return stream
    }

    func history(_ s: HarnessSubject, before: Int) async throws -> HarnessPage {
        try call("harness.history", HarnessHistoryParams(id: s.sessionID, before: before))
    }

    func screen(_ s: HarnessSubject, id: String, query: [String: String]?) async throws -> HarnessScreen {
        try call("harness.screen", HarnessScreenParams(id: s.sessionID, screen: id, query: query))
    }

    func act(_ s: HarnessSubject, action: String, target: String?, args: [String: JSONValue]?) async throws -> HarnessActionResult {
        try call("harness.action", HarnessActionParams(id: s.sessionID, action: action, target: target, args: args))
    }

    func send(_ s: HarnessSubject, text: String, mode: HarnessSendParams.Mode, images: [String]) async throws -> HarnessItem {
        let result: HarnessSendResult = try call("harness.send", HarnessSendParams(id: s.sessionID, text: text, mode: mode, images: images))
        return result.item
    }

    func upload(_ s: HarnessSubject, name: String, data: Data) async throws -> String {
        let result: UploadResult = try call("fs.upload", UploadParams(sessionId: s.sessionID, name: name, data: data))
        return result.path
    }

    func image(_ s: HarnessSubject, path: String) async throws -> Data {
        let result: FsImageResult = try call("fs.image", FsImageParams(sessionId: s.sessionID, path: path))
        return result.data
    }

    func interrupt(_ s: HarnessSubject) async throws -> HarnessActionResult {
        try call("harness.interrupt", HarnessSessionParams(id: s.sessionID))
    }

    func complete(_ s: HarnessSubject, kind: HarnessCompleteParams.Kind, query: String) async throws -> [HarnessCompletion] {
        let params = HarnessCompleteParams(id: s.sessionID, kind: kind, query: query,
                                           limit: kind == .command ? LiveHarnessServices.commandLimit : nil)
        let result: HarnessCompleteResult = try call("harness.complete", params)
        return result.items
    }

    func locate(_ s: HarnessSubject, anchor: HarnessAnchor) async throws -> HarnessLocation {
        try call("harness.locate", HarnessLocateParams(id: s.sessionID, anchor: anchor))
    }

    func surfaceConnection(_ s: HarnessSubject) async throws -> any SurfaceConnection {
        throw unavailable("surface connection")
    }

    func terminalConnection(_ s: HarnessSubject) async throws -> any TerminalNativeConnection {
        throw unavailable("terminal connection")
    }

    private func unavailable(_ method: String) -> StrictReplayTape.Failure {
        run.record(.unexpectedCall(method: method, expectedLine: nil))
    }

    private func call<Value: Decodable>(_ method: String, _ params: some Encodable) throws -> Value {
        let exchange = try run.take(method, params: params)
        try deliver(exchange.beforeReply)
        let result: Result<Value, any Error> = Result { try run.value(exchange) }
        try deliver(exchange.afterReply)
        return try result.get()
    }

    private func deliver(_ events: [StrictReplayTape.Event]) throws {
        for event in events {
            switch event.method {
            case "harness":
                guard let data = try? JSONEncoder().encode(event.params),
                      let note = try? JSONDecoder().decode(HarnessNotification.self, from: data),
                      let feed = feeds[note.sessionId] else {
                    throw run.record(.invalidEvent(line: event.line))
                }
                feed.continuation.yield(.note(note))
            case "replay.eof":
                guard case .string(let id) = event.params["id"], let feed = feeds.removeValue(forKey: id) else {
                    throw run.record(.invalidEvent(line: event.line))
                }
                feed.continuation.finish()
            default:
                throw run.record(.invalidEvent(line: event.line))
            }
        }
    }
}
#endif
