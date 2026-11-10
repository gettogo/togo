/// Reads the unified diff the helper sends into files, hunks and lines, with word-level
/// changes marked. Input may be cut anywhere; whatever arrived is kept and nothing traps.
public enum DiffParser {
    public static func parse(_ diff: GitDiff) -> [DiffFile] {
        var parser = Parser()
        var pieces = diff.diff.utf8.split(separator: 0x0A, omittingEmptySubsequences: false)
        // A final newline ends the last line; it does not start an empty one.
        if pieces.last?.isEmpty == true { pieces.removeLast() }
        for piece in pieces {
            let (line, hadCR) = Lines.stripCR(Substring(piece))
            parser.feed(line, endsWithCR: hadCR)
        }
        var files = parser.finish()

        let binary = Set(diff.binary)
        var seen: Set<String> = []
        for index in files.indices {
            let paths = [files[index].oldPath, files[index].newPath].compactMap { $0 }
            seen.formUnion(paths)
            if paths.contains(where: binary.contains) { files[index].isBinary = true }
        }
        for path in diff.binary where !seen.contains(path) {
            seen.insert(path)
            files.append(DiffFile(oldPath: path, newPath: path, change: .modified, isBinary: true))
        }
        for file in files.indices {
            for hunk in files[file].hunks.indices { WordDiff.mark(&files[file].hunks[hunk]) }
        }
        return files
    }
}

private struct Parser {
    private struct Pending {
        var file = DiffFile(oldPath: nil, newPath: nil, change: .modified)
        var oldNull = false
        var newNull = false
        var created = false
        var deleted = false
        var renamed = false
        var copied = false
        var similarity: Int?
        var modeChanged = false
    }

    private var files: [DiffFile] = []
    private var pending: Pending?
    private var hunk: DiffHunk?
    private var oldLeft = 0
    private var newLeft = 0
    /// Lines still due from each parent after the first, in a combined (`diff --cc`) hunk;
    /// empty in an ordinary one.
    private var otherLeft: [Int] = []
    private var oldLine = 0
    private var newLine = 0

    private var endsWithCR = false

    mutating func feed(_ line: Substring, endsWithCR: Bool) {
        self.endsWithCR = endsWithCR
        if hunk != nil, body(line) { return }
        closeHunk()
        header(line)
    }

    mutating func finish() -> [DiffFile] {
        closeFile()
        return files
    }

    /// Adds a line to the open hunk. False when the line does not belong to it.
    private mutating func body(_ line: Substring) -> Bool {
        if !otherLeft.isEmpty { return combinedBody(line) }
        let first = line.utf8.first
        switch first {
        case UInt8(ascii: " ") where oldLeft > 0 || newLeft > 0, nil where oldLeft > 0 && newLeft > 0:
            append(.context, line, old: true, new: true)
            oldLeft = max(oldLeft - 1, 0)
            newLeft = max(newLeft - 1, 0)
        case UInt8(ascii: "-") where oldLeft > 0:
            append(.removed, line, old: true, new: false)
            oldLeft -= 1
            pending?.file.deletions += 1
        case UInt8(ascii: "+") where newLeft > 0:
            append(.added, line, old: false, new: true)
            newLeft -= 1
            pending?.file.additions += 1
        case UInt8(ascii: "\\") where hunk?.lines.isEmpty == false:
            noNewline(line)
        default:
            return false
        }
        return true
    }

    private mutating func noNewline(_ line: Substring) {
        var text = Self.dropMarks(line, 1)
        if text.utf8.first == UInt8(ascii: " ") { text = Self.dropMarks(text, 1) }
        let cut = Lines.clip(text)
        hunk?.lines.append(DiffLine(kind: .noNewline, text: cut.text, clipped: cut.clipped))
    }

