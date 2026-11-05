import Foundation

/// A small syntax tokenizer. It scans each line once, front to back, with no
/// regular expressions, so the cost of a line is bounded by `ReviewLimits.maxColumns`.
/// Block comments and multi-line strings carry from one line to the next
/// through `State`, which lets a renderer tokenize only the lines on screen.
public enum Tokenizer {
    public struct State: Hashable, Sendable {
        enum Mode: Hashable, Sendable {
            case code
            case blockComment
            case multiLineString(String)
        }

        var mode: Mode = .code

        public init() {}
    }

    public static func tokenize(_ lines: [String], language: Language) -> [[TokenSpan]] {
        guard language != .plain else { return Array(repeating: [], count: lines.count) }
        var state = State()
        var result: [[TokenSpan]] = []
        result.reserveCapacity(lines.count)
        for text in lines {
            result.append(line(text, language: language, state: &state))
        }
        return result
    }

    /// Tokenizes one line. Spans are UTF-16 ranges; unclassified text has no span.
    /// Only the first `ReviewLimits.maxColumns` code units are scanned.
    public static func line(_ line: String, language: Language, state: inout State) -> [TokenSpan] {
        guard language != .plain else { return [] }
        let rules = LanguageRules.rules(for: language)
        let all = Array(line.utf16)
        var cap = ReviewLimits.maxColumns
        guard all.count > cap else {
            var scanner = LineScanner(rules: rules, language: language, units: all, state: state)
            scanner.run()
            state = scanner.state
            return scanner.spans
        }
        // Spans come from the capped prefix. The carried state comes from a spanless
        // scan of the whole line, so a comment or string that crosses the cap is not lost.
        if (0xD800...0xDBFF).contains(all[cap - 1]) { cap -= 1 }
        var display = LineScanner(rules: rules, language: language, units: Array(all[..<cap]), state: state)
        display.run()
        var carry = LineScanner(rules: rules, language: language, units: all, state: state, collectsSpans: false)
        carry.runStateOnly()
        state = carry.state
        return display.spans
    }
}

struct LineScanner {
    let rules: LanguageRules
    let language: Language
    let units: [UInt16]
    var state: Tokenizer.State
    var spans: [TokenSpan] = []
    private var index = 0
    private var inTag = false
    private let collectsSpans: Bool

    init(
        rules: LanguageRules, language: Language, units: [UInt16], state: Tokenizer.State,
        collectsSpans: Bool = true
    ) {
        self.collectsSpans = collectsSpans
        self.rules = rules
        self.language = language
        self.units = units
        self.state = state
    }

    mutating func run() {
        if rules.markdown {
            runMarkdown()
        } else {
            runCode()
        }
    }

    // MARK: - State only

    /// Computes just the state carried out of the line: it looks only at the characters that
    /// can open or close a comment or string and skips everything else. One pass, no spans.
    mutating func runStateOnly() {
        if rules.markdown {
            // Only a leading fence changes the state.
            let lead = (units.firstIndex(where: { !isSpace($0) }) ?? units.count) + 3
            var head = LineScanner(
                rules: rules, language: language, units: Array(units.prefix(lead)), state: state,
                collectsSpans: false
            )
            head.run()
            state = head.state
            return
        }
        var j = 0
        switch state.mode {
        case .code:
            break
        case .blockComment:
            guard let end = skip(rules.blockClose, from: 0, escapes: false) else { return }
            j = end
            state.mode = .code
        case .multiLineString(let name):
            let delimiter = Array(name.utf16)
            guard let end = skip(delimiter, from: 0, escapes: escapes(for: delimiter[0])) else { return }
            j = end
            state.mode = .code
        }
        while j < units.count {
            let c = units[j]
            guard c < 128, rules.startsSpecial[Int(c)] else {
                j += 1
                continue
            }
            if rules.lineCommentUnits.contains(where: { $0[0] == c && matches($0, at: j) }) { return }
            if let first = rules.blockOpen.first, first == c, matches(rules.blockOpen, at: j) {
                guard let end = skip(rules.blockClose, from: j + rules.blockOpen.count, escapes: false) else {
                    state.mode = .blockComment
                    return
                }
                j = end
            } else if let open = rules.multiLineUnits.first(where: { $0[0] == c && matches($0, at: j) }) {
                guard let end = skip(open, from: j + open.count, escapes: escapes(for: c)) else {
                    state.mode = .multiLineString(String(decoding: open, as: UTF16.self))
                    return
                }
                j = end
            } else if !rules.markup, rules.quoteUnits.contains(c) {
                j = skipQuoted(c, at: j)
            } else {
                j += 1
            }
        }
    }

