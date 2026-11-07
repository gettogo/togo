import Foundation

// Events, approvals and mute. Field names follow protocol/v1/schema.json exactly; keys
// the schema requires but allows to be null encode as null rather than going missing.

// MARK: - Approvals

public struct ApprovalOption: Codable, Hashable, Sendable {
    public enum Role: WireEnum {
        case approve, approveAlways, deny, other
        case unknown(String)

        public static let known: [Role] = [.approve, .approveAlways, .deny, .other]

        public var rawValue: String {
            switch self {
            case .approve: "approve"
            case .approveAlways: "approve_always"
            case .deny: "deny"
            case .other: "other"
            case .unknown(let value): value
            }
        }
    }

    /// The number the agent's menu shows; `Decision.option(n)` picks it.
    public var n: Int
    public var label: String
    public var role: Role

    public init(n: Int, label: String, role: Role) {
        self.n = n
        self.label = label
        self.role = role
    }
}

/// The one thing an approval asks about, structured so it can be shown without screen text.
public struct Subject: Codable, Hashable, Sendable {
    public enum Kind: String, Codable, Sendable {
        case command, file, url, text
    }

    /// The most Unicode scalars `text` may hold. The helper counts the same unit (Go runes),
    /// not grapheme clusters or UTF-16 units.
    public static let maxTextScalars = 1024

    public var kind: Kind
    /// 1-1024 Unicode scalars; newline is the only control character (Unicode Cc) allowed, and
    /// bidirectional and zero-width characters are refused (see `isInvisible`).
    public var text: String

    public init(kind: Kind, text: String) {
        self.kind = kind
        self.text = text
    }

    private enum CodingKeys: String, CodingKey {
        case kind, text
    }

    /// Rejects text the schema does not allow, since subjects are shown on the lock screen,
    /// the Live Activity and the Watch. The error never quotes the text.
    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        kind = try container.decode(Kind.self, forKey: .kind)
        text = try container.decode(String.self, forKey: .text)
        let scalars = text.unicodeScalars
        let control = scalars.contains {
            ($0 != "\n" && $0.properties.generalCategory == .control) || Self.isInvisible($0)
        }
        if scalars.isEmpty || scalars.count > Self.maxTextScalars || control {
            throw DecodingError.dataCorruptedError(
                forKey: .text, in: container,
                debugDescription: "subject text must be 1-1024 scalars with no control or invisible characters")
        }
    }

    /// Bidirectional overrides, embeddings, isolates and marks, and zero-width characters,
    /// which can make a command read differently from the bytes that run.
    public static func isInvisible(_ scalar: Unicode.Scalar) -> Bool {
        switch scalar.value {
        case 0x202A...0x202E, 0x2066...0x2069, 0x200B...0x200F, 0x061C, 0x2060, 0xFEFF: true
        default: false
        }
    }
}

public struct Approval: Codable, Hashable, Sendable, Identifiable {
    public enum Source: WireEnum {
        case hook, screen, ask
        case unknown(String)

        public static let known: [Source] = [.hook, .screen, .ask]

        public var rawValue: String {
            switch self {
            case .hook: "hook"
            case .screen: "screen"
            case .ask: "ask"
            case .unknown(let value): value
            }
        }
    }

    public var id: String { requestId }
    public var requestId: String
    public var sessionId: String?
    public var bib: String?
    public var agent: String
    public var source: Source
    public var title: String
    public var detail: String?
    /// Nil from helpers that predate it, when none was found, or when its kind is one this
    /// version does not know; `detail` still describes the request then.
    public var subject: Subject?
    public var snippet: String
    public var options: [ApprovalOption]
    public var allowText: Bool
    /// False while the helper cannot deliver an answer from the phone (a hook request with no
    /// options and no screen fingerprint yet): show it as waiting in the terminal. Absent means true.
    public var answerable: Bool
    public var createdAt: Date
    public var expiresAt: Date?

    public init(
        requestId: String, sessionId: String?, bib: String?, agent: String, source: Source,
        title: String, detail: String?, snippet: String, options: [ApprovalOption],
        allowText: Bool, createdAt: Date, expiresAt: Date?, subject: Subject? = nil, answerable: Bool = true
    ) {
        self.requestId = requestId
        self.sessionId = sessionId
        self.bib = bib
        self.agent = agent
        self.source = source
        self.title = title
        self.detail = detail
        self.subject = subject
        self.snippet = snippet
        self.options = options
        self.allowText = allowText
        self.answerable = answerable
        self.createdAt = createdAt
        self.expiresAt = expiresAt
    }

