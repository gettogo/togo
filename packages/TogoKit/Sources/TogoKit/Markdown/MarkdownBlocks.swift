import Foundation
import Synchronization

/// Reply markdown split into prose, headings, fenced code and tables. Prose keeps its inline
/// markdown for the view to style; list bullets become "•" and task boxes "☐" and "☑".
///
/// A streamed reply only grows, and every block but the last is finished once a later block
/// began, so `continued(to:)` re-reads from the start of the last block instead of from the top.
public struct MarkdownBlocks: Sendable, Equatable {
    public enum Block: Hashable, Sendable {
        case prose(String)
        case heading(String)
        case code(language: String?, text: String)
        /// A GitHub-flavoured table: a header row, a delimiter row, then body rows.
        case table(MarkdownTable)
    }

    public private(set) var text: String
    public private(set) var blocks: [Block]
    /// UTF-8 offset in `text` of the line each block started at.
    private var starts: [Int]
    /// How many leading blocks the last `continued(to:)` kept; 0 after a whole parse.
    public private(set) var reparsedFrom: Int

    public static func parse(_ text: String) -> MarkdownBlocks {
        var out = MarkdownBlocks(text: text, blocks: [], starts: [], reparsedFrom: 0)
        out.read(text.utf8.startIndex, in: text)
        return out
    }

    /// The blocks of `next`. When `next` extends `text`, the finished blocks are kept and
    /// only the last one onward is read again.
    public func continued(to next: String) -> MarkdownBlocks {
        guard let lastStart = starts.last, next.utf8.count >= text.utf8.count, next.utf8.starts(with: text.utf8) else {
            return Self.parse(next)
        }
        var out = MarkdownBlocks(
            text: next, blocks: Array(blocks.dropLast()), starts: Array(starts.dropLast()), reparsedFrom: blocks.count - 1
        )
        out.read(next.utf8.index(next.utf8.startIndex, offsetBy: lastStart), in: next)
        return out
    }

    private init(text: String, blocks: [Block], starts: [Int], reparsedFrom: Int) {
        self.text = text
        self.blocks = blocks
        self.starts = starts
        self.reparsedFrom = reparsedFrom
    }

    /// Reads lines from `from`, which is the start of a line where no block is open.
    private mutating func read(_ from: String.UTF8View.Index, in text: String) {
        let utf8 = text.utf8
        var prose: [Substring] = []
        /// Where each prose line starts, so a table's header line can leave the prose.
        var proseStarts: [Int] = []
        var proseStart = 0
        var table: (header: [String], alignments: [MarkdownTable.Alignment], rows: [[String]], start: Int)?
        var code: [Substring] = []
        var codeStart = 0
        var language: String?
        var inCode = false
        /// The fence that opened the code, ``` or ~~~: only the same one closes it.
        var openFence = "```"
        /// Spaces before the opening fence; a fence inside a list item indents its code by as
        /// much, and that indent is the list's, not the code's.
        var fenceIndent = 0

        func offset(_ i: String.UTF8View.Index) -> Int { utf8.distance(from: utf8.startIndex, to: i) }
        func emit(_ block: Block, at start: Int) {
            blocks.append(block)
            starts.append(start)
        }
        func flushProse() {
            let joined = prose.joined(separator: "\n").trimmingCharacters(in: .newlines)
            if !joined.isEmpty { emit(.prose(joined), at: proseStarts.first ?? proseStart) }
            prose = []
            proseStarts = []
        }
        func flushTable() {
            guard let open = table else { return }
            emit(.table(MarkdownTable(header: open.header, alignments: open.alignments, rows: open.rows)), at: open.start)
            table = nil
        }

        var lineStart = from
        while true {
            let lineEnd = utf8[lineStart...].firstIndex(of: UInt8(ascii: "\n")) ?? utf8.endIndex
            let line = text[lineStart..<lineEnd]
            let start = offset(lineStart)
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if table != nil, !inCode {
                // A table runs until a blank line or a line with no pipe.
                if !trimmed.isEmpty, let cells = MarkdownTable.cells(trimmed) {
                    table?.rows.append(cells)
                    guard lineEnd < utf8.endIndex else { break }
                    lineStart = utf8.index(after: lineEnd)
                    continue
                }
                flushTable()
            }
            if let fence = Self.fence(trimmed), !inCode || fence == openFence {
                if inCode {
                    emit(.code(language: language, text: code.joined(separator: "\n")), at: codeStart)
                    code = []
                    language = nil
                } else {
                    flushProse()
                    openFence = fence
                    let tag = trimmed.dropFirst(3).trimmingCharacters(in: .whitespacesAndNewlines)
                    language = tag.isEmpty ? nil : tag
                    fenceIndent = line.prefix { $0 == " " }.count
                    codeStart = start
                }
                inCode.toggle()
            } else if inCode {
                code.append(Self.codeLine(line, dropping: fenceIndent))
            } else if let heading = Self.heading(trimmed) {
                flushProse()
                emit(.heading(heading), at: start)
            } else if let alignments = MarkdownTable.alignments(trimmed), let headerLine = prose.last,
                      let header = MarkdownTable.cells(headerLine), header.count == alignments.count {
                // The line before a delimiter row was the table's header, not prose.
                prose.removeLast()
                let headerStart = proseStarts.removeLast()
                flushProse()
                table = (header, alignments, [], headerStart)
            } else if Self.isSetextUnderline(trimmed), let title = prose.last,
                      !title.trimmingCharacters(in: .whitespaces).isEmpty, !Self.isListItem(title) {
                // "Title" over "===" or "---" is a heading.
                prose.removeLast()
                let titleStart = proseStarts.removeLast()
                flushProse()
                emit(.heading(title.trimmingCharacters(in: .whitespaces)), at: titleStart)
            } else {
                if prose.isEmpty { proseStart = start }
                prose.append(Self.isRule(trimmed) ? Self.rule : Self.inline(Self.bullet(Self.quote(line))))
                proseStarts.append(start)
            }
            guard lineEnd < utf8.endIndex else { break }
            lineStart = utf8.index(after: lineEnd)
        }
        // A reply still streaming may stop inside a fence; show what came so far as code.
        if inCode { emit(.code(language: language, text: code.joined(separator: "\n")), at: codeStart) }
        flushTable()
        flushProse()
    }

