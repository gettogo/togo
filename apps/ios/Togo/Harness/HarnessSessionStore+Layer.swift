import Foundation
import TogoKit

/// The layers of a session. Chat is the default where the harness has an adapter; the terminal
/// is the source of truth, one tap away. `pending` is never chosen, only shown: plain ink while
/// the app waits to learn whether a session that should have a chat has one, so the terminal
/// never flashes before the chat.
enum SessionLayer: String, Hashable, Sendable {
    case chat
    case terminal
    case pending
}

extension SessionLayer {
    /// A tab's layer before its harness store exists. Until the machines are read, nothing says
    /// which layer a session gets, so it waits (the loader) rather than showing the terminal
    /// and its bar first. Then a session that should have a chat, on a machine that serves
    /// harness.v1, waits for its store; anything else is the terminal.
    static func beforeStore(hostsLoaded: Bool, listed: Bool, serves: Bool, expectsChat: Bool) -> SessionLayer {
        if !listed { return hostsLoaded ? .terminal : .pending }
        return serves && expectsChat ? .pending : .terminal
    }
}

/// What takes the typing under a session, from the layer it shows (S4). The chat has its
/// composer; the terminal has the prompt bar or the compose bar; while it is not known yet which
/// one the session gets (`pending`), neither shows, only the loader, so the terminal's one-line
/// bar never flashes under a session that is about to show its chat.
enum SessionInput: Equatable {
    case composer
    case terminal
    case none

    init(_ layer: SessionLayer) {
        switch layer {
        case .chat: self = .composer
        case .terminal: self = .terminal
        case .pending: self = .none
        }
    }

    /// The app's prompt bar steps aside: the chat's composer types, or nothing does yet.
    var hidesPromptBar: Bool { self != .terminal }
}

/// Where "Show in terminal" should land: the terminal scrolls `linesFromBottom` up and marks
/// the line holding `needle`.
struct TerminalJump: Hashable, Sendable {
    var sessionID: String
    var linesFromBottom: Int
    var needle: String?
}

/// A scroll the chat makes because someone asked for it: "Go to request", the "Needs you"
/// capsule. Each request is new, so asking twice for the same card scrolls twice.
struct ChatScrollRequest: Equatable, Sendable {
    let id = UUID()
    var row: String
}

extension HarnessSessionStore {
    var hasAdapter: Bool { descriptor?.adapter != nil }

    /// The agent as the app names it everywhere ("OMP", "Claude Code"): the catalog's name for
    /// the adapter's agent, else the helper's own; "" until described.
    var agentName: String { Self.displayName(descriptor?.adapter) }

    static func displayName(_ adapter: HarnessDescriptor.Adapter?) -> String {
        guard let adapter else { return "" }
        return AgentCatalog.name(adapter.id) ?? adapter.name
    }

    func has(_ capability: String) -> Bool { descriptor?.has(capability) ?? false }

    var timeline: HarnessTimeline { feed.timeline }

    var mission: MissionState { feed.mission }

    /// A turn is running, so a prompt waits and Interrupt shows.
    var working: Bool { feed.mission.summary.working }

    /// The chat layer can show: an adapter serves the session with a conversation.
    var chatAvailable: Bool { hasAdapter && has(HarnessCapability.chat) && phase != .terminalOnly }

    /// Ink while a session expected to have a chat is still being described. Describe failing
    /// or saying there is no adapter ends it at once.
    var awaitsChat: Bool {
        descriptor == nil && expectsChat && subject.available && !describeFailed && !waitExpired && phase != .terminalOnly
    }

    /// The layer on screen: the chosen one where the chat can show, ink while it is expected,
    /// the terminal otherwise. Changes that come from availability are cuts, never fades.
    var shownLayer: SessionLayer {
        if chatAvailable { return layer == .terminal ? .terminal : .chat }
        return awaitsChat ? .pending : .terminal
    }

    /// The oldest needs card the user has not answered yet, in the main chat.
    var oldestOpenNeeds: String? {
        rows.first { row in
            guard case .needs(let item) = row else { return false }
            return WorkingLogic.hasOpenNeeds([item])
        }?.id
    }

    /// Shows the chat and scrolls it to a needs card: `id`, or the oldest one still open. Used
    /// by "Go to request" and by the "Needs you" capsule.
    func revealNeeds(id: String? = nil) {
        guard chatAvailable, let row = id ?? oldestOpenNeeds else { return }
        focusedAgent = nil
        layer = .chat
        scrollRequest = ChatScrollRequest(row: row)
    }

    // MARK: Actions

    func run(action: String, target: String? = nil, args: [String: JSONValue]? = nil) async throws -> HarnessActionResult {
        try await services.act(subject, action: action, target: target, args: args)
    }