    /// The index just past a single-line string or character literal starting at `position`.
    private func skipQuoted(_ quote: UInt16, at position: Int) -> Int {
        let from = position + 1
        if rules.lifetimes, quote == 0x27 {
            guard from < units.count, units[from] != 0x5C else { return findUnit(quote, from: from, escapes: true) ?? units.count }
            let width = (0xD800...0xDBFF).contains(units[from]) ? 2 : 1
            return from + width < units.count && units[from + width] == 0x27 ? from + width + 1 : from
        }
        return findUnit(quote, from: from, escapes: escapes(for: quote)) ?? units.count
    }

    private func findUnit(_ unit: UInt16, from: Int, escapes: Bool) -> Int? {
        var j = from
        while j < units.count {
            let c = units[j]
            if escapes, c == 0x5C {
                j += 2
            } else if c == unit {
                return j + 1
            } else {
                j += 1
            }
        }
        return nil
    }

    private func skip(_ close: [UInt16], from: Int, escapes: Bool) -> Int? {
        find(close, from: from, escapes: escapes)
    }

    // MARK: - Code

    private mutating func runCode() {
        switch state.mode {
        case .code:
            break
        case .blockComment:
            guard finishBlockComment(from: 0) else { return }
        case .multiLineString(let delimiter):
            guard finishMultiLineString(from: 0, delimiter: Array(delimiter.utf16), name: delimiter) else { return }
        }
        while index < units.count {
            let c = units[index]
            if isSpace(c) {
                index += 1
            } else if !scanSpecial(c) {
                return
            }
        }
    }

    /// Handles one token starting at `index`. Returns false when the rest of the line is consumed.
    private mutating func scanSpecial(_ c: UInt16) -> Bool {
        guard c < 128, rules.startsSpecial[Int(c)] else { return scanPlain(c) }
        for pattern in rules.lineCommentUnits where pattern[0] == c && matches(pattern, at: index) {
            emit(index, units.count, .comment)
            index = units.count
            return false
        }
        if let first = rules.blockOpen.first, first == c, matches(rules.blockOpen, at: index) {
            let start = index
            index += rules.blockOpen.count
            return finishBlockComment(from: start)
        }
        for open in rules.multiLineUnits where open[0] == c && matches(open, at: index) {
            let start = index
            index += open.count
            let name = String(decoding: open, as: UTF16.self)
            return finishMultiLineString(from: start, delimiter: open, name: name)
        }
        if rules.markup, scanMarkup(c) { return true }
        if (!rules.markup || inTag), rules.quoteUnits.contains(c) {
            scanQuoted(c)
            return true
        }
        return scanPlain(c)
    }

    private mutating func scanPlain(_ c: UInt16) -> Bool {
        if isDigit(c), !rules.markup || inTag {
            scanNumber()
        } else if c == 0x40, rules.atAttributes, isIdentStart(at: index + 1) {
            scanPrefixed()
        } else if c == 0x23, rules.hashDirectives, isIdentStart(at: index + 1) {
            scanPrefixed()
        } else if isIdentStart(c) {
            scanIdentifier()
        } else if isPunctuation(c) {
            scanPunctuation()
        } else {
            index += 1
        }
        return true
    }

    private mutating func finishBlockComment(from start: Int) -> Bool {
        if let end = find(rules.blockClose, from: index, escapes: false) {
            emit(start, end, .comment)
            index = end
            state.mode = .code
            return true
        }
        emit(start, units.count, .comment)
        index = units.count
        state.mode = .blockComment
        return false
    }

    private mutating func finishMultiLineString(from start: Int, delimiter: [UInt16], name: String) -> Bool {
        if let end = find(delimiter, from: index, escapes: escapes(for: delimiter[0])) {
            emit(start, end, .string)
            index = end
            state.mode = .code
            return true
        }
        emit(start, units.count, .string)
        index = units.count
        state.mode = .multiLineString(name)
        return false
    }

