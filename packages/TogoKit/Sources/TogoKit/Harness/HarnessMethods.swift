import Foundation

/// Session capabilities a harness adapter advertises in `harness.describe`. The app shows a
/// layer, a mission control section or a control only when its flag is present.
public enum HarnessCapability {
    public static let chat = "chat"
    public static let chatStream = "chat.stream"
    public static let history = "chat.history"
    public static let anchors = "chat.anchors"
    public static let sendAPI = "send.api"
    public static let queue = "send.queue"
    public static let interrupt = "interrupt"
    public static let commands = "commands"
    public static let files = "files"
    public static let mode = "mode"
    public static let model = "model"
    public static let effort = "effort"
    public static let needsApprovals = "needs.approvals"
    public static let needsQuestions = "needs.questions"
    public static let agents = "agents"
    public static let background = "background"
    public static let plan = "plan"
    public static let changes = "changes"
    public static let ports = "ports"
    public static let status = "status"
    public static let usage = "status.usage"
    public static let context = "status.context"
    public static let resume = "resume"
    public static let compact = "compact"
    public static let mcp = "mcp"
    public static let settings = "settings"
}

/// The `harness.describe` result.
public struct HarnessDescriptor: Codable, Hashable, Sendable {
    public struct Adapter: Codable, Hashable, Sendable {
        public var id: String
        public var name: String
        public var harnessVersion: String
        public var revision: Int

        public init(id: String, name: String, harnessVersion: String, revision: Int) {
            self.id = id
            self.name = name
            self.harnessVersion = harnessVersion
            self.revision = revision
        }
    }

    /// `section` places the screen: a mission control section kind, `composer` (the mode chip
    /// and the / menu) or `more`.
    public struct ScreenRef: Codable, Hashable, Sendable, Identifiable {
        public var id: String
        public var primitive: HarnessPrimitive
        public var title: String
        public var section: String

        public init(id: String, primitive: HarnessPrimitive, title: String, section: String) {
            self.id = id
            self.primitive = primitive
            self.title = title
            self.section = section
        }
    }

    /// nil when no adapter serves the session: show the terminal only.
    public var adapter: Adapter?
    public var capabilities: [String]
    public var screens: [ScreenRef]
    /// The mode chip's choices, in the harness's own cycle order.
    public var modes: [HarnessChoice]
    /// The current mode id, or empty when unknown.
    public var mode: String
    public var degraded: [String]

    public init(adapter: Adapter? = nil, capabilities: [String] = [], screens: [ScreenRef] = [],
                modes: [HarnessChoice] = [], mode: String = "", degraded: [String] = []) {
        self.adapter = adapter
        self.capabilities = capabilities
        self.screens = screens
        self.modes = modes
        self.mode = mode
        self.degraded = degraded
    }

    public func has(_ capability: String) -> Bool { capabilities.contains(capability) }

    private enum CodingKeys: String, CodingKey { case adapter, capabilities, screens, modes, mode, degraded }

    // adapter is a required key that may be null.
    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(adapter, forKey: .adapter)
        try c.encode(capabilities, forKey: .capabilities)
        try c.encode(screens, forKey: .screens)
        try c.encode(modes, forKey: .modes)
        try c.encode(mode, forKey: .mode)
        try c.encode(degraded, forKey: .degraded)
    }
}

// MARK: - Params and results

/// `harness.describe`, `harness.close` and `harness.interrupt` take only the session id.
public typealias HarnessSessionParams = SessionIDParams

public struct HarnessOpenParams: Codable, Hashable, Sendable {
    public var id: String
    /// The client's cursor; 0 for none.
    public var after: Int?
    public var limit: Int?

    public init(id: String, after: Int? = nil, limit: Int? = nil) {
        self.id = id
        self.after = after
        self.limit = limit
    }
}

/// The `harness.open` result. Drop notified items with `seq <= cursor`. A different epoch than
/// the one held means the held items are void.
public struct HarnessOpenResult: Codable, Hashable, Sendable {
    public var epoch: String
    public var cursor: Int
    public var items: [HarnessItem]
    /// Older items exist; page them with `harness.history`.
    public var more: Bool
    public var mission: HarnessMission

    public init(epoch: String, cursor: Int, items: [HarnessItem], more: Bool, mission: HarnessMission) {
        self.epoch = epoch
        self.cursor = cursor
        self.items = items
        self.more = more
        self.mission = mission
    }
}

public struct HarnessHistoryParams: Codable, Hashable, Sendable {
    public var id: String
    public var before: Int
    public var limit: Int?

    public init(id: String, before: Int, limit: Int? = nil) {
        self.id = id
        self.before = before
        self.limit = limit
    }
}

public struct HarnessPage: Codable, Hashable, Sendable {
    public var items: [HarnessItem]
    public var more: Bool

    public init(items: [HarnessItem], more: Bool) {
        self.items = items
        self.more = more
    }
}

