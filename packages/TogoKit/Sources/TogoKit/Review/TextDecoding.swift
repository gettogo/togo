import Foundation

/// Turns file contents read from the helper into lines for the code viewer.
public enum TextDecoding {
    /// Splits UTF-8 content into lines. Returns nil for base64 or unknown encodings and for
    /// content holding a NUL byte, which the viewer shows as binary. Replacement characters
    /// the helper inserted for invalid UTF-8 stay as they are; tabs stay for the renderer.
    public static func file(_ result: FsReadResult) -> TextFile? {
        guard result.encoding == .utf8, !result.content.utf8.contains(0) else { return nil }
        let split = Lines.split(result.content)
        var lines: [String] = []
        lines.reserveCapacity(split.lines.count)
        var clipped: Set<Int> = []
        for (index, line) in split.lines.enumerated() {
            let cut = Lines.clip(line)
            if cut.clipped { clipped.insert(index) }
            lines.append(cut.text)
        }
        return TextFile(lines: lines, lineEnding: split.ending, clipped: clipped)
    }

    /// An untracked file shown as a diff: one hunk of added lines.
    public static func addedFile(path: String, _ result: FsReadResult) -> DiffFile {
        guard let text = file(result) else {
            return DiffFile(oldPath: nil, newPath: path, change: .added, isBinary: true)
        }
        if result.content.isEmpty {
            return DiffFile(oldPath: nil, newPath: path, change: .added)
        }
        var lines: [DiffLine] = []
        lines.reserveCapacity(text.lines.count + 1)
        for (index, line) in text.lines.enumerated() {
            lines.append(DiffLine(
                kind: .added, text: line, newNumber: index + 1, clipped: text.clipped.contains(index)
            ))
        }
        if !result.truncated && !result.content.hasSuffix("\n") {
            lines.append(DiffLine(kind: .noNewline, text: Lines.noNewlineText))
        }
        let count = text.lines.count
        let hunk = DiffHunk(
            header: "@@ -0,0 +1,\(count) @@", oldStart: 0, oldCount: 0, newStart: 1, newCount: count, lines: lines
        )
        return DiffFile(oldPath: nil, newPath: path, change: .added, hunks: [hunk], additions: count)
    }
}

/// Line splitting and clipping shared by the diff parser and file decoding.
enum Lines {
    static let noNewlineText = "No newline at end of file"

    /// Splits on `\n` and strips one trailing `\r` per line. A final newline does not start
    /// an extra line; empty content is one empty line.
    static func split(_ text: String) -> (lines: [Substring], ending: LineEnding) {
        var lines: [Substring] = []
        var crlf = 0
        var lf = 0
        let pieces = text.utf8.split(separator: 0x0A, omittingEmptySubsequences: false)
        let terminated = text.utf8.last == 0x0A
        lines.reserveCapacity(pieces.count)
        for (index, piece) in pieces.enumerated() {
            let isLast = index == pieces.count - 1
            if isLast && terminated && pieces.count > 1 { break }
            let (line, hadCR) = stripCR(Substring(piece))
            if !isLast || terminated {
                if hadCR { crlf += 1 } else { lf += 1 }
            }
            lines.append(line)
        }
        let ending: LineEnding = crlf > 0 && lf > 0 ? .mixed : (crlf > 0 ? .crlf : .lf)
        return (lines, ending)
    }

    static func stripCR(_ line: Substring) -> (Substring, Bool) {
        guard line.utf8.last == 0x0D else { return (line, false) }
        return (line[..<line.utf8.index(before: line.utf8.endIndex)], true)
    }

    /// Cuts a line at `ReviewLimits.maxColumns` UTF-16 units without splitting a surrogate pair.
    static func clip(_ line: Substring) -> (text: String, clipped: Bool) {
        let limit = ReviewLimits.maxColumns
        guard line.utf8.count > limit else { return (String(line), false) }
        let units = line.utf16
        guard var end = units.index(units.startIndex, offsetBy: limit, limitedBy: units.endIndex),
              end != units.endIndex
        else { return (String(line), false) }
        if UTF16.isTrailSurrogate(units[end]) {
            end = units.index(before: end)
        }
        return (String(line.unicodeScalars[..<end]), true)
    }
}