    private mutating func scanQuoted(_ quote: UInt16) {
        let start = index
        index += 1
        if rules.lifetimes, quote == 0x27, scanRustQuote(start) { return }
        guard let end = find([quote], from: index, escapes: escapes(for: quote)) else {
            emit(start, units.count, .string)
            index = units.count
            return
        }
        index = end
        if rules.jsonKeys, nextNonSpace(from: end) == 0x3A {
            emit(start, end, .attribute)
        } else {
            emit(start, end, .string)
        }
    }

    private mutating func scanNumber() {
        let start = index
        var hexLike = false
        if units[index] == 0x30, index + 1 < units.count, isRadixMarker(units[index + 1]) {
            hexLike = true
            index += 2
        } else {
            index += 1
        }
        while index < units.count {
            let c = units[index]
            if isAlnum(c) || c == 0x5F {
                index += 1
            } else if c == 0x2E, index + 1 < units.count, isDigit(units[index + 1]) {
                index += 1
            } else if c == 0x2B || c == 0x2D, !hexLike, isExponent(units[index - 1]),
                      index + 1 < units.count, isDigit(units[index + 1]) {
                index += 1
            } else {
                break
            }
        }
        emit(start, index, .number)
    }

    private mutating func scanPrefixed() {
        let start = index
        index += 1
        while index < units.count, isIdentPart(units[index]) { index += 1 }
        emit(start, index, .attribute)
    }

    private mutating func scanIdentifier() {
        let start = index
        let fold = rules.caseInsensitive
        var hash = LanguageRules.hashSeed
        while index < units.count, isIdentPart(units[index]) {
            hash = LanguageRules.mix(hash, units[index], fold: fold)
            index += 1
        }
        if rules.markup {
            if inTag { emit(start, index, .attribute) }
            return
        }
        if index - start <= 24, rules.keywordHashes.contains(hash) {
            var word = String(decoding: units[start..<index], as: UTF16.self)
            if fold { word = word.lowercased() }
            if rules.keywords.contains(word) {
                emit(start, index, .keyword)
                return
            }
        }
        if rules.typeHeuristic, units[start] >= 0x41, units[start] <= 0x5A {
            emit(start, index, .type)
        }
    }

    private mutating func scanPunctuation() {
        let start = index
        while index < units.count, isPunctuation(units[index]) { index += 1 }
        emit(start, index, .punctuation)
    }

    /// `<tag`, `</tag`, `>`. Returns true when it consumed something.
    private mutating func scanMarkup(_ c: UInt16) -> Bool {
        if c == 0x3E {
            inTag = false
            emit(index, index + 1, .punctuation)
            index += 1
            return true
        }
        guard c == 0x3C else { return false }
        var j = index + 1
        if j < units.count, units[j] == 0x2F { j += 1 }
        guard isIdentStart(at: j) else { return false }
        while j < units.count, isIdentPart(units[j]) || units[j] == 0x2D { j += 1 }
        emit(index, j, .keyword)
        index = j
        inTag = true
        return true
    }

    // MARK: - Markdown

    private mutating func runMarkdown() {
        let first = units.firstIndex(where: { !isSpace($0) })
        let fenced = first.map { matches(fence, at: $0) } ?? false
        if state.mode != .code {
            emit(0, units.count, .string)
            if fenced { state.mode = .code }
            return
        }
        guard let first else { return }
        if fenced {
            emit(0, units.count, .string)
            state.mode = .multiLineString("```")
            return
        }
        var hashes = first
        while hashes < units.count, units[hashes] == 0x23 { hashes += 1 }
        let level = hashes - first
        if (1...6).contains(level), first <= 3, hashes == units.count || isSpace(units[hashes]) {
            emit(first, units.count, .keyword)
            return
        }
        scanCodeSpans()
    }

    private let fence: [UInt16] = [0x60, 0x60, 0x60]

    /// Pairs backtick runs of equal length in one pass.
    private mutating func scanCodeSpans() {
        var runs: [(start: Int, end: Int)] = []
        var j = 0
        while j < units.count {
            if units[j] == 0x60 {
                let start = j
                while j < units.count, units[j] == 0x60 { j += 1 }
                runs.append((start, j))
            } else {
                j += 1
            }
        }
        var nextSame = [Int](repeating: -1, count: runs.count)
        var latest: [Int: Int] = [:]
        for k in stride(from: runs.count - 1, through: 0, by: -1) {
            let length = runs[k].end - runs[k].start
            nextSame[k] = latest[length] ?? -1
            latest[length] = k
        }
        var k = 0
        while k < runs.count {
            if nextSame[k] >= 0 {
                emit(runs[k].start, runs[nextSame[k]].end, .string)
                k = nextSame[k] + 1
            } else {
                k += 1
            }
        }
    }