public struct HarnessScreenParams: Codable, Hashable, Sendable {
    public var id: String
    public var screen: String
    public var query: [String: String]?

    public init(id: String, screen: String, query: [String: String]? = nil) {
        self.id = id
        self.screen = screen
        self.query = query
    }
}

/// Built-in actions: `answer` (args.answers), `set_mode` (args.id), `interrupt`, `stop` (target).
public struct HarnessActionParams: Codable, Hashable, Sendable {
    public var id: String
    public var action: String
    public var target: String?
    public var args: [String: JSONValue]?

    public init(id: String, action: String, target: String? = nil, args: [String: JSONValue]? = nil) {
        self.id = id
        self.action = action
        self.target = target
        self.args = args
    }
}

public struct HarnessActionResult: Codable, Hashable, Sendable {
    /// applied: the harness confirmed it. typed: a documented command was typed, not yet
    /// confirmed ("Sent"). fallback: no route works; open the terminal.
    public struct Outcome: OpenString {
        public var rawValue: String
        public init(rawValue: String) { self.rawValue = rawValue }
        public static let applied: Self = "applied"
        public static let typed: Self = "typed"
        public static let relaunched: Self = "relaunched"
        public static let queued: Self = "queued"
        public static let failed: Self = "failed"
        public static let fallback: Self = "fallback"
    }

    public var result: Outcome
    public var message: String?
    /// The screen read again after the action, when the action came from one.
    public var screen: HarnessScreen?

    public init(result: Outcome, message: String? = nil, screen: HarnessScreen? = nil) {
        self.result = result
        self.message = message
        self.screen = screen
    }
}

public struct HarnessSendParams: Codable, Hashable, Sendable {
    /// `queue` types now when idle, else holds the prompt until the turn ends; `now` is refused
    /// with conflict reason busy while the agent works.
    public enum Mode: String, Codable, Hashable, Sendable { case now, queue }

    public var id: String
    public var text: String
    public var mode: Mode?

    public init(id: String, text: String, mode: Mode? = nil) {
        self.id = id
        self.text = text
        self.mode = mode
    }
}

public struct HarnessSendResult: Codable, Hashable, Sendable {
    public var item: HarnessItem

    public init(item: HarnessItem) { self.item = item }
}

public struct HarnessCompleteParams: Codable, Hashable, Sendable {
    public enum Kind: String, Codable, Hashable, Sendable { case command, file }

    public var id: String
    public var kind: Kind
    public var query: String
    public var limit: Int?

    public init(id: String, kind: Kind, query: String, limit: Int? = nil) {
        self.id = id
        self.kind = kind
        self.query = query
        self.limit = limit
    }
}

/// One / or @ suggestion. With `screen` set, choosing it opens that native screen instead of
/// inserting text.
public struct HarnessCompletion: Codable, Hashable, Sendable {
    public var insert: String
    public var title: String
    public var detail: String?
    public var hint: String?
    public var screen: String?

    public init(insert: String, title: String, detail: String? = nil, hint: String? = nil, screen: String? = nil) {
        self.insert = insert
        self.title = title
        self.detail = detail
        self.hint = hint
        self.screen = screen
    }
}

public struct HarnessCompleteResult: Codable, Hashable, Sendable {
    public var items: [HarnessCompletion]

    public init(items: [HarnessCompletion]) { self.items = items }
}

public struct HarnessLocateParams: Codable, Hashable, Sendable {
    public var id: String
    public var anchor: HarnessAnchor

    public init(id: String, anchor: HarnessAnchor) {
        self.id = id
        self.anchor = anchor
    }
}

/// Where an anchor sits in the pane now. `found` is false when it scrolled out of history and
/// its needle is not on screen.
public struct HarnessLocation: Codable, Hashable, Sendable {
    public var found: Bool
    public var linesFromBottom: Int
    public var needle: String?

    public init(found: Bool, linesFromBottom: Int, needle: String? = nil) {
        self.found = found
        self.linesFromBottom = linesFromBottom
        self.needle = needle
    }
}

/// The params of a `harness` notification, sent only for sessions this connection opened.
public struct HarnessNotification: Codable, Hashable, Sendable {
    public struct Kind: OpenString {
        public var rawValue: String
        public init(rawValue: String) { self.rawValue = rawValue }
        public static let items: Self = "items"
        public static let mission: Self = "mission"
        /// Open the session again: the adapter or epoch changed, or the connection fell behind.
        public static let reset: Self = "reset"
    }

    public var sessionId: String
    public var kind: Kind
    public var ts: WireDate
    public var epoch: String
    public var items: [HarnessItem]?
    public var mission: HarnessDelta?
    public var reason: String?

    public init(sessionId: String, kind: Kind, ts: WireDate, epoch: String, items: [HarnessItem]? = nil,
                mission: HarnessDelta? = nil, reason: String? = nil) {
        self.sessionId = sessionId
        self.kind = kind
        self.ts = ts
        self.epoch = epoch
        self.items = items
        self.mission = mission
        self.reason = reason
    }
}