    /// A line of a combined hunk: one mark per parent, `-` where a parent had a line the
    /// result lacks, `+` where the result has a line that parent lacked. The first parent's
    /// numbers are the old side.
    private mutating func combinedBody(_ line: Substring) -> Bool {
        let columns = otherLeft.count + 1
        let marks = Array(line.utf8.prefix(columns))
        if marks.first == UInt8(ascii: "\\") {
            guard hunk?.lines.isEmpty == false else { return false }
            noNewline(line)
            return true
        }
        guard marks.count == columns else { return false }
        var added = false
        var removed = false
        for mark in marks {
            switch mark {
            case UInt8(ascii: " "): break
            case UInt8(ascii: "+"): added = true
            case UInt8(ascii: "-"): removed = true
            default: return false
            }
        }
        guard !(added && removed) else { return false }
        // A removed line is in the parents marked `-`; any other line in those marked ` `.
        let held = marks.map { $0 == UInt8(ascii: removed ? "-" : " ") }
        guard removed || newLeft > 0, !held[0] || oldLeft > 0 else { return false }
        for index in otherLeft.indices where held[index + 1] && otherLeft[index] == 0 { return false }

        let kind: DiffLineKind = removed ? .removed : (added ? .added : .context)
        let cut = Lines.clip(Self.dropMarks(line, columns))
        hunk?.lines.append(DiffLine(
            kind: kind, text: cut.text, oldNumber: held[0] && kind != .added ? oldLine : nil,
            newNumber: removed ? nil : newLine, clipped: cut.clipped, endsWithCR: endsWithCR
        ))
        if held[0] { oldLine += 1; oldLeft -= 1 }
        if !removed { newLine += 1; newLeft -= 1 }
        for index in otherLeft.indices where held[index + 1] { otherLeft[index] -= 1 }
        if kind == .added { pending?.file.additions += 1 }
        if kind == .removed { pending?.file.deletions += 1 }
        return true
    }

    /// The text after a line's leading ASCII marks. Dropped as bytes, not characters: a
    /// combining mark or joiner opening the text clusters with the mark before it.
    private static func dropMarks(_ line: Substring, _ count: Int) -> Substring {
        Substring(line.utf8.dropFirst(count))
    }

    /// The rest of `line` after `prefix`, compared as bytes for the same reason.
    private static func after(_ prefix: String, in line: Substring) -> Substring? {
        guard line.utf8.starts(with: prefix.utf8) else { return nil }
        return Substring(line.utf8.dropFirst(prefix.utf8.count))
    }

    private mutating func append(_ kind: DiffLineKind, _ line: Substring, old: Bool, new: Bool) {
        let cut = Lines.clip(Self.dropMarks(line, 1))
        hunk?.lines.append(DiffLine(
            kind: kind, text: cut.text, oldNumber: old ? oldLine : nil, newNumber: new ? newLine : nil,
            clipped: cut.clipped, endsWithCR: endsWithCR
        ))
        if old { oldLine += 1 }
        if new { newLine += 1 }
    }

    private mutating func header(_ line: Substring) {
        if let rest = Self.after("diff --git ", in: line) {
            closeFile()
            var next = Pending()
            if let paths = DiffPaths.gitHeader(rest) {
                next.file.oldPath = paths.old
                next.file.newPath = paths.new
            }
            pending = next
            return
        }
        // A file with unresolved conflicts diffs as a combined diff: one name, no prefix.
        if let rest = Self.after("diff --cc ", in: line) ?? Self.after("diff --combined ", in: line) {
            closeFile()
            var next = Pending()
            let path = DiffPaths.plain(rest)
            next.file.oldPath = path
            next.file.newPath = path
            pending = next
            return
        }
        if Self.after("--- ", in: line) != nil, pending == nil || pending?.file.hunks.isEmpty == false {
            closeFile()
            pending = Pending()
        }
        guard pending != nil else { return }

        if line.utf8.first == UInt8(ascii: "@"), line.utf8.dropFirst().first == UInt8(ascii: "@") {
            openHunk(line)
        } else if let rest = Self.after("--- ", in: line) {
            let path = DiffPaths.side(rest, prefix: "a/")
            if let path { pending?.file.oldPath = path } else { pending?.oldNull = true }
        } else if let rest = Self.after("+++ ", in: line) {
            let path = DiffPaths.side(rest, prefix: "b/")
            if let path { pending?.file.newPath = path } else { pending?.newNull = true }
        } else if let rest = Self.after("rename from ", in: line) {
            pending?.renamed = true
            pending?.file.oldPath = DiffPaths.plain(rest)
        } else if let rest = Self.after("rename to ", in: line) {
            pending?.renamed = true
            pending?.file.newPath = DiffPaths.plain(rest)
        } else if let rest = Self.after("copy from ", in: line) {
            pending?.copied = true
            pending?.file.oldPath = DiffPaths.plain(rest)
        } else if let rest = Self.after("copy to ", in: line) {
            pending?.copied = true
            pending?.file.newPath = DiffPaths.plain(rest)
        } else if var digits = Self.after("similarity index ", in: line) {
            if digits.hasSuffix("%") { digits = digits.dropLast() }
            pending?.similarity = Int(digits).map { min(max($0, 0), 100) }
        } else if line.hasPrefix("new file mode") {
            pending?.created = true
        } else if line.hasPrefix("deleted file mode") {
            pending?.deleted = true
        } else if line.hasPrefix("old mode") || line.hasPrefix("new mode") {
            pending?.modeChanged = true
        } else if line.hasPrefix("Binary files ") || line.hasPrefix("GIT binary patch") {
            pending?.file.isBinary = true
        }
    }

