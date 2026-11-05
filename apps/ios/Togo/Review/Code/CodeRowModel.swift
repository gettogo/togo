import Foundation
import TogoKit

/// One row of code or diff text, as `CodeLinesView` draws it. Word ranges and token ranges are
/// UTF-16 offsets into `text`, which holds no `+`/`-` prefix and no trailing `\r`.
enum CodeRow: Hashable, Sendable {
    case line(
        number: Int?, oldNumber: Int?, text: String, kind: DiffLineKind?, tokens: [TokenSpan], changes: [Range<Int>],
        clipped: Bool
    )
    case split(left: CodeRowSide?, right: CodeRowSide?)
    case hunk(String)
    /// A sentence in place of content, such as "Binary file, not shown".
    case notice(String)
}

/// One half of a side-by-side row.
struct CodeRowSide: Hashable, Sendable {
    var number: Int?
    var text: String
    var kind: DiffLineKind
    var tokens: [TokenSpan]
    var changes: [Range<Int>]
}

/// Words the code view shows or speaks, under `review.diff.`.
enum CodeCopy {
    static let clipped = String(localized: "review.diff.clipped", defaultValue: "cut at 2,000 characters")
    static let noNewline = String(localized: "review.diff.noNewline", defaultValue: "No newline at end of file")
    static let copyLine = String(localized: "review.diff.copyLine", defaultValue: "Copy line")
    static let comment = String(localized: "review.diff.comment", defaultValue: "Comment on line")
    static let blank = String(localized: "review.diff.blank", defaultValue: "blank")
    /// Under a side-by-side pair whose text was cut, which has no room for the inline marker.
    static let clippedNotice = String(
        localized: "review.diff.clippedNotice", defaultValue: "This line is cut at 2,000 characters."
    )

    /// Shown under a line whose only change is its ending.
    static func lineEnding(fromCRLF: Bool) -> String {
        fromCRLF
            ? String(localized: "review.diff.lineEndingToLF", defaultValue: "Line ending changed (CRLF -> LF)")
            : String(localized: "review.diff.lineEndingToCRLF", defaultValue: "Line ending changed (LF -> CRLF)")
    }

    static func line(_ kind: DiffLineKind?, number: Int?) -> String {
        let number = number.map { "\($0)" }
        switch (kind, number) {
        case (.added, let n?): return String(localized: "review.diff.addedLine", defaultValue: "Added line \(n)")
        case (.added, nil): return String(localized: "review.diff.added", defaultValue: "Added line")
        case (.removed, let n?): return String(localized: "review.diff.removedLine", defaultValue: "Removed line \(n)")
        case (.removed, nil): return String(localized: "review.diff.removed", defaultValue: "Removed line")
        case (_, let n?): return String(localized: "review.diff.lineNumber", defaultValue: "Line \(n)")
        case (_, nil): return String(localized: "review.diff.line", defaultValue: "Line")
        }
    }

    static func lines(from start: Int, count: Int) -> String {
        count <= 1
            ? String(localized: "review.diff.hunkOne", defaultValue: "Changes at line \(start)")
            : String(localized: "review.diff.hunkMany", defaultValue: "Changes at lines \(start) to \(start + count - 1)")
    }
}

extension CodeRow {
    var isHunk: Bool {
        if case .hunk = self { true } else { false }
    }

    /// What VoiceOver reads for the row: kind, number, then the text.
    var accessibilityLabel: String {
        switch self {
        case let .line(number, oldNumber, text, kind, _, _, clipped):
            if kind == .noNewline { return CodeCopy.noNewline }
            let shown = kind == .removed ? (oldNumber ?? number) : (number ?? oldNumber)
            return Self.spoken(kind: kind, number: shown, text: text, clipped: clipped)
        case let .split(left, right):
            if let left, let right, left.kind == .context, right.kind == .context {
                return Self.spoken(kind: .context, number: right.number, text: right.text, clipped: false)
            }
            return [left, right].compactMap { side in
                side.map { Self.spoken(kind: $0.kind, number: $0.number, text: $0.text, clipped: false) }
            }.joined(separator: ". ")
        case let .hunk(header):
            return Self.hunkLabel(header)
        case let .notice(text):
            return text.replacingOccurrences(of: " -> ", with: " to ")
        }
    }

