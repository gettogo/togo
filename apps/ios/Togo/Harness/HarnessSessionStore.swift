import Foundation
import Observation
import TogoKit

/// The three layers of a session. Chat is the default where the harness has an adapter; the
/// terminal is the source of truth, one tap away.
enum SessionLayer: String, Hashable, Sendable {
    case chat
    case terminal
}

/// Where "Show in terminal" should land: the terminal scrolls `linesFromBottom` up and marks
/// the line holding `needle`.
struct TerminalJump: Hashable, Sendable {
    var sessionID: String
    var linesFromBottom: Int
    var needle: String?
}

/// One session's native harness state: what the adapter can do, the conversation, mission
/// control and the composer's queue. One store per open tab; the chat, the pill, mission
/// control and the primitives read it.
@MainActor @Observable final class HarnessSessionStore {
    let subject: HarnessSubject
    @ObservationIgnored let services: any HarnessServices

    /// nil until described; a descriptor with a nil adapter means terminal only.
    private(set) var descriptor: HarnessDescriptor?
    private(set) var timeline = HarnessTimeline()
    private(set) var mission = MissionState()
    var layer: SessionLayer = .terminal
    /// A subagent whose own conversation is shown (its agent item id), or nil for the main one.
    var focusedAgent: String?
    /// Set by "Show in terminal"; the terminal layer consumes and clears it.
    var pendingJump: TerminalJump?
    /// Why the native layers are not showing, when they fell back to the terminal.
    private(set) var fallback: String?

    init(subject: HarnessSubject, services: any HarnessServices) {
        self.subject = subject
        self.services = services
    }

    var hasAdapter: Bool { descriptor?.adapter != nil }

    func has(_ capability: String) -> Bool { descriptor?.has(capability) ?? false }

    /// Describes the session, opens its feed and applies it until `stop` or cancellation,
    /// reopening with the cursor after a drop or a `.reopen`.
    func start() async {}

    func stop() {}

    func loadOlder() async {}

    func send(_ text: String, mode: HarnessSendParams.Mode = .queue) async throws {}

    func interrupt() async throws {}

    func run(action: String, target: String? = nil, args: [String: JSONValue]? = nil) async throws -> HarnessActionResult {
        try await services.act(subject, action: action, target: target, args: args)
    }

    func screen(_ id: String, query: [String: String]? = nil) async throws -> HarnessScreen {
        try await services.screen(subject, id: id, query: query)
    }

    /// Resolves an item's anchor and sets `pendingJump`, switching to the terminal layer.
    func showInTerminal(_ item: HarnessItem) async {}
}
