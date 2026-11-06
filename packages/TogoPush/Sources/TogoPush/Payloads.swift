import Foundation

/// What the helper seals for one device and sends through the relay. `ts` is Unix
/// milliseconds. `requestId` and `options` are set for `needsYou`; `clear` sets `requestId`
/// only.
public struct PushPayload: Codable, Sendable, Equatable {
    public enum Kind: String, Codable, Sendable {
        case needsYou = "needs_you"
        case done, job, notify, clear
        /// The agent exited on its own; never a "done".
        case ended
        /// The agent crashed, hit an error or stopped on a usage limit.
        case failed
        /// The machine went quiet while sessions were working.
        case offline
    }

    public var v: Int
    public var kind: Kind
    public var sessionId: String?
    public var bib: String?
    public var agent: String?
    /// The machine's short display name.
    public var host: String?
    public var title: String
    public var body: String
    public var requestId: String?
    public var options: [PushOption]?
    public var subject: PushSubject?
    public var ts: Int64
    /// How the agent ended; set for `ended` and `failed`.
    public var exit: PushExit?
    /// How many sessions were working; set for `offline`.
    public var active: Int?
    /// True for the one reminder of a request that is still waiting.
    public var reminder: Bool?
    /// True when a request takes a typed answer as well as its options. Absent from older
    /// helpers, which reads as false.
    public var allowText: Bool?

    public init(
        v: Int = 1, kind: Kind, sessionId: String? = nil, bib: String? = nil, agent: String? = nil,
        host: String? = nil, title: String, body: String, requestId: String? = nil, options: [PushOption]? = nil,
        subject: PushSubject? = nil, ts: Int64, exit: PushExit? = nil, active: Int? = nil, reminder: Bool? = nil,
        allowText: Bool? = nil
    ) {
        self.v = v
        self.kind = kind
        self.sessionId = sessionId
        self.bib = bib
        self.agent = agent
        self.host = host
        self.title = title
        self.body = body
        self.requestId = requestId
        self.options = options
        self.subject = subject
        self.ts = ts
        self.exit = exit
        self.active = active
        self.reminder = reminder
        self.allowText = allowText
    }
}

/// Why an agent stopped, as the helper reports it. `reason` is `exit`, `interrupted`,
/// `error`, `usage_limit` or a value a newer helper adds.
public struct PushExit: Codable, Sendable, Equatable {
    public var code: Int?
    public var reason: String
    public var detail: String?

    public init(code: Int?, reason: String, detail: String?) {
        self.code = code
        self.reason = reason
        self.detail = detail
    }
}

public struct PushOption: Codable, Sendable, Equatable {
    public var n: Int
    public var label: String
    /// `approve`, `approve_always`, `deny` or another role the helper reports.
    public var role: String

    public init(n: Int, label: String, role: String) {
        self.n = n
        self.label = label
        self.role = role
    }
}

/// The one thing an approval asks about. Decoding enforces what the helper validates: a
/// known kind and 1-1024 characters with no control characters other than newline and no
/// invisible characters (`PromptText.isInvisible`).
public struct PushSubject: Codable, Sendable, Equatable {
    public static let kinds: Set<String> = ["command", "file", "url", "text"]
    public static let maxText = 1024

    /// `command`, `file`, `url` or `text`.
    public var kind: String
    public var text: String

    public init(kind: String, text: String) {
        self.kind = kind
        self.text = text
    }

    public var isValid: Bool {
        Self.kinds.contains(kind) && !text.isEmpty && text.unicodeScalars.count <= Self.maxText
            && !text.unicodeScalars.contains {
                ($0 != "\n" && $0.properties.generalCategory == .control) || PromptText.isInvisible($0)
            }
    }

    private enum CodingKeys: String, CodingKey { case kind, text }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        kind = try container.decode(String.self, forKey: .kind)
        text = try container.decode(String.self, forKey: .text)
        guard isValid else {
            throw DecodingError.dataCorruptedError(
                forKey: .text, in: container, debugDescription: "subject must be a known kind with 1-1024 characters"
            )
        }
    }
}

/// An answer sealed for the host. `type` is never encoded: helpers that predate it reject
/// unknown fields, and an answer without a type is read as an answer.
public struct AnswerPlaintext: Codable, Sendable {
    public var type: String = "answer"
    public var requestId: String
    /// `approve`, `deny` or `option:<n>`.
    public var decision: String
    public var ts: Int64
    public var deviceId: String
    /// Ed25519 over `Signing.approvalBytes`, base64url without padding.
    public var sig: String

    public init(requestId: String, decision: String, ts: Int64, deviceId: String, sig: String) {
        self.requestId = requestId
        self.decision = decision
        self.ts = ts
        self.deviceId = deviceId
        self.sig = sig
    }

    private enum CodingKeys: String, CodingKey { case type, requestId, decision, ts, deviceId, sig }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        type = try container.decodeIfPresent(String.self, forKey: .type) ?? "answer"
        requestId = try container.decode(String.self, forKey: .requestId)
        decision = try container.decode(String.self, forKey: .decision)
        ts = try container.decode(Int64.self, forKey: .ts)
        deviceId = try container.decode(String.self, forKey: .deviceId)
        sig = try container.decode(String.self, forKey: .sig)
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(requestId, forKey: .requestId)
        try container.encode(decision, forKey: .decision)
        try container.encode(ts, forKey: .ts)
        try container.encode(deviceId, forKey: .deviceId)
        try container.encode(sig, forKey: .sig)
    }
}

/// New push tokens and the command text choice, sealed for the host through the mailbox.
/// Absent tokens are omitted; the choice is always sent and replaces the stored one.
public struct RegisterPlaintext: Codable, Sendable {
    public var type: String = "register"
    public var deviceId: String
    public var apnsToken: String?
    public var liveActivityToken: String?
    public var liveActivityStartToken: String?
    public var liveActivityCommand: Bool
    public var ts: Int64
    /// Ed25519 over `Signing.registerBytes`, base64url without padding.
    public var sig: String

    public init(
        deviceId: String, apnsToken: String?, liveActivityToken: String?, liveActivityStartToken: String?,
        liveActivityCommand: Bool, ts: Int64, sig: String
    ) {
        self.deviceId = deviceId
        self.apnsToken = apnsToken
        self.liveActivityToken = liveActivityToken
        self.liveActivityStartToken = liveActivityStartToken
        self.liveActivityCommand = liveActivityCommand
        self.ts = ts
        self.sig = sig
    }
}