    private mutating func openHunk(_ line: Substring) {
        guard let range = HunkRange(line) else { return }
        let cut = Lines.clip(line)
        hunk = DiffHunk(
            header: cut.text, oldStart: range.oldStart, oldCount: range.oldCount,
            newStart: range.newStart, newCount: range.newCount, section: range.section, lines: []
        )
        oldLeft = range.oldCount
        newLeft = range.newCount
        otherLeft = range.otherCounts
        oldLine = range.oldStart
        newLine = range.newStart
    }

    private mutating func closeHunk() {
        guard let done = hunk else { return }
        pending?.file.hunks.append(done)
        hunk = nil
        oldLeft = 0
        newLeft = 0
        otherLeft = []
    }

    private mutating func closeFile() {
        closeHunk()
        guard let next = pending else { return }
        pending = nil
        var file = next.file
        if next.created || (next.oldNull && !next.newNull) {
            file.change = .added
            file.oldPath = nil
        } else if next.deleted || (next.newNull && !next.oldNull) {
            file.change = .deleted
            file.newPath = nil
        } else if next.renamed {
            file.change = .renamed(similarity: next.similarity)
        } else if next.copied {
            file.change = .copied
        } else if next.modeChanged, file.hunks.isEmpty, !file.isBinary {
            file.change = .modeOnly
        }
        files.append(file)
    }
}

/// The numbers and section of a `@@ -a,b +c,d @@ section` line, or of a combined diff's
/// `@@@ -a,b -c,d +e,f @@@ section`, which has one old range per parent.
private struct HunkRange {
    var oldStart = 0, oldCount = 1, newStart = 0, newCount = 1
    /// The line counts of the parents after the first; empty for an ordinary hunk.
    var otherCounts: [Int] = []
    var section: String?

    /// Numbers past this are clamped so line arithmetic never overflows.
    private static let ceiling = 1_000_000_000
    /// Git combines at most this many parents; a longer run of `@` is not a header.
    private static let maxParents = 64

    init?(_ line: Substring) {
        var bytes = line.utf8[...]
        let ats = bytes.prefix { $0 == UInt8(ascii: "@") }.count
        guard ats >= 2, ats <= Self.maxParents + 1 else { return nil }
        bytes = bytes.dropFirst(ats)
        for parent in 0..<(ats - 1) {
            guard Self.take(" -", &bytes), let start = Self.number(&bytes) else { return nil }
            var count = 1
            if Self.take(",", &bytes) {
                guard let value = Self.number(&bytes) else { return nil }
                count = value
            }
            if parent == 0 {
                oldStart = start
                oldCount = count
            } else {
                otherCounts.append(count)
            }
        }
        guard Self.take(" +", &bytes), let new = Self.number(&bytes) else { return nil }
        newStart = new
        if Self.take(",", &bytes) {
            guard let count = Self.number(&bytes) else { return nil }
            newCount = count
        }
        guard Self.take(" " + String(repeating: "@", count: ats), &bytes) else { return nil }
        let rest = Substring(bytes).drop { $0 == " " }
        section = rest.isEmpty ? nil : Lines.clip(rest).text
    }

    private static func take(_ literal: String, _ bytes: inout Substring.UTF8View.SubSequence) -> Bool {
        guard bytes.starts(with: literal.utf8) else { return false }
        bytes = bytes.dropFirst(literal.utf8.count)
        return true
    }

    private static func number(_ bytes: inout Substring.UTF8View.SubSequence) -> Int? {
        var value = 0
        var digits = 0
        while let byte = bytes.first, (0x30...0x39).contains(byte) {
            value = min(value * 10 + Int(byte - 0x30), ceiling)
            digits += 1
            bytes = bytes.dropFirst()
        }
        return digits > 0 ? value : nil
    }
}
