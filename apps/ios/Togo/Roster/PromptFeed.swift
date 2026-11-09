import Foundation
import TogoKit

/// Keeps the prompt bar in step with the store. What waits on the user comes first, oldest
/// first, one at a time with its place in the queue; with nothing waiting the bar is the focused
/// terminal's input, or the line that starts work.
enum PromptFeed {
    /// The requests as the bar shows them, oldest first. The store keeps cards in the order
    /// they arrived, across machines. An answered one stays, with its outcome, until the store
    /// lets it go a moment later.
    /// `inlineIn` is a tab whose chat shows its own requests inline; the bar leaves out only
    /// those the chat really draws (`chatShows`), so no request is ever invisible.
    @MainActor static func requests(in store: AppStore, inlineIn chat: TabID? = nil) -> [PromptRequest] {
        store.controlCenter.requests.filter { card in
            chat == nil || card.tabID != chat || !chatShows(card, in: store)
        }.map { card in
            let row = card.tabID.flatMap { id in
                store.controlCenter.machines.first { $0.id == id.hostID }?.sessions.first { $0.id == id.sessionID }
            }
            return request(
                card, row: row, machineName: store.machineName(for: card.hostID), failure: store.answerFailures[card.id]
            )
        }
    }

    /// The tab's chat draws this request: its adapter declares approvals, or its conversation
    /// holds the request's approval item or a question that is the same prompt.
    @MainActor static func chatShows(_ card: RequestCard, in store: AppStore) -> Bool {
        guard let id = card.tabID, let harness = store.harness(for: id) else { return false }
        if harness.has(HarnessCapability.needsApprovals) { return true }
        if harness.timeline.item("approval-" + card.id) != nil { return true }
        return harness.timeline.items.contains { item in
            if case .question(let question) = item.body { question.requestId == card.id } else { false }
        }
    }

    static func request(_ card: RequestCard, row: SessionRow?, machineName: String, failure: String? = nil) -> PromptRequest {
        let name = row.map { $0.name.isEmpty ? $0.agentName : $0.name } ?? card.agentName
        let choices = RequestChoices(card.options)
        return PromptRequest(
            id: card.id, sessionID: card.sessionID ?? "", bib: card.bib ?? row?.bib ?? "",
            sessionName: name, agentName: card.agentName, machineName: machineName,
            question: card.title, command: card.detail.flatMap { $0.isEmpty ? nil : $0 },
            allowsReply: card.allowText,
            // A question with no options that takes words is answered in words alone.
            // A sign-in step with nothing to choose is answered on its page or in its field.
            asksYesOrNo: !(card.options.isEmpty && (card.allowText || card.signIn != nil)),
            more: choices.more.map { PromptOption(id: $0.n, label: $0.label) },
            state: state(of: card, failure: failure),
            signIn: card.signIn, hostID: card.hostID
        )
    }

    static func state(of card: RequestCard, failure: String?) -> PromptRequestState {
        switch card.phase {
        case .open: failure.map { .failed($0) } ?? .open
        case .sending: .sending
        case .done(let outcome): .done(outcome.prompt)
        }
    }

    /// The mode the bar should be in. A reply being written stays, so a new arrival never takes
    /// the field away mid-sentence; if its request goes, or is answered elsewhere, the reply
    /// stays with its words until the user puts it away. While the terminal itself takes the
    /// typing the input line steps aside, so the terminal has the height and one place to type.
    /// While the focused tab shows its chat, the chat's composer is the one place to type, so
    /// the bar keeps only requests (from other sessions: the chat answers its own inline).
    static func mode(
        requests: [PromptRequest], current: PromptBarModel.Mode, focused: TabID?, composing: Bool, hasMachines: Bool,
        terminalTyping: Bool = false, focusedConnected: Bool = true, chatShown: Bool = false,
        composerTyping: Bool = false
    ) -> PromptBarModel.Mode {
        guard hasMachines else { return .hidden }
        if case .replying(let writing) = current {
            var kept = requests.first { $0.id == writing.id } ?? writing
            if case .done = kept.state { kept.state = .withdrawn }
            if !requests.contains(where: { $0.id == writing.id }) { kept.state = .withdrawn }
            return .replying(kept)
        }
        // The chat's composer holds the keyboard: another session's request waits off screen
        // until the keyboard goes, rather than sitting over the field and its menu.
        if composerTyping { return .hidden }
        if let first = requests.first {
            return .request(first, position: 1, count: requests.count)
        }
        // The compose bar is the terminal's input while it is open.
        if composing || chatShown { return .hidden }
        // Nothing typed reaches a terminal that is not connected, so the bar does not offer to.
        if let focused { return terminalTyping || !focusedConnected ? .hidden : .input(focused) }
        // No start line: every roster and empty detail has a New session button in its bar,
        // and ⌘T opens the sheet when the bar is not on its start line.
        return .hidden
    }

