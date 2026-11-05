import Foundation

/// Turns the structured text of an approval into something a notification, Live Activity or
/// the Watch can show: no box drawing, no menu cursors, no column alignment, few lines. The
/// helper applies the same rules; `protocol/fixtures/render/clean.json` holds the shared table.
public enum PromptText {
    private static let cursors = ["❯", "›", "▸", ">"]

    /// Bidirectional overrides, embeddings, isolates and marks, and zero-width characters.
    /// They can make a command read differently from the bytes that run, so they are removed
    /// from displayed text and refused in a subject.
    public static func isInvisible(_ scalar: Unicode.Scalar) -> Bool {
        switch scalar.value {
        case 0x202A...0x202E, 0x2066...0x2069, 0x200E, 0x200F, 0x061C, 0x200B...0x200D, 0x2060, 0xFEFF:
            true
        default:
            false
        }
    }

    /// Strips ANSI sequences, box drawing (U+2500-U+257F), block elements (U+2580-U+259F) and
    /// invisible characters, turns other control characters into spaces, drops a menu cursor
    /// at the start of a line, collapses runs of white space, drops empty lines and keeps at
    /// most `maxLines` lines.
    public static func clean(_ s: String, maxLines: Int = 3) -> String {
        var lines: [String] = []
        // Split on scalars: as Characters, "\r\n" is one grapheme and would not split on "\n".
        for raw in stripANSI(s).unicodeScalars.split(separator: "\n", omittingEmptySubsequences: false) {
            guard lines.count < maxLines else { break }
            let line = cleanLine(raw)
            if !line.isEmpty { lines.append(line) }
        }
        return lines.joined(separator: "\n")
    }

    private static func cleanLine(_ raw: Substring.UnicodeScalarView) -> String {
        var scalars = String.UnicodeScalarView()
        for scalar in raw {
            if (0x2500...0x259F).contains(scalar.value) || isInvisible(scalar) { continue }
            scalars.append(scalar.properties.generalCategory == .control ? " " : scalar)
        }
        var line = trimmed(String(scalars))
        if let cursor = cursors.first(where: { line.hasPrefix($0) }) {
            line.removeFirst(cursor.count)
        }
        return line.unicodeScalars
            .split(whereSeparator: { $0.properties.isWhitespace })
            .map { String(String.UnicodeScalarView($0)) }
            .joined(separator: " ")
    }

    private static func trimmed(_ s: String) -> String {
        let scalars = s.unicodeScalars
        guard let start = scalars.firstIndex(where: { !$0.properties.isWhitespace }),
              let end = scalars.lastIndex(where: { !$0.properties.isWhitespace })
        else { return "" }
        return String(scalars[start...end])
    }

    /// CSI sequences (`ESC [ … final`) and OSC sequences (`ESC ] … BEL` or `ESC \`).
    private static func stripANSI(_ s: String) -> String {
        guard s.unicodeScalars.contains("\u{1B}") else { return s }
        var out = String.UnicodeScalarView()
        var it = Array(s.unicodeScalars)[...]
        while let c = it.popFirst() {
            guard c == "\u{1B}", let kind = it.first, kind == "[" || kind == "]" else {
                out.append(c)
                continue
            }
            it.removeFirst()
            if kind == "[" {
                while let p = it.first, (0x30...0x3F).contains(p.value) { it.removeFirst() }
                while let p = it.first, (0x20...0x2F).contains(p.value) { it.removeFirst() }
                if let f = it.first, (0x40...0x7E).contains(f.value) { it.removeFirst() }
            } else {
                while let p = it.first, p != "\u{07}", p != "\u{1B}" { it.removeFirst() }
                if it.first == "\u{07}" {
                    it.removeFirst()
                } else if it.first == "\u{1B}", it.dropFirst().first == "\\" {
                    it.removeFirst(2)
                }
            }
        }
        return String(out)
    }
}
