import Foundation

public enum DiagnosticCategory: String, Codable, Sendable {
    case app, link, machine, helper, push, perf
}

/// A machine as it appears in diagnostics: `m-` and eight hex digits of an HMAC of the host id
/// under a salt that never leaves this device, so a token cannot be matched to a machine.
public struct MachineToken: Codable, Hashable, Sendable {
    public let value: String

    public init(value: String) {
        self.value = value
    }

    public init(from decoder: any Decoder) throws {
        value = try decoder.singleValueContainer().decode(String.self)
    }

    public func encode(to encoder: any Encoder) throws {
        var container = encoder.singleValueContainer()
        try container.encode(value)
    }
}

/// The only kinds of value a diagnostic field can hold. There is no plain string case:
/// words come from a fixed vocabulary and free text is always scrubbed and cut.
public enum DiagnosticValue: Codable, Equatable, Sendable {
    case int(Int)
    case double(Double)
    case bool(Bool)
    /// Must match `^[a-z0-9_.:-]{1,40}$` and survive `Redactor` unchanged, or the field is dropped.
    case word(String)
    case machine(MachineToken)
    /// Scrubbed with `Redactor`, then cut to `textLimit` characters.
    case text(String)

    public static let textLimit = 300
    private static let wordPattern = "^[a-z0-9_.:-]{1,40}$"

    public static func isWord(_ candidate: String) -> Bool {
        candidate.range(of: wordPattern, options: .regularExpression) != nil && Redactor.scrub(candidate) == candidate
    }

    /// A word from `raw` lowercased, or nil when it does not fit the vocabulary.
    public static func word(lowercasing raw: String) -> DiagnosticValue? {
        let lowered = raw.lowercased()
        return isWord(lowered) ? .word(lowered) : nil
    }

    /// The value as it may be kept, or nil when it must be dropped.
    public var sanitized: DiagnosticValue? {
        sanitized(known: [])
    }

    /// As `sanitized`, also removing `known` names from text and dropping words that contain one.
    public func sanitized(known: [String]) -> DiagnosticValue? {
        switch self {
        case .int, .bool: return self
        case .machine(let token): return token.value.range(of: "^m-[0-9a-f]{8}$", options: .regularExpression) != nil ? self : nil
        case .double(let value): return value.isFinite ? self : nil
        case .word(let word):
            return Self.isWord(word) && !Redactor.mentions(word, known: known) ? self : nil
        case .text(let text):
            // Control characters become spaces so a value cannot start a line of its own.
            let scrubbed = Redactor.scrub(text, known: known).unicodeScalars.map {
                CharacterSet.controlCharacters.contains($0) ? " " : Character($0)
            }
            return .text(String(String(scrubbed).prefix(Self.textLimit)))
        }
    }

    /// Plain rendering for the text report.
    public var display: String {
        switch self {
        case .int(let value): String(value)
        case .double(let value): String(value)
        case .bool(let value): value ? "true" : "false"
        case .word(let word): word
        case .machine(let token): token.value
        case .text(let text): "\"\(text)\""
        }
    }

    private enum Key: String, CodingKey {
        case int, double, bool, word, machine, text
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: Key.self)
        if let value = try c.decodeIfPresent(Int.self, forKey: .int) { self = .int(value) }
        else if let value = try c.decodeIfPresent(Double.self, forKey: .double) { self = .double(value) }
        else if let value = try c.decodeIfPresent(Bool.self, forKey: .bool) { self = .bool(value) }
        else if let value = try c.decodeIfPresent(String.self, forKey: .word) { self = .word(value) }
        else if let value = try c.decodeIfPresent(MachineToken.self, forKey: .machine) { self = .machine(value) }
        else if let value = try c.decodeIfPresent(String.self, forKey: .text) { self = .text(value) }
        else { throw DecodingError.dataCorrupted(.init(codingPath: c.codingPath, debugDescription: "Unknown value")) }
    }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: Key.self)
        switch self {
        case .int(let value): try c.encode(value, forKey: .int)
        case .double(let value): try c.encode(value, forKey: .double)
        case .bool(let value): try c.encode(value, forKey: .bool)
        case .word(let value): try c.encode(value, forKey: .word)
        case .machine(let value): try c.encode(value, forKey: .machine)
        case .text(let value): try c.encode(value, forKey: .text)
        }
    }
}

public struct DiagnosticEvent: Codable, Equatable, Sendable {
    public var at: Date
    public var category: DiagnosticCategory
    public var name: String
    public var fields: [String: DiagnosticValue]

    public init(at: Date = Date(), category: DiagnosticCategory, name: String, fields: [String: DiagnosticValue] = [:]) {
        self.at = at
        self.category = category
        self.name = name
        self.fields = fields
    }

    /// The event with every field checked, or nil when its name is not a word. Field names
    /// and values that do not fit are dropped.
    public var sanitized: DiagnosticEvent? {
        sanitized(known: [])
    }

    public func sanitized(known: [String]) -> DiagnosticEvent? {
        guard DiagnosticValue.isWord(name) else { return nil }
        var kept: [String: DiagnosticValue] = [:]
        for (key, value) in fields where DiagnosticValue.isWord(key) {
            if let clean = value.sanitized(known: known) { kept[key] = clean }
        }
        return DiagnosticEvent(at: at, category: category, name: name, fields: kept)
    }
}
