import Foundation

public struct KeySequenceError: Error, Equatable {
    /// Offset in characters from the start of the parsed string.
    public var position: Int
    public var message: String

    public init(position: Int, message: String) {
        self.position = position
        self.message = message
    }
}

/// The text form of a key macro, as typed in the shortcut builder.
///
/// Steps are separated by single spaces. A step is a key name (`Esc`, `Tab`, `Enter`, `BS`,
/// `Del`, `Up`, `Down`, `Left`, `Right`, `Home`, `End`, `PgUp`, `PgDn`, `Ins`, `F1` to `F12`)
/// or one printable character, optionally prefixed by `C-`, `M-` and `S-` in any order;
/// `text:"..."` with `\"` and `\\` escapes; or `delay:<ms>` with 1 to 2000 ms.
public enum KeySequence {
    static let maximumDelay = 2000

    private static let names: [(String, TerminalKey)] = [
        ("Esc", .escape), ("Tab", .tab), ("Enter", .enter), ("BS", .backspace), ("Del", .delete),
        ("Up", .up), ("Down", .down), ("Left", .left), ("Right", .right), ("Home", .home),
        ("End", .end), ("PgUp", .pageUp), ("PgDn", .pageDown), ("Ins", .insert),
    ] + (1...12).map { ("F\($0)", TerminalKey.function($0)) }

    private static let prefixes: [(Character, KeyModifiers)] = [("C", .ctrl), ("M", .alt), ("S", .shift)]

    public static func parse(_ s: String) throws -> [KeyStep] {
        let chars = Array(s)
        var steps: [KeyStep] = []
        var index = 0
        while index < chars.count {
            let start = index
            let step: KeyStep
            if chars[index...].starts(with: "text:\"") {
                (step, index) = try parseText(chars, from: start)
            } else {
                while index < chars.count, chars[index] != " " { index += 1 }
                step = try parseToken(String(chars[start..<index]), at: start)
            }
            steps.append(step)
            if index < chars.count {
                guard chars[index] == " " else {
                    throw KeySequenceError(position: index, message: "expected a space between steps")
                }
                index += 1
                guard index < chars.count, chars[index] != " " else {
                    throw KeySequenceError(position: index, message: "empty step")
                }
            }
        }
        return steps
    }

    public static func format(_ steps: [KeyStep]) -> String {
        steps.map(format).joined(separator: " ")
    }

    private static func format(_ step: KeyStep) -> String {
        switch step {
        case .delay(let ms):
            return "delay:\(ms)"
        case .text(let text):
            let escaped = text.replacingOccurrences(of: "\\", with: "\\\\")
                .replacingOccurrences(of: "\"", with: "\\\"")
            return "text:\"\(escaped)\""
        case .stroke(let stroke):
            var out = ""
            for (letter, modifier) in prefixes where stroke.modifiers.contains(modifier) {
                out += "\(letter)-"
            }
            if case .char(let c) = stroke.key { return out + String(c) }
            return out + (names.first { $0.1 == stroke.key }?.0 ?? stroke.key.codedName)
        }
    }

    private static func parseText(_ chars: [Character], from start: Int) throws -> (KeyStep, Int) {
        var index = start + "text:\"".count
        var text = ""
        while index < chars.count {
            switch chars[index] {
            case "\"":
                return (.text(text), index + 1)
            case "\\":
                guard index + 1 < chars.count, chars[index + 1] == "\"" || chars[index + 1] == "\\" else {
                    throw KeySequenceError(position: index, message: "only \\\" and \\\\ escapes are allowed")
                }
                text.append(chars[index + 1])
                index += 2
            default:
                text.append(chars[index])
                index += 1
            }
        }
        throw KeySequenceError(position: start, message: "text is missing its closing quote")
    }

    private static func parseToken(_ token: String, at position: Int) throws -> KeyStep {
        if token.hasPrefix("delay:") {
            guard let ms = Int(token.dropFirst("delay:".count)), (1...maximumDelay).contains(ms) else {
                throw KeySequenceError(position: position, message: "delay must be 1 to \(maximumDelay) ms")
            }
            return .delay(milliseconds: ms)
        }
        var rest = Substring(token)
        var modifiers: KeyModifiers = []
        while rest.count >= 2, rest.dropFirst().first == "-",
              let prefix = prefixes.first(where: { $0.0 == rest.first }) {
            guard !modifiers.contains(prefix.1) else {
                throw KeySequenceError(position: position, message: "modifier \(prefix.0)- repeated")
            }
            modifiers.insert(prefix.1)
            rest = rest.dropFirst(2)
        }
        guard !rest.isEmpty else {
            throw KeySequenceError(position: position, message: "missing key after modifier")
        }
        if let named = names.first(where: { $0.0 == rest }) {
            return .stroke(KeyStroke(named.1, modifiers))
        }
        guard rest.count == 1, let c = rest.first, isPrintable(c) else {
            throw KeySequenceError(position: position, message: "unknown key \(String(rest).debugDescription)")
        }
        return .stroke(KeyStroke(.char(c), modifiers))
    }

    private static func isPrintable(_ c: Character) -> Bool {
        !c.isWhitespace && !c.unicodeScalars.contains { $0.properties.generalCategory == .control }
    }
}
