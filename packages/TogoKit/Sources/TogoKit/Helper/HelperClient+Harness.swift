import Foundation

// harness.v1: native conversation, mission control, screens and actions for sessions an
// adapter serves.
//
// Notifications: after `harnessOpen` the helper sends `harness` notifications for that
// session on this connection, in `harnessNotifications`. That stream is single consumer and
// keeps the newest 512 when the consumer falls behind, so a gap in a session's item seq or
// mission rev means the consumer should open the session again. The open result is written
// before any notification for the session, but the caller may see notifications first; a
// consumer buffers them until the open result is applied and drops items with seq at or
// below its cursor and deltas at or below its rev. A notification that does not decode is
// dropped, and the gap it leaves has the same remedy.
extension HelperClient {
    static func makeHarnessStream() -> (AsyncStream<HarnessNotification>, AsyncStream<HarnessNotification>.Continuation) {
        AsyncStream<HarnessNotification>.makeStream(bufferingPolicy: .bufferingNewest(512))
    }

    nonisolated static func harnessNotification(_ frame: Data) -> HarnessNotification? {
        try? JSONDecoder().decode(HarnessFrame.self, from: frame).params
    }

    /// Which adapter serves the session and what it can do. A session no adapter serves gets
    /// a descriptor with a nil adapter: show the terminal only.
    public func harnessDescribe(id: String) async throws -> HarnessDescriptor {
        try await readyCall(.harnessDescribe, SessionIDParams(id: id), needs: Capability.harness)
    }

    /// Items after `after` and the mission snapshot; notifications for the session follow.
    /// Throws `HelperError` refused with reason no_adapter for a session with no adapter.
    public func harnessOpen(id: String, after: Int = 0, limit: Int? = nil) async throws -> HarnessOpenResult {
        try await readyCall(.harnessOpen, HarnessOpenParams(id: id, after: after, limit: limit), needs: Capability.harness)
    }

    /// Stops notifications for the session on this connection.
    public func harnessClose(id: String) async throws {
        let _: EmptyResult = try await readyCall(.harnessClose, SessionIDParams(id: id), needs: Capability.harness)
    }

    public func harnessHistory(id: String, before: Int, limit: Int? = nil) async throws -> HarnessPage {
        try await readyCall(.harnessHistory, HarnessHistoryParams(id: id, before: before, limit: limit), needs: Capability.harness)
    }

    /// Reads a screen. A source that cannot be served throws refused with reason fallback: show
    /// the terminal.
    public func harnessScreen(id: String, screen: String, query: [String: String]? = nil) async throws -> HarnessScreen {
        try await readyCall(.harnessScreen, HarnessScreenParams(id: id, screen: screen, query: query), needs: Capability.harness)
    }

    public func harnessAction(_ params: HarnessActionParams) async throws -> HarnessActionResult {
        try await readyCall(.harnessAction, params, needs: Capability.harness)
    }

    /// Sends a prompt. With `.queue` (the default) it waits for the turn to end; `.now` throws
    /// conflict reason busy while the agent works.
    public func harnessSend(
        id: String, text: String, mode: HarnessSendParams.Mode = .queue, images: [String] = []
    ) async throws -> HarnessItem {
        let result: HarnessSendResult = try await readyCall(
            .harnessSend, HarnessSendParams(id: id, text: text, mode: mode, images: images), needs: Capability.harness)
        return result.item
    }

    /// Reads a picture the chat shows, from the session's uploads or its folder. Larger
    /// pictures arrive shrunk to fit.
    public func image(_ params: FsImageParams) async throws -> FsImageResult {
        try await readyCall(.fsImage, params, needs: Capability.image)
    }

    public func harnessInterrupt(id: String) async throws -> HarnessActionResult {
        try await readyCall(.harnessInterrupt, SessionIDParams(id: id), needs: Capability.harness)
    }

    public func harnessComplete(id: String, kind: HarnessCompleteParams.Kind, query: String, limit: Int? = nil) async throws -> [HarnessCompletion] {
        let result: HarnessCompleteResult = try await readyCall(
            .harnessComplete, HarnessCompleteParams(id: id, kind: kind, query: query, limit: limit), needs: Capability.harness)
        return result.items
    }

    /// Where an item's anchor sits in the pane now, for Show in terminal.
    public func harnessLocate(id: String, anchor: HarnessAnchor) async throws -> HarnessLocation {
        try await readyCall(.harnessLocate, HarnessLocateParams(id: id, anchor: anchor), needs: Capability.harness)
    }
}

private struct HarnessFrame: Decodable {
    var params: HarnessNotification
}
