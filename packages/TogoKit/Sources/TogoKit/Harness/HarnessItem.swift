import Foundation

/// One entry in a session's conversation (harness.v1). `id` is stable: when a tool finishes or
/// streamed text grows, the helper sends the item again with the same id and a new `seq`, and
/// the client replaces what it had.
public struct HarnessItem: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    /// Orders the session's stream; strictly increasing per epoch. An update takes a new seq.
    public var seq: Int
    /// Groups the items of one prompt and the agent's answer.
    public var turnId: String?
    /// The agent item whose own conversation holds this item; nil for the main conversation.
    public var parent: String?
    public var at: WireDate
    public var anchor: HarnessAnchor?
    public var body: Body

    public init(id: String, seq: Int, turnId: String? = nil, parent: String? = nil, at: WireDate,
                anchor: HarnessAnchor? = nil, body: Body) {
        self.id = id
        self.seq = seq
        self.turnId = turnId
        self.parent = parent
        self.at = at
        self.anchor = anchor
        self.body = body
    }

    public enum Body: Hashable, Sendable {
        case user(User)
        case assistant(Text)
        case thinking(Text)
        case tool(Tool)
        case turn(Turn)
        case approval(Approval)
        case question(Question)
        case plan(Plan)
        case agent(Agent)
        case background(Background)
        case notice(Notice)
        case shell(Shell)
        /// A kind this client does not know, from a newer helper. Skipped when drawn.
        case unknown(kind: String)

        public var kind: String {
            switch self {
            case .user: "user"
            case .assistant: "assistant"
            case .thinking: "thinking"
            case .tool: "tool"
            case .turn: "turn"
            case .approval: "approval"
            case .question: "question"
            case .plan: "plan"
            case .agent: "agent"
            case .background: "background"
            case .notice: "notice"
            case .shell: "shell"
            case .unknown(let kind): kind
            }
        }
    }

    public struct User: Codable, Hashable, Sendable {
        public var text: String
        /// True while the prompt waits for the agent's turn to end.
        public var queued: Bool
        /// phone, desk or unknown.
        public var source: String
        /// True once the user took the queued prompt back before it was typed: not shown.
        public var withdrawn: Bool
        /// Pictures the prompt carried.
        public var images: [HarnessImage]

        public init(text: String, queued: Bool, source: String, withdrawn: Bool = false, images: [HarnessImage] = []) {
            self.text = text
            self.queued = queued
            self.source = source
            self.withdrawn = withdrawn
            self.images = images
        }

        private enum CodingKeys: String, CodingKey { case text, queued, source, withdrawn, images }

        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            text = try c.decode(String.self, forKey: .text)
            queued = try c.decode(Bool.self, forKey: .queued)
            source = try c.decode(String.self, forKey: .source)
            withdrawn = try c.decodeIfPresent(Bool.self, forKey: .withdrawn) ?? false
            images = try c.decodeIfPresent([HarnessImage].self, forKey: .images) ?? []
        }

        public func encode(to encoder: any Encoder) throws {
            var c = encoder.container(keyedBy: CodingKeys.self)
            try c.encode(text, forKey: .text)
            try c.encode(queued, forKey: .queued)
            try c.encode(source, forKey: .source)
            if withdrawn { try c.encode(true, forKey: .withdrawn) }
            if !images.isEmpty { try c.encode(images, forKey: .images) }
        }
    }

    /// Assistant or thinking text, as markdown.
    public struct Text: Codable, Hashable, Sendable {
        public var text: String
        public var partial: Bool

        public init(text: String, partial: Bool) {
            self.text = text
            self.partial = partial
        }
    }

    public struct ToolCategory: OpenString {
        public var rawValue: String
        public init(rawValue: String) { self.rawValue = rawValue }
        public static let read: Self = "read"
        public static let edit: Self = "edit"
        public static let write: Self = "write"
        public static let search: Self = "search"
        public static let shell: Self = "shell"
        public static let web: Self = "web"
        public static let agent: Self = "agent"
        public static let mcp: Self = "mcp"
        public static let other: Self = "other"
    }

    public struct Tool: Codable, Hashable, Sendable {
        public var callId: String
        public var name: String
        public var title: String
        public var category: ToolCategory
        public var subject: Subject?
        public var state: HarnessState
        public var startedAt: WireDate
        public var endedAt: WireDate?
        /// Tail of the tool's output, at most 4096 bytes, secrets removed by the helper.
        public var output: String?
        public var exitCode: Int?
        public var files: [HarnessFileChange]?
        /// Pictures the tool read or produced.
        public var images: [HarnessImage]?

        public init(callId: String, name: String, title: String, category: ToolCategory, subject: Subject? = nil,
                    state: HarnessState, startedAt: WireDate, endedAt: WireDate? = nil, output: String? = nil,
                    exitCode: Int? = nil, files: [HarnessFileChange]? = nil, images: [HarnessImage]? = nil) {
            self.images = images
            self.callId = callId
            self.name = name
            self.title = title
            self.category = category
            self.subject = subject
            self.state = state
            self.startedAt = startedAt
            self.endedAt = endedAt
            self.output = output
            self.exitCode = exitCode
            self.files = files
        }

        private enum CodingKeys: String, CodingKey {
            case callId, name, title, category, subject, state, startedAt, endedAt, output, exitCode, files, images
        }

        public func encode(to encoder: any Encoder) throws {
            var c = encoder.container(keyedBy: CodingKeys.self)
            try c.encode(callId, forKey: .callId)
            try c.encode(name, forKey: .name)
            try c.encode(title, forKey: .title)
            try c.encode(category, forKey: .category)
            try c.encodeIfPresent(subject, forKey: .subject)
            try c.encode(state, forKey: .state)
            try c.encode(startedAt, forKey: .startedAt)
            try c.encode(endedAt, forKey: .endedAt)
            try c.encodeIfPresent(output, forKey: .output)
            try c.encodeIfPresent(exitCode, forKey: .exitCode)
            try c.encodeIfPresent(files, forKey: .files)
            try c.encodeIfPresent(images, forKey: .images)
        }
    }

    /// The folded "Worked 1m 12s, 6 steps, 2 files changed" line. The item's `turnId` is its id.
    public struct Turn: Codable, Hashable, Sendable {
        public var state: HarnessState
        public var startedAt: WireDate
        public var endedAt: WireDate?
        public var steps: Int
        public var filesChanged: Int

        public init(state: HarnessState, startedAt: WireDate, endedAt: WireDate?, steps: Int, filesChanged: Int) {
            self.state = state
            self.startedAt = startedAt
            self.endedAt = endedAt
            self.steps = steps
            self.filesChanged = filesChanged
        }

        private enum CodingKeys: String, CodingKey { case state, startedAt, endedAt, steps, filesChanged }

        public func encode(to encoder: any Encoder) throws {
            var c = encoder.container(keyedBy: CodingKeys.self)
            try c.encode(state, forKey: .state)
            try c.encode(startedAt, forKey: .startedAt)
            try c.encode(endedAt, forKey: .endedAt)
            try c.encode(steps, forKey: .steps)
            try c.encode(filesChanged, forKey: .filesChanged)
        }
    }

    /// Points at a pending approval (approvals.v1); the chat shows it inline and answers it
    /// with `approval.answer`, the same path as the prompt bar.
    public struct Approval: Codable, Hashable, Sendable {
        public var requestId: String
        /// phone, desk, expired or gone once closed.
        public var resolved: String?
        /// The question item that is this same prompt. The question answers it, so the chat does
        /// not draw this approval on its own.
        public var owner: String?

        public init(requestId: String, resolved: String? = nil, owner: String? = nil) {
            self.requestId = requestId
            self.resolved = resolved
            self.owner = owner
        }
    }

    /// Questions the agent asked; answered with the `answer` action.
    public struct Question: Codable, Hashable, Sendable {
        public var questions: [HarnessQuestion]
        /// Chosen option ids or text per question id, once answered.
        public var answered: [String: [String]]?
        /// False when the harness offers no way to answer from the phone. Nil (older helpers) means true.
        public var answerable: Bool?
        /// The pending approval that is this question at the terminal, once linked.
        public var requestId: String?

        /// Whether the phone can answer: `answerable` with its default.
        public var isAnswerable: Bool { answerable ?? true }

        public init(questions: [HarnessQuestion], answered: [String: [String]]? = nil, answerable: Bool? = nil,
                    requestId: String? = nil) {
            self.questions = questions
            self.answered = answered
            self.answerable = answerable
            self.requestId = requestId
        }
    }

    public struct Plan: Codable, Hashable, Sendable {
        public var done: Int
        public var total: Int
        public var current: String?

        public init(done: Int, total: Int, current: String? = nil) {
            self.done = done
            self.total = total
            self.current = current
        }
    }

    /// A subagent. Items whose `parent` is this item's id form its own conversation.
    public struct Agent: Codable, Hashable, Sendable {
        public var agentId: String
        public var name: String
        public var role: String?
        public var state: HarnessState
        public var startedAt: WireDate
        public var endedAt: WireDate?
        public var summary: String?

        public init(agentId: String, name: String, role: String? = nil, state: HarnessState, startedAt: WireDate,
                    endedAt: WireDate? = nil, summary: String? = nil) {
            self.agentId = agentId
            self.name = name
            self.role = role
            self.state = state
            self.startedAt = startedAt
            self.endedAt = endedAt
            self.summary = summary
        }

        private enum CodingKeys: String, CodingKey { case agentId, name, role, state, startedAt, endedAt, summary }

        public func encode(to encoder: any Encoder) throws {
            var c = encoder.container(keyedBy: CodingKeys.self)
            try c.encode(agentId, forKey: .agentId)
            try c.encode(name, forKey: .name)
            try c.encodeIfPresent(role, forKey: .role)
            try c.encode(state, forKey: .state)
            try c.encode(startedAt, forKey: .startedAt)
            try c.encode(endedAt, forKey: .endedAt)
            try c.encodeIfPresent(summary, forKey: .summary)
        }
    }

    /// Long-running work outside the turn: a background shell, a dev server, a Codex exec or
    /// a togo job. `kind` is shell, server, exec or job.
    public struct Background: Codable, Hashable, Sendable {
        public var jobId: String
        public var kind: String
        public var title: String
        public var command: String?
        public var state: HarnessState
        public var startedAt: WireDate
        public var endedAt: WireDate?

        public init(jobId: String, kind: String, title: String, command: String? = nil, state: HarnessState,
                    startedAt: WireDate, endedAt: WireDate? = nil) {
            self.jobId = jobId
            self.kind = kind
            self.title = title
            self.command = command
            self.state = state
            self.startedAt = startedAt
            self.endedAt = endedAt
        }

        private enum CodingKeys: String, CodingKey { case jobId, kind, title, command, state, startedAt, endedAt }

        public func encode(to encoder: any Encoder) throws {
            var c = encoder.container(keyedBy: CodingKeys.self)
            try c.encode(jobId, forKey: .jobId)
            try c.encode(kind, forKey: .kind)
            try c.encode(title, forKey: .title)
            try c.encodeIfPresent(command, forKey: .command)
            try c.encode(state, forKey: .state)
            try c.encode(startedAt, forKey: .startedAt)
            try c.encode(endedAt, forKey: .endedAt)
        }
    }

    /// A quiet system line. `level` is info, warning or error; `code` is compacted, model, mode,
    /// rate_limit, error, resumed, interrupted, fallback or continued.
    public struct Notice: Codable, Hashable, Sendable {
        /// Opens a turn the agent started on its own, such as after a background task or
        /// subagent finished; the text names the trigger.
        public static let continued = "continued"

        public var level: String
        public var code: String
        public var text: String

        public init(level: String, code: String, text: String) {
            self.level = level
            self.code = code
            self.text = text
        }
    }

    /// One command block of a plain shell session, from OSC 133 prompt marks.
    public struct Shell: Codable, Hashable, Sendable {
        public var command: String
        public var cwd: String?
        public var state: HarnessState
        public var exitCode: Int?
        public var startedAt: WireDate
        public var endedAt: WireDate?
        /// Tail of what the command printed, plain text, once the block finished.
        public var output: String?

        public init(command: String, cwd: String? = nil, state: HarnessState, exitCode: Int?, startedAt: WireDate,
                    endedAt: WireDate?, output: String? = nil) {
            self.command = command
            self.cwd = cwd
            self.state = state
            self.exitCode = exitCode
            self.startedAt = startedAt
            self.endedAt = endedAt
            self.output = output
        }

        private enum CodingKeys: String, CodingKey { case command, cwd, state, exitCode, startedAt, endedAt, output }

        public func encode(to encoder: any Encoder) throws {
            var c = encoder.container(keyedBy: CodingKeys.self)
            try c.encode(command, forKey: .command)
            try c.encodeIfPresent(cwd, forKey: .cwd)
            try c.encode(state, forKey: .state)
            try c.encode(exitCode, forKey: .exitCode)
            try c.encode(startedAt, forKey: .startedAt)
            try c.encode(endedAt, forKey: .endedAt)
            try c.encodeIfPresent(output, forKey: .output)
        }
    }

    private enum CodingKeys: String, CodingKey {
        case id, seq, turnId, parent, kind, at, anchor
        case user, assistant, thinking, tool, turn, approval, question, plan, agent, background, notice, shell
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        seq = try c.decode(Int.self, forKey: .seq)
        turnId = try c.decodeIfPresent(String.self, forKey: .turnId)
        parent = try c.decodeIfPresent(String.self, forKey: .parent)
        at = try c.decode(WireDate.self, forKey: .at)
        anchor = try c.decodeIfPresent(HarnessAnchor.self, forKey: .anchor)
        let kind = try c.decode(String.self, forKey: .kind)
        switch kind {
        case "user": body = .user(try c.decode(User.self, forKey: .user))
        case "assistant": body = .assistant(try c.decode(Text.self, forKey: .assistant))
        case "thinking": body = .thinking(try c.decode(Text.self, forKey: .thinking))
        case "tool": body = .tool(try c.decode(Tool.self, forKey: .tool))
        case "turn": body = .turn(try c.decode(Turn.self, forKey: .turn))
        case "approval": body = .approval(try c.decode(Approval.self, forKey: .approval))
        case "question": body = .question(try c.decode(Question.self, forKey: .question))
        case "plan": body = .plan(try c.decode(Plan.self, forKey: .plan))
        case "agent": body = .agent(try c.decode(Agent.self, forKey: .agent))
        case "background": body = .background(try c.decode(Background.self, forKey: .background))
        case "notice": body = .notice(try c.decode(Notice.self, forKey: .notice))
        case "shell": body = .shell(try c.decode(Shell.self, forKey: .shell))
        default: body = .unknown(kind: kind)
        }
    }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(id, forKey: .id)
        try c.encode(seq, forKey: .seq)
        try c.encodeIfPresent(turnId, forKey: .turnId)
        try c.encodeIfPresent(parent, forKey: .parent)
        try c.encode(body.kind, forKey: .kind)
        try c.encode(at, forKey: .at)
        try c.encodeIfPresent(anchor, forKey: .anchor)
        switch body {
        case .user(let v): try c.encode(v, forKey: .user)
        case .assistant(let v): try c.encode(v, forKey: .assistant)
        case .thinking(let v): try c.encode(v, forKey: .thinking)
        case .tool(let v): try c.encode(v, forKey: .tool)
        case .turn(let v): try c.encode(v, forKey: .turn)
        case .approval(let v): try c.encode(v, forKey: .approval)
        case .question(let v): try c.encode(v, forKey: .question)
        case .plan(let v): try c.encode(v, forKey: .plan)
        case .agent(let v): try c.encode(v, forKey: .agent)
        case .background(let v): try c.encode(v, forKey: .background)
        case .notice(let v): try c.encode(v, forKey: .notice)
        case .shell(let v): try c.encode(v, forKey: .shell)
        case .unknown: break
        }
    }
}

