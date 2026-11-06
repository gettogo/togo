import Foundation

/// One visible terminal row as VoiceOver reads it.
public struct AccessibleLine: Equatable, Sendable {
    public var row: Int
    public var text: String
    /// True for the row the cursor is on; VoiceOver hears it as `AccessibilityWords.cursorLine`.
    public var isCursor: Bool

    public init(row: Int, text: String, isCursor: Bool = false) {
        self.row = row
        self.text = text
        self.isCursor = isCursor
    }
}

public enum TerminalAccessibility {
    /// Turns screen rows into spoken lines: drawing glyphs and spinners removed, runs of
    /// spaces collapsed, empty rows dropped. Row numbers stay those of the screen.
    public static func lines(screen: [String], cursorRow: Int) -> [AccessibleLine] {
        screen.enumerated().compactMap { row, raw in
            let text = clean(raw)
            guard !text.isEmpty else { return nil }
            return AccessibleLine(row: row, text: text, isCursor: row == cursorRow)
        }
    }

    /// The cursor row as VoiceOver reads it; an empty row is named, not skipped.
    public static func cursorLine(_ raw: String?, words: AccessibilityWords) -> String {
        let text = clean(raw ?? "")
        return "\(words.cursorLine), \(text.isEmpty ? words.blank : text)"
    }

    /// A row with its counters and status glyphs taken out: numbers with their units become
    /// `#`, arrows, dingbats and dots go. Two rows with the same key differ only in what an
    /// agent's timer, token count or progress bar changes from moment to moment.
    /// A number is a digit run with `.`, `,` or `:` inside, and an optional unit (`ms`, `s`,
    /// `m`, `h`, `k`, `K`, `%`); numbers separated only by spaces (`1m 3s`) make one `#`.
    /// Read for every new row while VoiceOver listens, so it is a single scan, not a regex.
    public static func tickKey(_ text: String) -> String {
        let scalars = Array(clean(text).unicodeScalars)
        var out = String.UnicodeScalarView()
        var pendingSpace = false
        var i = 0
        while i < scalars.count {
            let scalar = scalars[i]
            if isTicker(scalar) {
                i += 1
            } else if scalar == " " {
                pendingSpace = true
                i += 1
            } else if isDigit(scalar) {
                i += 1
                while i < scalars.count, isDigit(scalars[i]) || ".,:".unicodeScalars.contains(scalars[i]) { i += 1 }
                if i + 1 < scalars.count, scalars[i] == "m", scalars[i + 1] == "s" {
                    i += 2
                } else if i < scalars.count, "smhkK%".unicodeScalars.contains(scalars[i]) {
                    i += 1
                }
                if out.last == "#" {
                    pendingSpace = false
                    continue
                }
                if pendingSpace, !out.isEmpty { out.append(" ") }
                pendingSpace = false
                out.append("#")
            } else {
                if pendingSpace, !out.isEmpty { out.append(" ") }
                pendingSpace = false
                out.append(scalar)
                i += 1
            }
        }
        return String(out)
    }

    private static func isDigit(_ scalar: Unicode.Scalar) -> Bool {
        ("0"..."9").contains(scalar)
    }

    /// Arrows, dingbats (agent spinners such as ✻ ✶ ✢), middle dot, bullet and ellipsis.
    static func isTicker(_ scalar: Unicode.Scalar) -> Bool {
        switch scalar.value {
        case 0x2190...0x21FF, 0x2700...0x27BF, 0x00B7, 0x2022, 0x2026: true
        default: false
        }
    }

    /// Box drawing, block elements and Braille patterns (spinners) read as nothing.
    static func isDecoration(_ scalar: Unicode.Scalar) -> Bool {
        switch scalar.value {
        case 0x2500...0x259F, 0x2800...0x28FF: true
        default: false
        }
    }

    static func clean(_ raw: String) -> String {
        var out = String.UnicodeScalarView()
        var pendingSpace = false
        for scalar in raw.unicodeScalars {
            if isDecoration(scalar) || scalar.properties.isWhitespace {
                pendingSpace = true
                continue
            }
            if pendingSpace && !out.isEmpty { out.append(" ") }
            pendingSpace = false
            out.append(scalar)
        }
        return String(out)
    }
}

/// The words VoiceOver hears that are not terminal text; the app passes its localized ones.
public struct AccessibilityWords: Equatable, Sendable {
    public var cursorLine: String
    public var blank: String

    public init(cursorLine: String, blank: String) {
        self.cursorLine = cursorLine
        self.blank = blank
    }

    public static let english = AccessibilityWords(cursorLine: "Cursor line", blank: "blank")
}

/// The live screen as the output feed reads it.
public struct TerminalScreen: Equatable, Sendable {
    public var rows: [String]
    /// A full-screen app (vim, less, htop, a TUI) has switched to the alternate screen.
    public var isAlternate: Bool

    public init(rows: [String], isAlternate: Bool = false) {
        self.rows = rows
        self.isAlternate = isAlternate
    }
}
