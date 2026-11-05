import Foundation
import TogoKit
import TogoTerminal

/// One file of a diff, ready to draw in either layout.
struct PreparedDiffFile: Hashable, Sendable {
    var file: DiffFile
    var unified: [CodeRow]
    var split: [CodeRow]
    /// Rows that start a hunk, in each layout.
    var unifiedHunks: [Int]
    var splitHunks: [Int]
    /// Nothing to show but notices about the whole diff.
    var isEmpty: Bool

    init(file: DiffFile, unified: [CodeRow], split: [CodeRow], isEmpty: Bool) {
        self.file = file
        self.unified = unified
        self.split = split
        unifiedHunks = unified.indices.filter { unified[$0].isHunk }
        splitHunks = split.indices.filter { split[$0].isHunk }
        self.isEmpty = isEmpty
    }
}

/// Parsing and tokenizing for the diff screen. Pure functions of values, so the model runs them
/// away from the main actor.
enum DiffPreparation {
    /// A file whose permissions changed, as the last three octal digits.
    struct ModeChange: Hashable, Sendable {
        var old: String
        var new: String
    }

    static func prepare(_ diff: GitDiff) -> [PreparedDiffFile] {
        let files = DiffParser.parse(diff)
        let modes = modeChanges(diff.diff)
        let lead: [CodeRow] = diff.truncated ? [.notice(DiffCopy.truncated)] : []
        return files.enumerated().map { index, file in
            prepare(file, mode: index < modes.count ? modes[index] : nil, lead: lead)
        }
    }

    /// An untracked file read whole and shown as added lines.
    static func prepareAdded(path: String, _ result: FsReadResult) -> [PreparedDiffFile] {
        let lead: [CodeRow] = result.truncated ? [.notice(DiffCopy.truncated)] : []
        return [prepare(TextDecoding.addedFile(path: path, result), mode: nil, lead: lead)]
    }

    static func prepare(_ file: DiffFile, mode: ModeChange?, lead: [CodeRow]) -> PreparedDiffFile {
        if file.isBinary {
            let rows: [CodeRow] = [.notice(DiffCopy.binary)]
            return PreparedDiffFile(file: file, unified: rows, split: rows, isEmpty: false)
        }
        if file.hunks.isEmpty {
            let notice = emptyNotice(file, mode: mode)
            let rows = lead + (notice.map { [CodeRow.notice($0)] } ?? [])
            return PreparedDiffFile(file: file, unified: rows, split: rows, isEmpty: notice == nil)
        }
        let tokens = SideTokens(file)
        let unified = HiddenCharacters.reveal(CodeRow.unified(file, tokens: tokens.forUnified))
        let split = HiddenCharacters.reveal(CodeRow.split(file, tokens: tokens.forUnified).map(tokens.leftContextFromOldSide))
        return PreparedDiffFile(file: file, unified: lead + unified, split: lead + split, isEmpty: false)
    }

    private static func emptyNotice(_ file: DiffFile, mode: ModeChange?) -> String? {
        switch file.change {
        case .modeOnly: mode.map { DiffCopy.modeOnly(old: $0.old, new: $0.new) }
        case .renamed, .copied: file.oldPath.map(DiffCopy.renamedOnly)
        case .added: DiffCopy.emptyNewFile
        case .deleted: DiffCopy.emptyDeletedFile
        case .modified: nil
        }
    }

    /// Mode changes in the order of the diff's file headers, which is the parser's file order.
    static func modeChanges(_ text: String) -> [ModeChange?] {
        var result: [ModeChange?] = []
        var old: String?
        for line in text.split(separator: "\n", omittingEmptySubsequences: false) {
            if line.hasPrefix("diff --git ") {
                result.append(nil)
                old = nil
            } else if line.hasPrefix("old mode ") {
                old = String(line.dropFirst("old mode ".count).suffix(3))
            } else if line.hasPrefix("new mode "), let old, !result.isEmpty {
                result[result.count - 1] = ModeChange(old: old, new: String(line.dropFirst("new mode ".count).suffix(3)))
            } else if line.hasPrefix("@@") {
                old = nil
            }
        }
        return result
    }
}

/// Tokens for a file's lines. Removed and context lines are tokenized as the old file read them,
/// added and context lines as the new file reads them, each side in hunk order, so a comment or
/// string opened on one side colours only that side.
private struct SideTokens {
    private var old: [Int: [TokenSpan]] = [:]
    private var new: [Int: [TokenSpan]] = [:]

    init(_ file: DiffFile) {
        var firstLine: String?
        if let hunk = file.hunks.first, hunk.newStart <= 1 {
            firstLine = hunk.lines.first { $0.kind != .removed && $0.kind != .noNewline }?.text
        }
        let language = Language.detect(path: file.newPath ?? file.oldPath ?? "", firstLine: firstLine)
        guard language != .plain else { return }
        var oldLines: [(Int, String)] = []
        var newLines: [(Int, String)] = []
        for hunk in file.hunks {
            for line in hunk.lines {
                if line.kind == .context || line.kind == .removed, let number = line.oldNumber {
                    oldLines.append((number, line.text))
                }
                if line.kind == .context || line.kind == .added, let number = line.newNumber {
                    newLines.append((number, line.text))
                }
            }
        }
        old = Self.tokenize(oldLines, language)
        new = Self.tokenize(newLines, language)
    }

    private static func tokenize(_ lines: [(Int, String)], _ language: Language) -> [Int: [TokenSpan]] {
        let spans = Tokenizer.tokenize(lines.map(\.1), language: language)
        var result: [Int: [TokenSpan]] = [:]
        result.reserveCapacity(lines.count)
        for (index, line) in lines.enumerated() { result[line.0] = spans[index] }
        return result
    }

    /// The new side for context, since a single column reads as the file is now.
    func forUnified(_ line: DiffLine) -> [TokenSpan] {
        switch line.kind {
        case .removed: line.oldNumber.flatMap { old[$0] } ?? []
        case .added, .context: line.newNumber.flatMap { new[$0] } ?? []
        case .noNewline: []
        }
    }

    /// The left half of a context pair reads as the old file did.
    func leftContextFromOldSide(_ row: CodeRow) -> CodeRow {
        guard case .split(var left?, let right) = row, left.kind == .context else { return row }
        left.tokens = left.number.flatMap { old[$0] } ?? []
        return .split(left: left, right: right)
    }
}

/// Text bound for the session's terminal.
enum SessionText {
    /// Text as a terminal paste allows it, on one line: no control characters (so no escape
    /// sequence and no early end to a bracketed paste), no direction overrides and no
    /// invisible characters, and line breaks and tabs as spaces.
    static func clean(_ text: String) -> String {
        var scalars = String.UnicodeScalarView()
        for scalar in PasteText.sanitize(text).unicodeScalars where !isInvisible(scalar) {
            scalars.append(scalar.properties.generalCategory == .control ? " " : scalar)
        }
        return String(scalars).split(separator: " ", omittingEmptySubsequences: true).joined(separator: " ")
    }

    /// Bidirectional controls, zero-width characters and other format characters.
    private static func isInvisible(_ scalar: Unicode.Scalar) -> Bool {
        scalar.properties.generalCategory == .format || scalar.properties.isBidiControl
    }
}