    private enum CodingKeys: String, CodingKey {
        case requestId, sessionId, bib, agent, source, title, detail, subject, snippet, options, allowText
        case answerable, createdAt, expiresAt, state
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        requestId = try container.decode(String.self, forKey: .requestId)
        sessionId = try container.decodeIfPresent(String.self, forKey: .sessionId)
        bib = try container.decodeIfPresent(String.self, forKey: .bib)
        agent = try container.decode(String.self, forKey: .agent)
        source = try container.decode(Source.self, forKey: .source)
        title = try container.decode(String.self, forKey: .title)
        detail = try container.decodeIfPresent(String.self, forKey: .detail)
        // A subject this version cannot read must not drop the whole approval.
        subject = (try? container.decodeIfPresent(Subject.self, forKey: .subject)) ?? nil
        snippet = try container.decode(String.self, forKey: .snippet)
        options = try container.decode([ApprovalOption].self, forKey: .options)
        allowText = try container.decode(Bool.self, forKey: .allowText)
        answerable = try container.decodeIfPresent(Bool.self, forKey: .answerable) ?? true
        createdAt = try container.decodeTimestamp(forKey: .createdAt)
        expiresAt = try container.decodeTimestampIfPresent(forKey: .expiresAt)
    }

    // The helper lists and sends only pending approvals, so state is always "pending".
    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(requestId, forKey: .requestId)
        try container.encode(sessionId, forKey: .sessionId)
        try container.encode(bib, forKey: .bib)
        try container.encode(agent, forKey: .agent)
        try container.encode(source, forKey: .source)
        try container.encode(title, forKey: .title)
        try container.encode(detail, forKey: .detail)
        try container.encodeIfPresent(subject, forKey: .subject)
        try container.encode(snippet, forKey: .snippet)
        try container.encode(options, forKey: .options)
        try container.encode(allowText, forKey: .allowText)
        if !answerable { try container.encode(false, forKey: .answerable) }
        try container.encodeTimestamp(createdAt, forKey: .createdAt)
        try container.encodeTimestamp(expiresAt, forKey: .expiresAt)
        try container.encode("pending", forKey: .state)
    }
}

/// An answer to an approval. `approve` and `deny` let the helper pick the matching
/// option; a hook prompt whose options all have role `other` needs `.option(n)`.
public enum Decision: Hashable, Sendable {
    case approve, deny, option(Int), text(String)

    public var wire: String {
        switch self {
        case .approve: "approve"
        case .deny: "deny"
        case .option(let n): "option:\(n)"
        case .text(let text): "text:\(text)"
        }
    }
}

public struct ApprovalListParams: Codable, Hashable, Sendable {
    public var sessionId: String?

    public init(sessionId: String? = nil) {
        self.sessionId = sessionId
    }
}

public struct ApprovalList: Codable, Hashable, Sendable {
    public var approvals: [Approval]

    public init(approvals: [Approval]) {
        self.approvals = approvals
    }
}

public struct ApprovalAnswerParams: Codable, Hashable, Sendable {
    public var requestId: String
    public var decision: String

    public init(requestId: String, decision: Decision) {
        self.requestId = requestId
        self.decision = decision.wire
    }
}

public struct ApprovalAnswerResult: Codable, Hashable, Sendable {
    public var state: String

    public init(state: String) {
        self.state = state
    }
}

public struct SetMuteParams: Codable, Hashable, Sendable {
    public var id: String
    public var muted: Bool

    public init(id: String, muted: Bool) {
        self.id = id
        self.muted = muted
    }
}

// MARK: - Jobs

public struct Job: Codable, Hashable, Sendable {
    public enum Status: WireEnum {
        case running, ok, failed
        case unknown(String)

        public static let known: [Status] = [.running, .ok, .failed]

        public var rawValue: String {
            switch self {
            case .running: "running"
            case .ok: "ok"
            case .failed: "failed"
            case .unknown(let value): value
            }
        }
    }

    public var jobId: String
    public var name: String
    public var progress: Int?
    public var message: String?
    public var status: Status
    public var startedAt: Date
    public var endedAt: Date?

    public init(
        jobId: String, name: String, progress: Int?, message: String?, status: Status,
        startedAt: Date, endedAt: Date?
    ) {
        self.jobId = jobId
        self.name = name
        self.progress = progress
        self.message = message
        self.status = status
        self.startedAt = startedAt
        self.endedAt = endedAt
    }

    private enum CodingKeys: String, CodingKey {
        case jobId, name, progress, message, status, startedAt, endedAt
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        jobId = try container.decode(String.self, forKey: .jobId)
        name = try container.decode(String.self, forKey: .name)
        progress = try container.decodeIfPresent(Int.self, forKey: .progress)
        message = try container.decodeIfPresent(String.self, forKey: .message)
        status = try container.decode(Status.self, forKey: .status)
        startedAt = try container.decodeTimestamp(forKey: .startedAt)
        endedAt = try container.decodeTimestampIfPresent(forKey: .endedAt)
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(jobId, forKey: .jobId)
        try container.encode(name, forKey: .name)
        try container.encode(progress, forKey: .progress)
        try container.encode(message, forKey: .message)
        try container.encode(status, forKey: .status)
        try container.encodeTimestamp(startedAt, forKey: .startedAt)
        try container.encodeTimestamp(endedAt, forKey: .endedAt)
    }
}

// MARK: - Events

/// Why an approval closed. `gone`: its session vanished.
public enum CloseResolution: WireEnum {
    case phone, desk, expired, gone
    case unknown(String)

    public static let known: [CloseResolution] = [.phone, .desk, .expired, .gone]

