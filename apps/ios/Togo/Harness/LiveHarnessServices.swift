import Foundation
import TogoKit

/// Harness calls against a machine's helper.
///
/// Requests go over the machine's shared helper connection. Each open session's feed gets a
/// helper of its own, started on a new exec channel of that connection's SSH session: the
/// shared channel is request-scoped, and `harness` notifications for a session go only to the
/// connection that opened it. None of these calls ask the user anything; the session's
/// terminal is already signed in, so a saved password or key is enough.
@MainActor final class LiveHarnessServices: HarnessServices {
    private nonisolated static let client = ClientInfo(name: "togo-ios", version: "0.1.0")

    private let channels: any HelperCalling
    private let records: (HostID) -> HostRecord?
    /// Starts a helper on a new exec channel of the machine's connection.
    private let openFeed: @Sendable (HostRecord) async throws -> any ExecChannel

    init(
        channels: any HelperCalling,
        records: @escaping (HostID) -> HostRecord?,
        openFeed: @escaping @Sendable (HostRecord) async throws -> any ExecChannel
    ) {
        self.channels = channels
        self.records = records
        self.openFeed = openFeed
    }

    /// The command that starts the machine's helper on an exec channel.
    nonisolated static func serveCommand(for record: HostRecord) -> String {
        record.helperPath.map { shellQuote([$0, "serve", "--stdio"]) } ?? HelperCommand.serveStdio
    }

    func describe(_ s: HarnessSubject) async throws -> HarnessDescriptor {
        try await call(s) { try await $0.harnessDescribe(id: s.sessionID) }
    }

    func feed(_ s: HarnessSubject, after: Int) -> AsyncThrowingStream<HarnessFeed, any Error> {
        let record = records(s.hostID)
        let openFeed = self.openFeed
        return AsyncThrowingStream { continuation in
            let task = Task {
                guard let record else {
                    continuation.finish(throwing: LiveServiceError.unknownMachine)
                    return
                }
                do {
                    let client = HelperClient(channel: try await openFeed(record))
                    do {
                        try await Self.run(client, session: s.sessionID, after: after, into: continuation)
                    } catch {
                        await client.close()
                        throw error
                    }
                    await client.close()
                } catch {
                    continuation.finish(throwing: error)
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    /// Says hello, opens the session and forwards its notifications until the connection ends
    /// or the consumer stops listening; then closes the session on the helper.
    private nonisolated static func run(
        _ client: HelperClient, session: String, after: Int,
        into continuation: AsyncThrowingStream<HarnessFeed, any Error>.Continuation
    ) async throws {
        _ = try await client.hello(client: Self.client)
        // Subscribe before opening, so nothing sent right after the result is missed.
        let notes = client.harnessNotifications
        let opened = try await client.harnessOpen(id: session, after: after)
        continuation.yield(.opened(opened))
        for await note in notes where note.sessionId == session {
            if Task.isCancelled { break }
            continuation.yield(.note(note))
        }
        if Task.isCancelled {
            // Best effort: the channel closes right after either way.
            try? await client.harnessClose(id: session)
            continuation.finish()
        } else {
            continuation.finish(throwing: HelperError.closed)
        }
    }

    func history(_ s: HarnessSubject, before: Int) async throws -> HarnessPage {
        try await call(s) { try await $0.harnessHistory(id: s.sessionID, before: before) }
    }

    func screen(_ s: HarnessSubject, id: String, query: [String: String]?) async throws -> HarnessScreen {
        try await call(s) { try await $0.harnessScreen(id: s.sessionID, screen: id, query: query) }
    }

    func act(_ s: HarnessSubject, action: String, target: String?, args: [String: JSONValue]?) async throws -> HarnessActionResult {
        let params = HarnessActionParams(id: s.sessionID, action: action, target: target, args: args)
        return try await call(s) { try await $0.harnessAction(params) }
    }

    func send(_ s: HarnessSubject, text: String, mode: HarnessSendParams.Mode, images: [String]) async throws -> HarnessItem {
        try await call(s) { try await $0.harnessSend(id: s.sessionID, text: text, mode: mode, images: images) }
    }

    func upload(_ s: HarnessSubject, name: String, data: Data) async throws -> String {
        let params = UploadParams(sessionId: s.sessionID, name: name, data: data)
        return try await call(s) { try await $0.upload(params).path }
    }

    func image(_ s: HarnessSubject, path: String) async throws -> Data {
        let params = FsImageParams(sessionId: s.sessionID, path: path)
        return try await call(s) { try await $0.image(params).data }
    }

    func interrupt(_ s: HarnessSubject) async throws -> HarnessActionResult {
        try await call(s) { try await $0.harnessInterrupt(id: s.sessionID) }
    }

    func complete(_ s: HarnessSubject, kind: HarnessCompleteParams.Kind, query: String) async throws -> [HarnessCompletion] {
        try await call(s) { try await $0.harnessComplete(id: s.sessionID, kind: kind, query: query) }
    }

    func locate(_ s: HarnessSubject, anchor: HarnessAnchor) async throws -> HarnessLocation {
        try await call(s) { try await $0.harnessLocate(id: s.sessionID, anchor: anchor) }
    }

    private func call<T: Sendable>(
        _ s: HarnessSubject, _ body: @escaping @Sendable (HelperClient) async throws -> T
    ) async throws -> T {
        guard let record = records(s.hostID) else { throw LiveServiceError.unknownMachine }
        return try await channels.call(record, prompter: nil, body)
    }
}
