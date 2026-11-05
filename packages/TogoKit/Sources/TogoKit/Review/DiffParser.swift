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
            var text = line.dropFirst()
            if text.utf8.first == UInt8(ascii: " ") { text = text.dropFirst() }
            let cut = Lines.clip(text)
            hunk?.lines.append(DiffLine(kind: .noNewline, text: cut.text, clipped: cut.clipped))
        default:
            return false
        }
        return true
    }

    private mutating func append(_ kind: DiffLineKind, _ line: Substring, old: Bool, new: Bool) {
        let cut = Lines.clip(line.isEmpty ? line : line.dropFirst())
        hunk?.lines.append(DiffLine(
            kind: kind, text: cut.text, oldNumber: old ? oldLine : nil, newNumber: new ? newLine : nil,
            clipped: cut.clipped, endsWithCR: endsWithCR
        ))
        if old { oldLine += 1 }
        if new { newLine += 1 }
    }

    private mutating func header(_ line: Substring) {
        if line.hasPrefix("diff --git ") {
            closeFile()
            var next = Pending()
            if let paths = DiffPaths.gitHeader(line.dropFirst(11)) {
                next.file.oldPath = paths.old
                next.file.newPath = paths.new
            }
            pending = next
            return
        }
        if line.hasPrefix("--- "), pending == nil || pending?.file.hunks.isEmpty == false {
            closeFile()
            pending = Pending()
        }
        guard pending != nil else { return }

        if line.hasPrefix("@@") {
            openHunk(line)
        } else if line.hasPrefix("--- ") {
            let path = DiffPaths.side(line.dropFirst(4), prefix: "a/")
            if let path { pending?.file.oldPath = path } else { pending?.oldNull = true }
        } else if line.hasPrefix("+++ ") {
            let path = DiffPaths.side(line.dropFirst(4), prefix: "b/")
            if let path { pending?.file.newPath = path } else { pending?.newNull = true }
        } else if line.hasPrefix("rename from ") {
            pending?.renamed = true
            pending?.file.oldPath = DiffPaths.plain(line.dropFirst(12))
        } else if line.hasPrefix("rename to ") {
            pending?.renamed = true
            pending?.file.newPath = DiffPaths.plain(line.dropFirst(10))
        } else if line.hasPrefix("copy from ") {
            pending?.copied = true
            pending?.file.oldPath = DiffPaths.plain(line.dropFirst(10))
        } else if line.hasPrefix("copy to ") {
            pending?.copied = true
            pending?.file.newPath = DiffPaths.plain(line.dropFirst(8))
        } else if line.hasPrefix("similarity index ") {
            var digits = line.dropFirst(17)
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
        oldLine = range.oldStart
        newLine = range.newStart
    }

    private mutating func closeHunk() {
        guard let done = hunk else { return }
        pending?.file.hunks.append(done)
        hunk = nil
        oldLeft = 0
        newLeft = 0
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

/// The numbers and section of a `@@ -a,b +c,d @@ section` line.
private struct HunkRange {
    var oldStart = 0, oldCount = 1, newStart = 0, newCount = 1
    var section: String?

    /// Numbers past this are clamped so line arithmetic never overflows.
    private static let ceiling = 1_000_000_000

    init?(_ line: Substring) {
        var bytes = line.utf8[...]
        guard Self.take("@@ -", &bytes), let old = Self.number(&bytes) else { return nil }
        oldStart = old
        if Self.take(",", &bytes) {
            guard let count = Self.number(&bytes) else { return nil }
            oldCount = count
        }
        guard Self.take(" +", &bytes), let new = Self.number(&bytes) else { return nil }
        newStart = new
        if Self.take(",", &bytes) {
            guard let count = Self.number(&bytes) else { return nil }
            newCount = count
        }
        guard Self.take(" @@", &bytes) else { return nil }
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