    /// A code line as the code has it: up to `indent` leading spaces (the fence's own indent)
    /// and a CRLF's carriage return removed, everything else kept byte for byte.
    static func codeLine(_ line: Substring, dropping indent: Int) -> Substring {
        var out = line
        if out.unicodeScalars.last == "\r" { out = Substring(out.unicodeScalars.dropLast()) }
        let spaces = min(indent, out.prefix { $0 == " " }.count)
        return out.dropFirst(spaces)
    }

    private static func heading(_ line: String) -> String? {
        guard line.hasPrefix("#") else { return nil }
        let text = line.drop { $0 == "#" }
        guard text.first == " " else { return nil }
        return text.trimmingCharacters(in: .whitespaces)
    }

    /// A list item's marker as it reads: "•" for `-`, `*` and `+`, and a task's box as "☐" or
    /// "☑" in its place. Ordered items keep their numbers; nesting keeps its indent.
    static func bullet(_ line: Substring) -> Substring {
        let indent = line.prefix { $0 == " " || $0 == "\t" }
        var rest = line.dropFirst(indent.count)
        let ordered = rest.prefix { $0.isNumber }
        if !ordered.isEmpty, rest.dropFirst(ordered.count).hasPrefix(". ") || rest.dropFirst(ordered.count).hasPrefix(") ") {
            let item = rest.dropFirst(ordered.count + 2)
            guard let box = taskBox(item) else { return line }
            return Substring(String(indent) + String(ordered) + ". " + box + String(item.dropFirst(4)))
        }
        guard rest.hasPrefix("- ") || rest.hasPrefix("* ") || rest.hasPrefix("+ ") else { return line }
        rest = rest.dropFirst(2)
        if let box = taskBox(rest) {
            return Substring(String(indent) + box + String(rest.dropFirst(4)))
        }
        return Substring(String(indent) + "\u{2022} " + String(rest))
    }

    static func fence(_ trimmed: String) -> String? {
        if trimmed.hasPrefix("```") { return "```" }
        if trimmed.hasPrefix("~~~") { return "~~~" }
        return nil
    }

    private static func isSetextUnderline(_ trimmed: String) -> Bool {
        trimmed.count >= 3 && (trimmed.allSatisfy { $0 == "=" } || trimmed.allSatisfy { $0 == "-" })
    }

    private static func isListItem(_ line: Substring) -> Bool {
        let rest = line.drop { $0 == " " || $0 == "\t" }
        return rest.hasPrefix("\u{2022} ") || rest.hasPrefix("\u{2610} ") || rest.hasPrefix("\u{2611} ")
    }