    /// Places to start from: the folders and agents already on the user's machines, each once,
    /// those with a favorite agent first and most recent first within each part, as the agent
    /// picker orders them. The bar narrows them by what the user types.
    static func suggestions(
        machines: [MachineSection], archived: [SessionRow], favorites: [String] = [], limit: Int = 24
    ) -> [StartSuggestion] {
        let names = Dictionary(machines.map { ($0.id, $0.name) }, uniquingKeysWith: { first, _ in first })
        let rows = (machines.flatMap(\.sessions) + archived).sorted {
            let a = favorites.contains($0.agent), b = favorites.contains($1.agent)
            return a != b ? a : $0.statusSince > $1.statusSince
        }
        var seen = Set<String>()
        var found: [StartSuggestion] = []
        for row in rows where !row.cwd.isEmpty {
            guard let machine = names[row.hostID] else { continue }
            let id = suggestionID(hostID: row.hostID, folder: row.cwd, agent: row.agent)
            guard seen.insert(id).inserted else { continue }
            found.append(StartSuggestion(id: id, agentName: row.agentName, folder: row.cwd, machineName: machine))
            if found.count == limit { break }
        }
        return found
    }

    static func suggestionID(hostID: HostID, folder: String, agent: String) -> String {
        "\(hostID.uuidString)\n\(folder)\n\(agent)"
    }

    /// The machine, folder and agent a suggestion starts with, read back from its id.
    static func target(of suggestion: StartSuggestion) -> (hostID: HostID, folder: String, agent: String)? {
        let parts = suggestion.id.components(separatedBy: "\n")
        guard parts.count == 3, let hostID = UUID(uuidString: parts[0]) else { return nil }
        return (hostID, parts[1], parts[2])
    }

    /// The decision an answer in the bar sends. Yes and No take the agent's own options when it
    /// offers them, so a numbered prompt gets the number it asked for.
    /// Nil for words to a request that takes none: those are never sent.
    static func decision(_ answer: PromptAnswer, for card: RequestCard) -> Decision? {
        let choices = RequestChoices(card.options)
        switch answer {
        case .yes: return choices.approve.map { .option($0.n) } ?? .approve
        case .no: return choices.deny.map { .option($0.n) } ?? .deny
        case .option(let n): return card.options.contains { $0.n == n } ? .option(n) : nil
        case .reply(let text): return card.allowText ? .text(text) : nil
        }
    }
}

/// The agent's options split the way the bar offers them: one approve, one deny and the rest.
struct RequestChoices: Equatable {
    var approve: ApprovalOption?
    var deny: ApprovalOption?
    var more: [ApprovalOption]

    init(_ options: [ApprovalOption]) {
        let approve = options.first { $0.role == .approve } ?? options.first { $0.role != .deny }
        let deny = options.first { $0.role == .deny }
        self.approve = approve
        self.deny = deny
        more = options.filter { $0 != approve && $0 != deny }
    }
}

extension RequestCard.Outcome {
    var prompt: PromptOutcome {
        switch self {
        case .approved: .approved
        case .denied: .denied
        case .answeredElsewhere: .answeredElsewhere
        case .expired: .expired
        }
    }
}
