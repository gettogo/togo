import Foundation

/// Finds every web link in terminal text: the visible screen plus recent scrollback.
///
/// Rows the emulator soft-wrapped are joined first. TUIs also hard-break long URLs themselves,
/// often inside a box or under an indent, so a row that filled to its right margin and ends in
/// a URL continues on the next row when that row's text, past any indentation or box-drawing
/// border, starts with characters a URL may hold.
public enum LinkScan {
    /// One terminal row. `wrapped` is true when the emulator soft-wrapped it from the row above.
    public struct Row: Equatable, Sendable {
        public var text: String
        public var wrapped: Bool

        public init(_ text: String, wrapped: Bool = false) {
            self.text = text
            self.wrapped = wrapped
        }
    }

    /// Every allowed link, newest (lowest on screen) first, each once.
    public static func links(in rows: [Row], columns: Int) -> [URL] {
        var seen: Set<String> = []
        var found: [URL] = []
        for line in joined(rows, columns: columns).reversed() {
            for url in urls(in: line).reversed() where seen.insert(url.absoluteString).inserted {
                found.append(url)
            }
        }
        return found
    }

    /// The logical lines of `rows`: soft wraps joined, and URLs a TUI broke across rows rejoined.
    static func joined(_ rows: [Row], columns: Int) -> [String] {
        var physical: [[String]] = []
        for row in rows {
            if row.wrapped, !physical.isEmpty {
                physical[physical.count - 1].append(row.text)
            } else {
                physical.append([row.text])
            }
        }
        var lines: [String] = []
        var continuing = false
        for parts in physical {
            let text = parts.joined()
            let content = stripped(text)
            if continuing, let lead = content.first, isURLCharacter(lead), !startsURL(content) {
                var previous = Substring(lines[lines.count - 1])
                while let c = previous.last, isFrame(c) { previous.removeLast() }
                lines[lines.count - 1] = previous + content
            } else {
                lines.append(text)
            }
            let last = lines[lines.count - 1]
            continuing = filled(parts.last ?? "", columns: columns) && endsInURL(last)
        }
        return lines
    }

    /// The allowed URLs in one logical line, left to right.
    static func urls(in line: String) -> [URL] {
        line.matches(of: pattern).compactMap { match in
            let text = trimmed(String(line[match.range]))
            guard let url = URL(string: text), LinkPolicy.allows(url) else { return nil }
            return url
        }
    }

    static var pattern: Regex<Substring> { /https?:\/\/[^\s<>"'`│┃║|]+/.ignoresCase() }

    // MARK: Rows

    /// Box-drawing borders and the marks TUIs indent output with.
    private static func isFrame(_ c: Character) -> Bool {
        if c.isWhitespace || c == "|" || c == "⎿" || c == "⏺" || c == "▌" || c == "▐" { return true }
        guard let scalar = c.unicodeScalars.first, c.unicodeScalars.count == 1 else { return false }
        return (0x2500...0x257F).contains(scalar.value)
    }

    /// A row's text without the indentation and borders around it.
    static func stripped(_ text: String) -> String {
        var s = Substring(text)
        while let c = s.first, isFrame(c) { s.removeFirst() }
        while let c = s.last, isFrame(c) { s.removeLast() }
        return String(s)
    }

    /// Whether the row's text runs to the right margin: to the last column or two, or up to a
    /// closing border with at most one space of padding before it.
    static func filled(_ row: String, columns: Int) -> Bool {
        var chars = Array(row)
        while let last = chars.last, last == " " { chars.removeLast() }
        guard let last = chars.last else { return false }
        if !last.isWhitespace, isFrame(last) {
            chars.removeLast()
            var padding = 0
            while let c = chars.last, c == " " {
                chars.removeLast()
                padding += 1
            }
            return padding <= 1 && !chars.isEmpty && !isFrame(chars.last!)
        }
        let width = chars.reduce(0) { $0 + cellWidth($1) }
        return width >= max(1, columns - 1)
    }

    private static func cellWidth(_ c: Character) -> Int {
        guard let scalar = c.unicodeScalars.first else { return 1 }
        switch scalar.value {
        case 0x1100...0x115F, 0x2E80...0xA4CF, 0xAC00...0xD7A3, 0xF900...0xFAFF, 0xFE30...0xFE4F,
             0xFF00...0xFF60, 0xFFE0...0xFFE6, 0x1F300...0x1F64F, 0x1F900...0x1F9FF, 0x20000...0x3FFFD:
            return 2
        default:
            return 1
        }
    }

    /// Whether the line's last word, past any closing border, is a URL still being written.
    private static func endsInURL(_ line: String) -> Bool {
        let content = stripped(line)
        guard let last = content.last, isURLCharacter(last) else { return false }
        let word = content.split(whereSeparator: { $0.isWhitespace || isFrame($0) }).last.map(String.init) ?? ""
        return word.range(of: "https?://", options: [.regularExpression, .caseInsensitive]) != nil
            && word.allSatisfy(isURLCharacter)
    }

    private static func startsURL(_ text: String) -> Bool {
        text.range(of: "^https?://", options: [.regularExpression, .caseInsensitive]) != nil
    }

    private static let urlCharacters = CharacterSet(
        charactersIn: "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~:/?#[]@!$&'()*+,;=%"
    )

    static func isURLCharacter(_ c: Character) -> Bool {
        c.unicodeScalars.count == 1 && urlCharacters.contains(c.unicodeScalars.first!)
    }

    // MARK: Trimming

    /// Punctuation that usually ends the sentence around a URL rather than the URL itself.
    private static let trailingPunctuation: Set<Character> = [".", ",", ";", ":", "!", "?", "'", "\""]
    private static let closers: [Character: Character] = [")": "(", "]": "[", "}": "{"]

    static func trimmed(_ text: String) -> String {
        var text = Substring(text)
        while let last = text.last {
            if trailingPunctuation.contains(last) {
                text.removeLast()
            } else if let opener = closers[last], text.count(where: { $0 == opener }) < text.count(where: { $0 == last }) {
                // An unbalanced closer belongs to the prose: "(see https://a.dev/x)".
                text.removeLast()
            } else {
                break
            }
        }
        return String(text)
    }
}