    /// A thematic break: three or more `-`, `*` or `_`, spaces allowed between.
    private static func isRule(_ trimmed: String) -> Bool {
        let marks = trimmed.filter { $0 != " " }
        guard marks.count >= 3, let first = marks.first, "-*_".contains(first) else { return false }
        return marks.allSatisfy { $0 == first }
    }

    /// A thematic break as it reads in the text: a line.
    static let rule: Substring = Substring(String(repeating: "\u{2500}", count: 16))

    /// A blockquote's `>` markers as bars, one per level, so a quote reads as a quote and a
    /// nested one as nested: "┃ ┃ text".
    static func quote(_ line: Substring) -> Substring {
        var rest = line.drop { $0 == " " }
        guard rest.first == ">" else { return line }
        var depth = 0
        while rest.first == ">" {
            depth += 1
            rest = rest.dropFirst().drop { $0 == " " }
        }
        return Substring(String(repeating: "\u{2503} ", count: depth) + String(rest))
    }

    /// Inline syntax the chat's inline parser leaves raw, outside code spans: an image becomes a
    /// link to it ("Image: alt"), a footnote mark `[^1]` reads "[1]".
    static func inline(_ line: Substring) -> Substring {
        guard line.contains("![") || line.contains("[^") else { return line }
        var out = ""
        for (index, part) in line.split(separator: "`", omittingEmptySubsequences: false).enumerated() {
            if index > 0 { out += "`" }
            guard index.isMultiple(of: 2) else {
                out += part
                continue
            }
            var text = String(part)
            let range = NSRange(text.startIndex..., in: text)
            text = image.stringByReplacingMatches(in: text, range: range, withTemplate: "[Image: $1]($2)")
                .replacingOccurrences(of: "[Image: ](", with: "[Image](")
            text = footnote.stringByReplacingMatches(in: text, range: NSRange(text.startIndex..., in: text), withTemplate: "\\[$1\\]")
            out += text
        }
        return Substring(out)
    }

    private static let image = try! NSRegularExpression(pattern: "!\\[([^\\]]*)\\]\\(([^)\\s]+)(?:\\s+\"[^\"]*\")?\\)")
    private static let footnote = try! NSRegularExpression(pattern: "\\[\\^([^\\]\\s]+)\\]")

    /// "☐ " or "☑ " for an item that starts with `[ ] ` or `[x] `.
    private static func taskBox(_ item: Substring) -> String? {
        if item.hasPrefix("[ ] ") { return "\u{2610} " }
        if item.hasPrefix("[x] ") || item.hasPrefix("[X] ") { return "\u{2611} " }
        return nil
    }
}

/// Recent parses, newest last. A text seen before is answered from here; a text that extends
/// a recent one (a streamed reply's next update) re-reads only its last block.
public final class MarkdownBlocksCache: Sendable {
    private struct State {
        var entries: [MarkdownBlocks] = []
        var hits = 0
        var lastReparsedFrom = 0
    }

    private let limit: Int
    /// Only the newest few are tried as prefixes; the rest answer exact repeats.
    private let prefixProbe = 6
    private let state = Mutex(State())

    public init(limit: Int = 64) {
        self.limit = max(1, limit)
    }

    public func blocks(_ text: String) -> [MarkdownBlocks.Block] {
        if let found = state.withLock({ s -> [MarkdownBlocks.Block]? in
            guard let at = s.entries.lastIndex(where: { $0.text == text }) else { return nil }
            s.hits += 1
            s.entries.append(s.entries.remove(at: at))
            return s.entries[s.entries.count - 1].blocks
        }) {
            return found
        }
        let base = state.withLock { s in
            s.entries.suffix(prefixProbe).last { text.utf8.count > $0.text.utf8.count && text.hasPrefix($0.text) && !$0.text.isEmpty }
        }
        let parsed = base?.continued(to: text) ?? MarkdownBlocks.parse(text)
        state.withLock { s in
            s.lastReparsedFrom = parsed.reparsedFrom
            // The prefix it grew from is replaced: only the newest text of a stream is shown again.
            if let base, let at = s.entries.lastIndex(where: { $0.text == base.text }) { s.entries.remove(at: at) }
            s.entries.append(parsed)
            if s.entries.count > limit { s.entries.removeFirst(s.entries.count - limit) }
        }
        return parsed.blocks
    }

    public func removeAll() {
        state.withLock { $0.entries.removeAll() }
    }

    var count: Int { state.withLock { $0.entries.count } }
    var hits: Int { state.withLock { $0.hits } }
    var lastReparsedFrom: Int { state.withLock { $0.lastReparsedFrom } }
}
