import Foundation

/// How an agent's mark is drawn where TogoKit's catalog is not linked (the widget extension).
/// Kept in step with `AgentCatalog.entries`.
public enum LiveAgentMark: Equatable, Sendable {
    /// A symbol asset in the shared image catalog.
    case logo(String)
    /// One or two letters in SF Mono.
    case monogram(String)
    /// An SF Symbol.
    case symbol(String)

    private static let logos = [
        "claude": "agent.claude", "opencode": "agent.opencode", "copilot": "agent.copilot",
        "cursor": "agent.cursor", "cline": "agent.cline", "vibe": "agent.mistral", "omp": "agent.omp",
        "amp": "agent.amp", "goose": "agent.goose", "kilo": "agent.kilo", "kimi": "agent.kimi",
        "openhands": "agent.openhands", "pi": "agent.pi",
    ]
    private static let monograms = [
        "codex": "Cx", "aider": "Ad", "auggie": "Au", "crush": "Cr", "droid": "Dr", "kiro": "Kr",
    ]

    public static func of(_ agent: String) -> LiveAgentMark {
        let id = agent.lowercased()
        if let logo = logos[id] { return .logo(logo) }
        if let letters = monograms[id] { return .monogram(letters) }
        let clean = PromptText.clean(agent, maxLines: 1)
        if id.isEmpty || id == "shell" || id == "custom" || clean.isEmpty { return .symbol("terminal") }
        return .monogram(String(clean.prefix(2)).capitalized)
    }
}

/// What a Live Activity shows, worked out from its content state and sealed context, so the
/// lock screen and the Dynamic Island agree and the choices can be tested without drawing.
public struct LiveCard: Equatable, Sendable {
    public enum Phase: Equatable, Sendable {
        case needsYou, working, other
    }

    /// The thing a request is about. Commands, paths and URLs are machine text, drawn in mono.
    public enum Detail: Equatable, Sendable {
        case command(String), file(String), url(String), text(String)

        public var text: String {
            switch self {
            case .command(let s), .file(let s), .url(let s), .text(let s): s
            }
        }

        public var isMachineText: Bool {
            if case .text = self { return false }
            return true
        }
    }

    public var phase: Phase
    public var agentID: String
    public var agentName: String
    /// The session's title, else the agent's name.
    public var title: String
    /// The machine's name, only when the device is paired with more than one.
    public var machine: String?
    /// The question, shown with the detail unless it only names a tool.
    public var ask: String?
    public var detail: Detail?
    /// What a working session is doing now.
    public var step: String?
    /// Requests waiting besides the one shown.
    public var more: Int
    /// Buttons that may answer in place; nil when the app has to.
    public var answer: LiveContext.Answer?

    public init(
        agent: String, status: String, waiting: Int, requestId: String?, command: String?, context: LiveContext?,
        machine: String?, stale: Bool, showsCommand: Bool = true
    ) {
        let phase: Phase = stale ? .other : status == "needs_you" ? .needsYou : status == "working" ? .working : .other
        self.phase = phase
        agentID = agent
        agentName = AgentName.display(agent)
        let title = context?.title.map { PromptText.clean($0, maxLines: 1) } ?? ""
        self.title = title.isEmpty ? (agentName.isEmpty ? LiveCard.untitled : agentName) : title
        let machineName = machine.map { PromptText.clean($0, maxLines: 1) } ?? ""
        self.machine = machineName.isEmpty ? nil : machineName
        more = phase == .needsYou ? max(0, waiting - 1) : 0
        step = nil
        ask = nil
        detail = nil
        answer = nil
        switch phase {
        case .working:
            let step = context?.step.map { PromptText.clean($0, maxLines: 1) } ?? ""
            self.step = step.isEmpty ? nil : step
        case .needsYou:
            // The helper seals the command in the context for every device; the user's "show
            // command text" choice for the machine decides whether the lock screen shows it.
            let subject = context?.subject.flatMap { showsCommand || $0.kind != "command" ? $0 : nil }
            detail = Self.detail(subject) ?? (showsCommand ? command : nil).flatMap { raw in
                let clean = PromptText.clean(raw, maxLines: 2)
                return clean.isEmpty ? nil : .command(clean)
            }
            let ask = context?.ask.map { PromptText.clean($0, maxLines: detail == nil ? 3 : 1) } ?? ""
            // A request titled only with its tool's name says nothing the detail does not.
            if !ask.isEmpty, detail == nil || ask.contains(" ") {
                self.ask = ask
            }
            if requestId != nil { answer = context?.answer }
        case .other:
            break
        }
    }

    /// The title of a session with neither a name nor a known agent.
    public static let untitled = String(localized: "live.untitled", defaultValue: "Session")

    /// "+2 more" beside the request shown; nil when it is the only one.
    public var moreText: String? {
        more > 0 ? String(localized: "live.more", defaultValue: "+\(more) more") : nil
    }

    /// The subject as it reads at a glance: a URL as host and path, everything cleaned.
    static func detail(_ subject: PushSubject?) -> Detail? {
        guard let subject, subject.isValid else { return nil }
        switch subject.kind {
        case "command":
            let s = PromptText.clean(subject.text, maxLines: 2)
            return s.isEmpty ? nil : .command(s)
        case "file":
            let s = PromptText.clean(subject.text, maxLines: 2)
            return s.isEmpty ? nil : .file(s)
        case "url":
            var s = subject.text
            if let url = URLComponents(string: subject.text), let host = url.host, !host.isEmpty { s = host + url.path }
            s = PromptText.clean(s, maxLines: 1)
            return s.isEmpty ? nil : .url(s)
        default:
            let s = PromptText.clean(subject.text, maxLines: 3)
            return s.isEmpty ? nil : .text(s)
        }
    }
}