    func screen(_ id: String, query: [String: String]? = nil) async throws -> HarnessScreen {
        try await services.screen(subject, id: id, query: query)
    }

    /// Answers a question item: option ids, or typed text, per question id.
    func answer(_ item: HarnessItem, answers: [String: [String]]) async throws -> HarnessActionResult {
        let value = JSONValue.object(answers.mapValues { .array($0.map(JSONValue.string)) })
        return try await run(action: "answer", target: item.id, args: ["answers": value])
    }

    /// Sends a question's answers and says how it went: a thrown error becomes a failed result
    /// carrying its message, so the card can say why.
    func answerReporting(_ item: HarnessItem, args: [String: JSONValue]) async -> HarnessActionResult {
        do {
            return try await run(action: "answer", target: item.id, args: args)
        } catch {
            return HarnessActionResult(result: .failed, message: ServiceErrors.message(error))
        }
    }

    /// Resolves an item's anchor and sets `pendingJump`, switching to the terminal layer.
    func showInTerminal(_ item: HarnessItem) async {
        await showInTerminal(anchor: item.anchor)
    }
}

// MARK: - Changes this phone makes (S7)

/// A line this phone adds to the chat after a change it made: "Effort high", "Restarted with
/// Sonnet". It sits after the row that was last when it was made.
struct SessionNote: Equatable, Sendable {
    var id: String
    var text: String
    var code: String
    /// The row it follows; nil puts it first.
    var after: String?
    var at: Date

    var item: HarnessItem {
        HarnessItem(id: id, seq: 0, at: WireDate(at), body: .notice(.init(level: "info", code: code, text: text)))
    }

    /// The feed's rows with each note after the row it follows; a note whose row is gone (a
    /// reopened feed) goes last, so it is never lost.
    static func merge(_ notes: [SessionNote], into rows: [ChatRow]) -> [ChatRow] {
        guard !notes.isEmpty else { return rows }
        var out: [ChatRow] = []
        let ids = Set(rows.map(\.id))
        let lead = notes.filter { $0.after == nil }
        out.append(contentsOf: lead.map { ChatRow.notice($0.item) })
        for row in rows {
            out.append(row)
            out.append(contentsOf: notes.filter { $0.after == row.id }.map { ChatRow.notice($0.item) })
        }
        let orphans = notes.filter { note in note.after.map { !ids.contains($0) } ?? false }
        out.append(contentsOf: orphans.map { ChatRow.notice($0.item) })
        return out
    }
}

/// The Undo a restart offers: back to the value it replaced.
struct SessionUndo: Identifiable {
    let id = UUID()
    /// "Back to Opus".
    var title: String
    var run: @MainActor () async -> Void
}

/// A line for the composer's field. Each request is new, so asking twice fills it twice.
struct DraftRequest: Equatable, Sendable {
    let id = UUID()
    var text: String
}

/// A change held until the turn ends ("After this reply").
struct DeferredAction {
    var action: String
    var target: String?
    var args: [String: JSONValue]?
    /// The chat's line once it ran: "Restarted with Sonnet".
    var note: String
}

extension HarnessSessionStore {
    /// Adds a line to the chat after what shows now, and offers `undo` in the composer.
    func note(_ text: String, code: String = "info", undo: SessionUndo? = nil) {
        notes.append(SessionNote(id: "note-\(UUID().uuidString)", text: text, code: code, after: rows.last?.id, at: .now))
        self.undo = undo
        refreshRows()
    }

    /// Puts `line` in the composer, focused, and shows the chat: "With instructions…" from a
    /// sheet or mission control.
    func compose(_ line: String) {
        showsMission = false
        if chatAvailable { layer = .chat }
        draftRequest = DraftRequest(text: line)
    }

    /// Holds a change for the end of the turn; it runs as soon as the agent is idle.
    func runAfterTurn(action: String, target: String? = nil, args: [String: JSONValue]? = nil, note: String) {
        deferred.append(DeferredAction(action: action, target: target, args: args, note: note))
        if !working { runDeferred() }
    }

    /// Runs what was held for the turn's end, oldest first, and notes each in the chat.
    func runDeferred() {
        let held = deferred
        deferred = []
        guard !held.isEmpty else { return }
        Task { [weak self] in
            for change in held {
                guard let self else { return }
                let result = try? await self.run(action: change.action, target: change.target, args: change.args)
                switch result?.result {
                case .failed?, nil: self.note(result?.message ?? PrimitiveCopy.failed, code: "error")
                default: self.note(change.note, code: "model")
                }
            }
        }
    }

    /// The composer line "With instructions…" leaves for `action`, from the command list.
    func instructionsLine(for action: String) -> String? {
        ActionGate.instructionsLine(for: action, in: commandList)
    }
}
