import Foundation
import Synchronization

/// Reply markdown split into prose, headings and fenced code. Prose keeps its inline markdown
/// for the view to style; list bullets become "•".
///
/// A streamed reply only grows, and every block but the last is finished once a later block
/// began, so `continued(to:)` re-reads from the start of the last block instead of from the top.
public struct MarkdownBlocks: Sendable, Equatable {
    public enum Block: Hashable, Sendable {
        case prose(String)
        case heading(String)
        case code(language: String?, text: String)
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
        var proseStart = 0
        var code: [Substring] = []
        var codeStart = 0
        var language: String?
        var inCode = false

        func offset(_ i: String.UTF8View.Index) -> Int { utf8.distance(from: utf8.startIndex, to: i) }
        func emit(_ block: Block, at start: Int) {
            blocks.append(block)
            starts.append(start)
        }
        func flushProse() {
            let joined = prose.joined(separator: "\n").trimmingCharacters(in: .newlines)
            if !joined.isEmpty { emit(.prose(joined), at: proseStart) }
            prose = []
        }

        var lineStart = from
        while true {
            let lineEnd = utf8[lineStart...].firstIndex(of: UInt8(ascii: "\n")) ?? utf8.endIndex
            let line = text[lineStart..<lineEnd]
            let start = offset(lineStart)
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if trimmed.hasPrefix("```") {
                if inCode {
                    emit(.code(language: language, text: code.joined(separator: "\n")), at: codeStart)
                    code = []
                    language = nil
                } else {
                    flushProse()
                    let tag = trimmed.dropFirst(3).trimmingCharacters(in: .whitespaces)
                    language = tag.isEmpty ? nil : tag
                    codeStart = start
                }
                inCode.toggle()
            } else if inCode {
                code.append(line)
            } else if let heading = Self.heading(trimmed) {
                flushProse()
                emit(.heading(heading), at: start)
            } else {
                if prose.isEmpty { proseStart = start }
                prose.append(Self.bullet(line))
            }
            guard lineEnd < utf8.endIndex else { break }
            lineStart = utf8.index(after: lineEnd)
        }
        // A reply still streaming may stop inside a fence; show what came so far as code.
        if inCode { emit(.code(language: language, text: code.joined(separator: "\n")), at: codeStart) }
        flushProse()
    }

    private static func heading(_ line: String) -> String? {
        guard line.hasPrefix("#") else { return nil }
        let text = line.drop { $0 == "#" }
        guard text.first == " " else { return nil }
        return text.trimmingCharacters(in: .whitespaces)
    }

    private static func bullet(_ line: Substring) -> Substring {
        let indent = line.prefix { $0 == " " }
        let rest = line.dropFirst(indent.count)
        guard rest.hasPrefix("- ") || rest.hasPrefix("* ") else { return line }
        return Substring(String(indent) + "\u{2022} " + String(rest.dropFirst(2)))
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