/// A picture in the conversation: inline when small, else by path (fetched with `fs.image`),
/// or only described when the source kept neither.
public struct HarnessImage: Codable, Hashable, Sendable {
    /// Absolute path on the machine.
    public var path: String?
    public var mediaType: String?
    /// The decoded size, when known.
    public var bytes: Int?
    /// The picture itself, at most 48 KiB.
    public var data: Data?

    public init(path: String? = nil, mediaType: String? = nil, bytes: Int? = nil, data: Data? = nil) {
        self.path = path
        self.mediaType = mediaType
        self.bytes = bytes
        self.data = data
    }

    private enum CodingKeys: String, CodingKey { case path, mediaType, bytes, data }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.decodeIfPresent(String.self, forKey: .path)
        mediaType = try c.decodeIfPresent(String.self, forKey: .mediaType)
        bytes = try c.decodeIfPresent(Int.self, forKey: .bytes)
        data = try c.decodeIfPresent(String.self, forKey: .data).flatMap(Base64URL.decode)
    }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encodeIfPresent(path, forKey: .path)
        try c.encodeIfPresent(mediaType, forKey: .mediaType)
        try c.encodeIfPresent(bytes, forKey: .bytes)
        try c.encodeIfPresent(data.map(Base64URL.encode), forKey: .data)
    }
}