    // MARK: - Helpers

    private mutating func emit(_ start: Int, _ end: Int, _ kind: TokenKind) {
        guard collectsSpans, end > start else { return }
        spans.append(TokenSpan(range: start..<end, kind: kind))
    }

    private func matches(_ pattern: [UInt16], at position: Int) -> Bool {
        guard !pattern.isEmpty, position + pattern.count <= units.count else { return false }
        for k in 0..<pattern.count where units[position + k] != pattern[k] { return false }
        return true
    }

    /// Index just past the first `close` at or after `from`, skipping backslash escapes when asked.
    private func find(_ close: [UInt16], from: Int, escapes: Bool) -> Int? {
        let first = close[0]
        var j = from
        while j < units.count {
            let c = units[j]
            if escapes, c == 0x5C {
                j += 2
            } else if c == first, matches(close, at: j) {
                return j + close.count
            } else {
                j += 1
            }
        }
        return nil
    }

    private func escapes(for delimiter: UInt16) -> Bool {
        if language == .go, delimiter == 0x60 { return false }
        if language == .shell, delimiter == 0x27 { return false }
        return true
    }

    /// A Rust `'`: a character literal when it closes right away, otherwise a lifetime
    /// (left plain, like any identifier). `index` is just past the quote.
    private mutating func scanRustQuote(_ start: Int) -> Bool {
        guard index < units.count, units[index] != 0x5C else { return false }
        let width = (0xD800...0xDBFF).contains(units[index]) ? 2 : 1
        if index + width < units.count, units[index + width] == 0x27 {
            index += width + 1
            emit(start, index, .string)
            return true
        }
        while index < units.count, isIdentPart(units[index]) { index += 1 }
        return true
    }

    private func nextNonSpace(from position: Int) -> UInt16? {
        var j = position
        while j < units.count, isSpace(units[j]) { j += 1 }
        return j < units.count ? units[j] : nil
    }

    private func isIdentStart(at position: Int) -> Bool {
        position < units.count && isIdentStart(units[position])
    }

    // Bit flags per ASCII code unit: 1 space, 2 digit, 4 identifier start, 8 punctuation.
    private static let classes: [UInt8] = (0..<128).map { value -> UInt8 in
        let c = UInt16(value)
        var flags: UInt8 = 0
        if c == 0x20 || c == 0x09 || c == 0x0D || c == 0x0A { flags |= 1 }
        if c >= 0x30 && c <= 0x39 { flags |= 2 }
        if (c >= 0x41 && c <= 0x5A) || (c >= 0x61 && c <= 0x7A) || c == 0x5F || c == 0x24 { flags |= 4 }
        switch c {
        case 0x21, 0x25, 0x26, 0x28...0x2F, 0x3A...0x3F, 0x5B, 0x5D, 0x5E, 0x7B...0x7E: flags |= 8
        default: break
        }
        return flags
    }

    @inline(__always) private func flags(_ c: UInt16) -> UInt8 { c < 128 ? Self.classes[Int(c)] : 4 }
    private func isSpace(_ c: UInt16) -> Bool { c < 128 && Self.classes[Int(c)] & 1 != 0 }
    private func isDigit(_ c: UInt16) -> Bool { c < 128 && Self.classes[Int(c)] & 2 != 0 }
    private func isAlnum(_ c: UInt16) -> Bool {
        isDigit(c) || (c >= 0x41 && c <= 0x5A) || (c >= 0x61 && c <= 0x7A)
    }
    private func isIdentStart(_ c: UInt16) -> Bool { flags(c) & 4 != 0 }
    private func isIdentPart(_ c: UInt16) -> Bool { flags(c) & 6 != 0 }
    private func isExponent(_ c: UInt16) -> Bool { c == 0x65 || c == 0x45 }
    private func isRadixMarker(_ c: UInt16) -> Bool {
        c == 0x78 || c == 0x58 || c == 0x62 || c == 0x42 || c == 0x6F || c == 0x4F
    }
    private func isPunctuation(_ c: UInt16) -> Bool { c < 128 && Self.classes[Int(c)] & 8 != 0 }
}
