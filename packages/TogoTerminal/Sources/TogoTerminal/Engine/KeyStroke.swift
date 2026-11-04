import Foundation

public struct KeyModifiers: OptionSet, Hashable, Codable, Sendable {
    public let rawValue: Int
    public init(rawValue: Int) { self.rawValue = rawValue }

    public static let ctrl = KeyModifiers(rawValue: 1 << 0)
    public static let alt = KeyModifiers(rawValue: 1 << 1)
    public static let shift = KeyModifiers(rawValue: 1 << 2)
}

/// A key as the user means it, before any terminal mode turns it into bytes.
///
/// Encodes as a single string so saved key bar layouts stay readable: a character is
/// itself (`"é"`), a named key is its name (`"pageUp"`), a function key is `"f12"`.
/// Function keys run F1 to F20, the range xterm encodes; any other number fails to encode
/// and to decode, so a stored layout never holds a key that would send nothing.
public enum TerminalKey: Hashable, Codable, Sendable {
    case char(Character), escape, tab, enter, backspace, delete
    case up, down, left, right, home, end, pageUp, pageDown, insert
    case function(Int)

    private static let named: [(String, TerminalKey)] = [
        ("escape", .escape), ("tab", .tab), ("enter", .enter), ("backspace", .backspace),
        ("delete", .delete), ("up", .up), ("down", .down), ("left", .left), ("right", .right),
        ("home", .home), ("end", .end), ("pageUp", .pageUp), ("pageDown", .pageDown),
        ("insert", .insert),
    ]

    static let functionKeys = 1...20

    var codedName: String {
        switch self {
        case .char(let c): return String(c)
        case .function(let n): return "f\(n)"
        default: return Self.named.first { $0.1 == self }?.0 ?? ""
        }
    }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.singleValueContainer()
        let text = try container.decode(String.self)
        if text.count == 1, let c = text.first {
            self = .char(c)
        } else if let match = Self.named.first(where: { $0.0 == text }) {
            self = match.1
        } else if text.hasPrefix("f"), let n = Int(text.dropFirst()), Self.functionKeys.contains(n) {
            self = .function(n)
        } else {
            throw DecodingError.dataCorruptedError(
                in: container, debugDescription: "unknown terminal key \(text.debugDescription)")
        }
    }

    public func encode(to encoder: any Encoder) throws {
        if case .function(let n) = self, !Self.functionKeys.contains(n) {
            throw EncodingError.invalidValue(self, EncodingError.Context(
                codingPath: encoder.codingPath, debugDescription: "function key \(n) is outside F1-F20"))
        }
        var container = encoder.singleValueContainer()
        try container.encode(codedName)
    }
}

/// A key plus modifiers, encoded by `KeyEncoder` the way xterm does.
///
/// Shift only uppercases letters. For anything else the caller passes the shifted character
/// itself (`.char("!")`, not `.char("1")` with Shift), because the shifted symbol depends on
/// the keyboard layout. Ctrl with a character that has no C0 control code (anything outside
/// ASCII letters, space and `@[\]^_?/2345678`) sends nothing rather than the bare
/// character, so a chord never types something the user did not ask for.
public struct KeyStroke: Hashable, Codable, Sendable, CustomStringConvertible {
    public var key: TerminalKey
    public var modifiers: KeyModifiers

    public init(_ key: TerminalKey, _ modifiers: KeyModifiers = []) {
        self.key = key
        self.modifiers = modifiers
    }

    private enum CodingKeys: String, CodingKey { case key, modifiers }

    public init(from decoder: any Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        key = try container.decode(TerminalKey.self, forKey: .key)
        modifiers = try container.decodeIfPresent(KeyModifiers.self, forKey: .modifiers) ?? []
    }

    public var description: String {
        var parts: [String] = []
        if modifiers.contains(.ctrl) { parts.append("ctrl") }
        if modifiers.contains(.alt) { parts.append("alt") }
        if modifiers.contains(.shift) { parts.append("shift") }
        parts.append(key.codedName.debugDescription)
        return parts.joined(separator: "+")
    }
}