    /// The text "Copy line" puts on the pasteboard: the line as the file has it, without the
    /// markers shown for hidden characters.
    var copyText: String {
        switch self {
        case let .line(_, _, text, _, tokens, _, _): HiddenCharacters.raw(text, tokens: tokens)
        case let .split(left, right): (right ?? left).map { HiddenCharacters.raw($0.text, tokens: $0.tokens) } ?? ""
        case let .hunk(text), let .notice(text): text
        }
    }

    private static func spoken(kind: DiffLineKind?, number: Int?, text: String, clipped: Bool) -> String {
        if kind == .noNewline { return CodeCopy.noNewline }
        let body = text.allSatisfy(\.isWhitespace) ? CodeCopy.blank : HiddenCharacters.spoken(text)
        let label = "\(CodeCopy.line(kind, number: number)): \(body)"
        return clipped ? "\(label), \(CodeCopy.clipped)" : label
    }

    /// "@@ -10,7 +10,8 @@ export function routes" reads as "Changes at lines 10 to 17, export function routes".
    static func hunkLabel(_ header: String) -> String {
        let parts = header.split(separator: " ", maxSplits: 3, omittingEmptySubsequences: true)
        guard parts.count >= 3, parts[0] == "@@", parts[2].hasPrefix("+") else { return header }
        let range = parts[2].dropFirst().split(separator: ",")
        guard let start = range.first.flatMap({ Int($0) }) else { return header }
        let count = range.count > 1 ? Int(range[1]) ?? 1 : 1
        var label = CodeCopy.lines(from: start, count: count)
        if parts.count == 4 {
            let section = parts[3].hasPrefix("@@") ? parts[3].dropFirst(2) : parts[3]
            let trimmed = section.trimmingCharacters(in: .whitespaces)
            if !trimmed.isEmpty { label += ", \(trimmed)" }
        }
        return label
    }
}

// MARK: - Building rows from a parsed diff

extension CodeRow {
    /// Rows for one file in a single column: a header per hunk, then its lines, with a notice
    /// under each pair of lines that differ only in their line ending.
    static func unified(_ file: DiffFile, tokens: (DiffLine) -> [TokenSpan] = { _ in [] }) -> [CodeRow] {
        var rows: [CodeRow] = []
        rows.reserveCapacity(file.hunks.reduce(0) { $0 + $1.lines.count + 1 })
        for hunk in file.hunks {
            rows.append(.hunk(hunk.header))
            for line in hunk.lines {
                rows.append(.line(
                    number: line.kind == .removed ? nil : line.newNumber, oldNumber: line.oldNumber, text: line.text,
                    kind: line.kind, tokens: tokens(line), changes: line.changes, clipped: line.clipped
                ))
                if line.kind == .added, line.lineEndingChanged {
                    // The removed twin carries the other ending.
                    rows.append(.notice(CodeCopy.lineEnding(fromCRLF: !line.endsWithCR)))
                }
            }
        }
        return rows
    }

    /// Rows for one file side by side, from `SplitPairing`.
    static func split(_ file: DiffFile, tokens: (DiffLine) -> [TokenSpan] = { _ in [] }) -> [CodeRow] {
        func side(_ line: DiffLine?, old: Bool) -> CodeRowSide? {
            line.map {
                CodeRowSide(
                    number: old ? $0.oldNumber : $0.newNumber, text: $0.text, kind: $0.kind, tokens: tokens($0),
                    changes: $0.changes
                )
            }
        }
        var rows: [CodeRow] = []
        for pair in SplitPairing.rows(file) {
            if let header = pair.hunkHeader {
                rows.append(.hunk(header))
                continue
            }
            rows.append(.split(left: side(pair.left, old: true), right: side(pair.right, old: false)))
            if pair.left?.clipped == true || pair.right?.clipped == true {
                rows.append(.notice(CodeCopy.clippedNotice))
            }
            if let left = pair.left, left.lineEndingChanged, pair.right?.lineEndingChanged == true {
                rows.append(.notice(CodeCopy.lineEnding(fromCRLF: left.endsWithCR)))
            }
        }
        return rows
    }

    /// Rows for a whole file, numbered from 1.
    static func file(_ file: TextFile, tokens: [[TokenSpan]] = []) -> [CodeRow] {
        file.lines.enumerated().map { index, text in
            .line(
                number: index + 1, oldNumber: nil, text: text, kind: nil,
                tokens: index < tokens.count ? tokens[index] : [], changes: [], clipped: file.clipped.contains(index)
            )
        }
    }
}
