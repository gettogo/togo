import Foundation
import TogoKit

/// The session a harness screen talks about.
struct HarnessSubject: Hashable, Sendable {
    var hostID: HostID
    var sessionID: String
    /// The machine helper's hello capabilities.
    var capabilities: Set<String>

    /// The helper serves harness.v1; without it every session shows the terminal only.
    var available: Bool { capabilities.contains(Capability.harness) }
}

/// One element of an open session's feed: the open result first, then its notifications.
enum HarnessFeed: Sendable {
    case opened(HarnessOpenResult)
    case note(HarnessNotification)
}

/// What the native harness layers ask of a machine. Kept apart from `AppServices` and
/// `ReviewServices` so the chat, mission control and primitives do not grow a shared protocol.
///
/// `feed` holds one long-lived helper connection per open session: it calls harness.open, yields
/// `.opened` and then every `harness` notification for the session, and calls harness.close when
/// the consumer cancels. It finishes (or throws) when the connection drops; the consumer opens
/// again with its cursor.
@MainActor protocol HarnessServices: AnyObject {
    func describe(_ s: HarnessSubject) async throws -> HarnessDescriptor
    func feed(_ s: HarnessSubject, after: Int) -> AsyncThrowingStream<HarnessFeed, any Error>
    func history(_ s: HarnessSubject, before: Int) async throws -> HarnessPage
    func screen(_ s: HarnessSubject, id: String, query: [String: String]?) async throws -> HarnessScreen
    func act(_ s: HarnessSubject, action: String, target: String?, args: [String: JSONValue]?) async throws -> HarnessActionResult
    func send(_ s: HarnessSubject, text: String, mode: HarnessSendParams.Mode, images: [String]) async throws -> HarnessItem
    /// Writes a picture for the session through `fs.upload`; the path on the machine.
    func upload(_ s: HarnessSubject, name: String, data: Data) async throws -> String
    /// A picture the chat shows, through `fs.image`.
    func image(_ s: HarnessSubject, path: String) async throws -> Data
    func interrupt(_ s: HarnessSubject) async throws -> HarnessActionResult
    func complete(_ s: HarnessSubject, kind: HarnessCompleteParams.Kind, query: String) async throws -> [HarnessCompletion]
    func locate(_ s: HarnessSubject, anchor: HarnessAnchor) async throws -> HarnessLocation
}

struct HarnessUnavailable: Error {}

/// Stands in until the app is given live services: every session shows the terminal only.
@MainActor final class NoHarnessServices: HarnessServices {
    func describe(_ s: HarnessSubject) async throws -> HarnessDescriptor { HarnessDescriptor() }
    func feed(_ s: HarnessSubject, after: Int) -> AsyncThrowingStream<HarnessFeed, any Error> {
        AsyncThrowingStream { $0.finish(throwing: HarnessUnavailable()) }
    }
    func history(_ s: HarnessSubject, before: Int) async throws -> HarnessPage { throw HarnessUnavailable() }
    func screen(_ s: HarnessSubject, id: String, query: [String: String]?) async throws -> HarnessScreen {
        throw HarnessUnavailable()
    }
    func act(_ s: HarnessSubject, action: String, target: String?, args: [String: JSONValue]?) async throws -> HarnessActionResult {
        throw HarnessUnavailable()
    }
    func send(_ s: HarnessSubject, text: String, mode: HarnessSendParams.Mode, images: [String]) async throws -> HarnessItem {
        throw HarnessUnavailable()
    }
    func upload(_ s: HarnessSubject, name: String, data: Data) async throws -> String { throw HarnessUnavailable() }
    func image(_ s: HarnessSubject, path: String) async throws -> Data { throw HarnessUnavailable() }
    func interrupt(_ s: HarnessSubject) async throws -> HarnessActionResult { throw HarnessUnavailable() }
    func complete(_ s: HarnessSubject, kind: HarnessCompleteParams.Kind, query: String) async throws -> [HarnessCompletion] {
        []
    }
    func locate(_ s: HarnessSubject, anchor: HarnessAnchor) async throws -> HarnessLocation { throw HarnessUnavailable() }
}

extension HarnessServices {
    /// A prompt without pictures.
    func send(_ s: HarnessSubject, text: String, mode: HarnessSendParams.Mode) async throws -> HarnessItem {
        try await send(s, text: text, mode: mode, images: [])
    }
}