    public var rawValue: String {
        switch self {
        case .phone: "phone"
        case .desk: "desk"
        case .expired: "expired"
        case .gone: "gone"
        case .unknown(let value): value
        }
    }
}

public enum NotifyLevel: WireEnum {
    case info, done, needsYou
    case unknown(String)

    public static let known: [NotifyLevel] = [.info, .done, .needsYou]

    public var rawValue: String {
        switch self {
        case .info: "info"
        case .done: "done"
        case .needsYou: "needs_you"
        case .unknown(let value): value
        }
    }
}

public enum HelperEvent: Hashable, Sendable {
    case session(Session)
    /// The session named by the envelope's `sessionId` is gone.
    case sessionRemoved
    case approvalOpen(Approval)
    case approvalClosed(requestId: String, resolution: CloseResolution)
    case notify(title: String, body: String, level: NotifyLevel)
    case job(Job)
    /// The machine's keep-awake state changed (awake.v1). Belongs to no session.
    case awake(AwakeStatus)
    /// An agent asked to open a web page (browser.v1). `url` is an http or https URL the
    /// helper checked; the app checks it again before showing anything.
    case browserOpen(url: String)
    /// A kind this client does not know, from a newer helper. Its seq still counts.
    case unknown(kind: String)
}

/// The params of an `event` notification.
public struct EventEnvelope: Codable, Hashable, Sendable {
    public var seq: Int
    public var ts: Date
    public var sessionId: String?
    public var event: HelperEvent

    public init(seq: Int, ts: Date, sessionId: String?, event: HelperEvent) {
        self.seq = seq
        self.ts = ts
        self.sessionId = sessionId
        self.event = event
    }

    private enum CodingKeys: String, CodingKey {
        case seq, ts, kind, sessionId
        case session, approval, requestId, resolution, title, body, level, job, awake, url
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        seq = try container.decode(Int.self, forKey: .seq)
        ts = try container.decodeTimestamp(forKey: .ts)
        sessionId = try container.decodeIfPresent(String.self, forKey: .sessionId)
        let kind = try container.decode(String.self, forKey: .kind)
        switch kind {
        case "session":
            event = .session(try container.decode(Session.self, forKey: .session))
        case "session.removed":
            event = .sessionRemoved
        case "approval.open":
            event = .approvalOpen(try container.decode(Approval.self, forKey: .approval))
        case "approval.closed":
            event = .approvalClosed(
                requestId: try container.decode(String.self, forKey: .requestId),
                resolution: try container.decode(CloseResolution.self, forKey: .resolution)
            )
        case "notify":
            event = .notify(
                title: try container.decode(String.self, forKey: .title),
                body: try container.decode(String.self, forKey: .body),
                level: try container.decode(NotifyLevel.self, forKey: .level)
            )
        case "job":
            event = .job(try container.decode(Job.self, forKey: .job))
        case "awake":
            event = .awake(try container.decode(AwakeStatus.self, forKey: .awake))
        case "browser.open":
            event = .browserOpen(url: try container.decode(String.self, forKey: .url))
        default:
            event = .unknown(kind: kind)
        }
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(seq, forKey: .seq)
        try container.encodeTimestamp(ts, forKey: .ts)
        try container.encode(sessionId, forKey: .sessionId)
        switch event {
        case .session(let session):
            try container.encode("session", forKey: .kind)
            try container.encode(session, forKey: .session)
        case .sessionRemoved:
            try container.encode("session.removed", forKey: .kind)
        case .approvalOpen(let approval):
            try container.encode("approval.open", forKey: .kind)
            try container.encode(approval, forKey: .approval)
        case .approvalClosed(let requestId, let resolution):
            try container.encode("approval.closed", forKey: .kind)
            try container.encode(requestId, forKey: .requestId)
            try container.encode(resolution, forKey: .resolution)
        case .notify(let title, let body, let level):
            try container.encode("notify", forKey: .kind)
            try container.encode(title, forKey: .title)
            try container.encode(body, forKey: .body)
            try container.encode(level, forKey: .level)
        case .job(let job):
            try container.encode("job", forKey: .kind)
            try container.encode(job, forKey: .job)
        case .awake(let status):
            try container.encode("awake", forKey: .kind)
            try container.encode(status, forKey: .awake)
        case .browserOpen(let url):
            try container.encode("browser.open", forKey: .kind)
            try container.encode(url, forKey: .url)
        case .unknown(let kind):
            // The kind's own fields were not decoded, so only the header survives.
            try container.encode(kind, forKey: .kind)
        }
    }
}

public struct EventsSubscribeParams: Codable, Hashable, Sendable {
    public var foreground: Bool?

    public init(foreground: Bool? = nil) {
        self.foreground = foreground
    }
}

/// The `events.subscribe` result. `seq` is the last event already reflected in
/// `sessions` and `approvals`; the client drops events at or below it.
public struct EventSnapshot: Codable, Hashable, Sendable {
    public var seq: Int
    public var sessions: [Session]
    public var approvals: [Approval]

    public init(seq: Int, sessions: [Session], approvals: [Approval]) {
        self.seq = seq
        self.sessions = sessions
        self.approvals = approvals
    }
}
