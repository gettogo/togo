import Foundation

// Value types for reading code and diffs. Parsing, word diffs, pairing and tokenizing
// build these elsewhere; this file holds only the shapes. Offsets are UTF-16.

public enum DiffLineKind: UInt8, Hashable, Sendable {
    case context, added, removed, noNewline
}

public struct DiffLine: Hashable, Sendable {
    public var kind: DiffLineKind
    public var text: String
    public var oldNumber: Int?
    public var newNumber: Int?
    /// UTF-16 offsets of the words that differ from the paired line.
    public var changes: [Range<Int>]
    /// The text was cut at `ReviewLimits.maxColumns`.
    public var clipped: Bool
    /// The line ended in `\r\n`; the `\r` is not part of `text`.
    public var endsWithCR: Bool
    /// The paired removed and added lines differ only in their line ending.
    public var lineEndingChanged: Bool

    public init(
        kind: DiffLineKind, text: String, oldNumber: Int? = nil, newNumber: Int? = nil,
        changes: [Range<Int>] = [], clipped: Bool = false, endsWithCR: Bool = false,
        lineEndingChanged: Bool = false
    ) {
        self.kind = kind
        self.text = text
        self.oldNumber = oldNumber
        self.newNumber = newNumber
        self.changes = changes
        self.clipped = clipped
        self.endsWithCR = endsWithCR
        self.lineEndingChanged = lineEndingChanged
    }
}

public struct DiffHunk: Hashable, Sendable {
    public var header: String
    public var oldStart: Int
    public var oldCount: Int
    public var newStart: Int
    public var newCount: Int
    /// The text after the second `@@`, usually the enclosing function.
    public var section: String?
    public var lines: [DiffLine]

    public init(
        header: String, oldStart: Int, oldCount: Int, newStart: Int, newCount: Int,
        section: String? = nil, lines: [DiffLine]
    ) {
        self.header = header
        self.oldStart = oldStart
        self.oldCount = oldCount
        self.newStart = newStart
        self.newCount = newCount
        self.section = section
        self.lines = lines
    }
}

public struct DiffFile: Hashable, Sendable, Identifiable {
    public enum Change: Hashable, Sendable {
        case added, deleted, modified
        case renamed(similarity: Int?)
        case copied, modeOnly
    }

    public var id: String { newPath ?? oldPath ?? "" }
    public var oldPath: String?
    public var newPath: String?
    public var change: Change
    public var isBinary: Bool
    public var hunks: [DiffHunk]
    public var additions: Int
    public var deletions: Int

    public init(
        oldPath: String?, newPath: String?, change: Change, isBinary: Bool = false,
        hunks: [DiffHunk] = [], additions: Int = 0, deletions: Int = 0
    ) {
        self.oldPath = oldPath
        self.newPath = newPath
        self.change = change
        self.isBinary = isBinary
        self.hunks = hunks
        self.additions = additions
        self.deletions = deletions
    }
}

/// One row of a side-by-side diff: a pair of lines, or a hunk header spanning both sides.
public struct SplitRow: Hashable, Sendable {
    public var left: DiffLine?
    public var right: DiffLine?
    public var hunkHeader: String?

    public init(left: DiffLine? = nil, right: DiffLine? = nil, hunkHeader: String? = nil) {
        self.left = left
        self.right = right
        self.hunkHeader = hunkHeader
    }
}

public enum TokenKind: UInt8, Hashable, Sendable {
    case plain, keyword, type, string, number, comment, punctuation, attribute
    /// A character that draws nothing, shown as its code by the viewer. The tokenizer never
    /// produces it.
    case hidden
}

public struct TokenSpan: Hashable, Sendable {
    public var range: Range<Int>
    public var kind: TokenKind

    public init(range: Range<Int>, kind: TokenKind) {
        self.range = range
        self.kind = kind
    }
}

public enum Language: String, CaseIterable, Sendable {
    case swift, go, typescript, javascript, python, rust, ruby, shell, json, yaml, toml
    case markdown, html, css, c, cpp, objc, java, kotlin, sql, plain
}

public enum LineEnding: Sendable {
    case lf, crlf, mixed
}

public struct TextFile: Hashable, Sendable {
    public var lines: [String]
    public var lineEnding: LineEnding
    /// Indexes of lines cut at `ReviewLimits.maxColumns`.
    public var clipped: Set<Int>

    public init(lines: [String], lineEnding: LineEnding = .lf, clipped: Set<Int> = []) {
        self.lines = lines
        self.lineEnding = lineEnding
        self.clipped = clipped
    }
}

public enum ReviewLimits {
    /// Longer lines are cut here and marked.
    public static let maxColumns = 2_000
    /// Lines longer than this skip the word-level diff.
    public static let wordDiffMaxLine = 500
    public static let tabWidth = 4
}
