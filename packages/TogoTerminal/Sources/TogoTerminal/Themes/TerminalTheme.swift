import Foundation

/// Terminal colours as `#RRGGBB` strings, so themes stay plain data that sync and diff well.
public struct TerminalTheme: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var name: String
    /// The 16 ANSI colours, 0 through 15.
    public var ansi: [String]
    public var foreground: String
    public var background: String
    public var cursor: String
    public var selection: String

    public init(
        id: String,
        name: String,
        ansi: [String],
        foreground: String,
        background: String,
        cursor: String,
        selection: String
    ) {
        self.id = id
        self.name = name
        self.ansi = ansi
        self.foreground = foreground
        self.background = background
        self.cursor = cursor
        self.selection = selection
    }

    private enum CodingKeys: String, CodingKey {
        case id, name, ansi, foreground, background, cursor, selection
    }

    /// Stored and synced themes are untrusted input, so a palette that is not exactly 16
    /// `#RRGGBB` colours is refused here instead of being half applied by the renderer.
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        func color(_ key: CodingKeys) throws -> String {
            let value = try c.decode(String.self, forKey: key)
            guard RGB(hex: value) != nil else {
                throw DecodingError.dataCorruptedError(forKey: key, in: c, debugDescription: "not #RRGGBB: \(value)")
            }
            return value
        }
        id = try c.decode(String.self, forKey: .id)
        name = try c.decode(String.self, forKey: .name)
        ansi = try c.decode([String].self, forKey: .ansi)
        guard ansi.count == 16, ansi.allSatisfy({ RGB(hex: $0) != nil }) else {
            throw DecodingError.dataCorruptedError(
                forKey: .ansi, in: c, debugDescription: "expected 16 #RRGGBB colours, got \(ansi.count) entries")
        }
        foreground = try color(.foreground)
        background = try color(.background)
        cursor = try color(.cursor)
        selection = try color(.selection)
    }
}

/// Red, green and blue in 0...255, parsed from `#RRGGBB` or `RRGGBB`.
struct RGB: Equatable, Sendable {
    var red: UInt8
    var green: UInt8
    var blue: UInt8

    init(red: UInt8, green: UInt8, blue: UInt8) {
        self.red = red
        self.green = green
        self.blue = blue
    }

    init?(hex: String) {
        var text = Substring(hex.trimmingCharacters(in: .whitespaces))
        if text.hasPrefix("#") { text = text.dropFirst() }
        guard text.count == 6, text.allSatisfy(\.isHexDigit), let value = UInt32(text, radix: 16) else {
            return nil
        }
        red = UInt8((value >> 16) & 0xFF)
        green = UInt8((value >> 8) & 0xFF)
        blue = UInt8(value & 0xFF)
    }

    var hex: String {
        String(format: "#%02X%02X%02X", red, green, blue)
    }
}
