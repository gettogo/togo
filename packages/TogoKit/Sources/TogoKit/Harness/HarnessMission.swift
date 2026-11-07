import Foundation

/// Session.mission (harness.v1): what the activity pill and the roster line show.
public struct MissionSummary: Codable, Hashable, Sendable {
    public var needsYou: Int
    public var agents: Int
    public var background: Int
    /// nil when the harness has no plan or it is empty.
    public var plan: Progress?
    /// A turn is running.
    public var working: Bool

    public struct Progress: Codable, Hashable, Sendable {
        public var done: Int
        public var total: Int

        public init(done: Int, total: Int) {
            self.done = done
            self.total = total
        }
    }

    public init(needsYou: Int = 0, agents: Int = 0, background: Int = 0, plan: Progress? = nil, working: Bool = false) {
        self.needsYou = needsYou
        self.agents = agents
        self.background = background
        self.plan = plan
        self.working = working
    }

    private enum CodingKeys: String, CodingKey { case needsYou, agents, background, plan, working }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(needsYou, forKey: .needsYou)
        try c.encode(agents, forKey: .agents)
        try c.encode(background, forKey: .background)
        try c.encode(plan, forKey: .plan)
        try c.encode(working, forKey: .working)
    }
}

/// Mission control section kinds. `order` is the fixed display order.
public struct MissionSectionKind: OpenString {
    public var rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }

    public static let needs: Self = "needs"
    public static let agents: Self = "agents"
    public static let background: Self = "background"
    public static let plan: Self = "plan"
    public static let changes: Self = "changes"
    public static let ports: Self = "ports"
    public static let status: Self = "status"

    public static let order: [Self] = [.needs, .agents, .background, .plan, .changes, .ports, .status]
}

/// A session's mission control snapshot. A `HarnessDelta` applies to the snapshot whose `rev`
/// is one less than the delta's.
public struct HarnessMission: Codable, Hashable, Sendable {
    public var rev: Int
    public var summary: MissionSummary
    /// Only the sections the harness supports, in `MissionSectionKind.order`.
    public var sections: [HarnessSection]

    public init(rev: Int, summary: MissionSummary, sections: [HarnessSection]) {
        self.rev = rev
        self.summary = summary
        self.sections = sections
    }
}

public struct HarnessSection: Codable, Hashable, Sendable, Identifiable {
    public var kind: MissionSectionKind
    public var rows: [HarnessRow]
    /// Set for the status section only.
    public var status: HarnessStatus?

    public var id: String { kind.rawValue }

    public init(kind: MissionSectionKind, rows: [HarnessRow] = [], status: HarnessStatus? = nil) {
        self.kind = kind
        self.rows = rows
        self.status = status
    }
}

/// What tapping a row opens. `ref` depends on `kind`: an agent item id (chat), a session or
/// job id (terminal), a path (diff), a port or URL (url), a screen id, or a request id.
public struct HarnessOpen: Codable, Hashable, Sendable {
    public struct Kind: OpenString {
        public var rawValue: String
        public init(rawValue: String) { self.rawValue = rawValue }
        public static let chat: Self = "chat"
        public static let terminal: Self = "terminal"
        public static let diff: Self = "diff"
        public static let url: Self = "url"
        public static let screen: Self = "screen"
        public static let approval: Self = "approval"
    }

    public var kind: Kind
    public var ref: String

    public init(kind: Kind, ref: String) {
        self.kind = kind
        self.ref = ref
    }
}

/// A line in a mission control section or a Live list screen.
public struct HarnessRow: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var title: String
    public var subtitle: String?
    public var state: HarnessState
    public var startedAt: WireDate?
    /// 0...1 when known.
    public var progress: Double?
    /// Last lines of live output, at most 4.
    public var tail: [String]?
    public var open: HarnessOpen?
    public var actions: [HarnessActionRef]?
    public var children: [HarnessRow]?

    public init(id: String, title: String, subtitle: String? = nil, state: HarnessState, startedAt: WireDate? = nil,
                progress: Double? = nil, tail: [String]? = nil, open: HarnessOpen? = nil,
                actions: [HarnessActionRef]? = nil, children: [HarnessRow]? = nil) {
        self.id = id
        self.title = title
        self.subtitle = subtitle
        self.state = state
        self.startedAt = startedAt
        self.progress = progress
        self.tail = tail
        self.open = open
        self.actions = actions
        self.children = children
    }
}

/// The Status primitive and the mission control status section.
public struct HarnessStatus: Codable, Hashable, Sendable {
    public var model: String?
    public var mode: String?
    public var effort: String?
    /// How full the context window is, 0...100.
    public var contextPercent: Double?
    public var costUsd: Double?
    public var meters: [Meter]?
    public var facts: [HarnessFact]?
    public var actions: [HarnessActionRef]?

    public struct Meter: Codable, Hashable, Sendable, Identifiable {
        public var id: String
        public var label: String
        public var percent: Double
        public var resetsAt: WireDate?

        public init(id: String, label: String, percent: Double, resetsAt: WireDate? = nil) {
            self.id = id
            self.label = label
            self.percent = percent
            self.resetsAt = resetsAt
        }
    }

    public init(model: String? = nil, mode: String? = nil, effort: String? = nil, contextPercent: Double? = nil,
                costUsd: Double? = nil, meters: [Meter]? = nil, facts: [HarnessFact]? = nil,
                actions: [HarnessActionRef]? = nil) {
        self.model = model
        self.mode = mode
        self.effort = effort
        self.contextPercent = contextPercent
        self.costUsd = costUsd
        self.meters = meters
        self.facts = facts
        self.actions = actions
    }
}

/// Moves a mission from `rev - 1` to `rev`. A client holding any other rev opens the session
/// again.
public struct HarnessDelta: Codable, Hashable, Sendable {
    public var rev: Int
    public var ops: [Op]

    public init(rev: Int, ops: [Op]) {
        self.rev = rev
        self.ops = ops
    }

    /// One change. Which fields are set depends on `op`: upsert (section, row), remove
    /// (section, id), replace (section, rows), status (status), summary (summary).
    public struct Op: Codable, Hashable, Sendable {
        public struct Kind: OpenString {
            public var rawValue: String
            public init(rawValue: String) { self.rawValue = rawValue }
            public static let upsert: Self = "upsert"
            public static let remove: Self = "remove"
            public static let replace: Self = "replace"
            public static let status: Self = "status"
            public static let summary: Self = "summary"
        }

        public var op: Kind
        public var section: MissionSectionKind?
        public var id: String?
        public var row: HarnessRow?
        public var rows: [HarnessRow]?
        public var status: HarnessStatus?
        public var summary: MissionSummary?

        public init(op: Kind, section: MissionSectionKind? = nil, id: String? = nil, row: HarnessRow? = nil,
                    rows: [HarnessRow]? = nil, status: HarnessStatus? = nil, summary: MissionSummary? = nil) {
            self.op = op
            self.section = section
            self.id = id
            self.row = row
            self.rows = rows
            self.status = status
            self.summary = summary
        }
    }
}
