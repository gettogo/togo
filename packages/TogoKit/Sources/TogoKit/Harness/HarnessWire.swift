import Foundation

// Shared pieces of the harness.v1 models. Field names follow protocol/v1/schema.json
// ($defs Harness*). Optional keys the helper omits stay optional here so a value encodes back
// the way it arrived; keys the schema requires but allows to be null encode as null.

/// A string value from the helper that the app matches against known constants. Unknown values
/// decode and encode unchanged, so a newer helper never fails the message that carries one.
public protocol OpenString: RawRepresentable, Codable, Hashable, Sendable, ExpressibleByStringLiteral
where RawValue == String {
    init(rawValue: String)
}

extension OpenString {
    public init(stringLiteral value: String) { self.init(rawValue: value) }
}

/// An RFC 3339 timestamp, whole seconds in UTC on the way out, as the helper writes them.
public struct WireDate: Codable, Hashable, Sendable, Comparable {
    public var date: Date

    public init(_ date: Date) { self.date = date }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.singleValueContainer()
        let stamp = try container.decode(String.self)
        guard let date = RFC3339.parse(stamp) else {
            throw DecodingError.dataCorruptedError(in: container, debugDescription: "not an RFC 3339 timestamp: \(stamp)")
        }
        self.date = date
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(RFC3339.format(date))
    }

    public static func < (a: WireDate, b: WireDate) -> Bool { a.date < b.date }
}

/// Run states shared by tools, turns, agents, background work, shell blocks and rows.
public struct HarnessState: OpenString {
    public var rawValue: String
    public init(rawValue: String) { self.rawValue = rawValue }

    public static let running: Self = "running"
    public static let waiting: Self = "waiting"
    public static let ok: Self = "ok"
    public static let failed: Self = "failed"
    public static let denied: Self = "denied"
    public static let cancelled: Self = "cancelled"
    public static let interrupted: Self = "interrupted"
    public static let idle: Self = "idle"
    // Plan rows.
    public static let pending: Self = "pending"
    public static let active: Self = "active"
    public static let done: Self = "done"
    public static let skipped: Self = "skipped"

    /// Still going: running, waiting or active.
    public var isLive: Bool { self == .running || self == .waiting || self == .active }
}

/// A labelled option, used by questions, pickers, filters, settings and modes.
public struct HarnessChoice: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var label: String
    public var detail: String?

    public init(id: String, label: String, detail: String? = nil) {
        self.id = id
        self.label = label
        self.detail = detail
    }
}

/// One question of a Confirm, in a question item or a confirm screen.
public struct HarnessQuestion: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var header: String?
    public var prompt: String
    public var options: [HarnessChoice]
    public var multi: Bool
    public var allowText: Bool

    public init(id: String, header: String? = nil, prompt: String, options: [HarnessChoice], multi: Bool, allowText: Bool) {
        self.id = id
        self.header = header
        self.prompt = prompt
        self.options = options
        self.multi = multi
        self.allowText = allowText
    }

    /// A missing or null `options` reads as none, so an older helper's question without
    /// options still decodes instead of failing its whole frame.
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        header = try c.decodeIfPresent(String.self, forKey: .header)
        prompt = try c.decode(String.self, forKey: .prompt)
        options = try c.decodeIfPresent([HarnessChoice].self, forKey: .options) ?? []
        multi = try c.decode(Bool.self, forKey: .multi)
        allowText = try c.decode(Bool.self, forKey: .allowText)
    }
}

public struct HarnessFileChange: Codable, Hashable, Sendable {
    public var path: String
    public var added: Int
    public var removed: Int

    public init(path: String, added: Int, removed: Int) {
        self.path = path
        self.added = added
        self.removed = removed
    }
}

/// Where in the pane's output an item happened, for "Show in terminal".
public struct HarnessAnchor: Codable, Hashable, Sendable {
    /// Names the pane's output stream; an anchor from another epoch is found by `needle` only.
    public var epoch: String
    /// Output bytes the helper's tap had seen in this epoch when the item arrived.
    public var bytes: Int64
    /// Absolute grid line of the cursor when the item arrived: history lines plus cursor row.
    public var line: Int64
    /// Up to 120 characters of text near `line`, to confirm the match or search for it.
    public var needle: String?

    public init(epoch: String, bytes: Int64, line: Int64, needle: String? = nil) {
        self.epoch = epoch
        self.bytes = bytes
        self.line = line
        self.needle = needle
    }
}

/// A label and a value, for status extras and key-value bodies.
public struct HarnessFact: Codable, Hashable, Sendable {
    public var label: String
    public var value: String

    public init(label: String, value: String) {
        self.label = label
        self.value = value
    }
}
