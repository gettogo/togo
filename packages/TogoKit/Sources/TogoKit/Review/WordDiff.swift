/// Finds the words that changed between a removed line and the added line paired with it.
public enum WordDiff {
    /// Below this share of unchanged UTF-16 units in the longer line, the pair is too
    /// different for word highlights to help.
    static let minimumUnchanged = 0.4
    /// Token edits beyond this mean the lines are unrelated; the search stops there.
    static let maxEdits = 64

    /// UTF-16 ranges of the changed tokens on each side, adjacent tokens merged. Nil when
    /// either line is longer than `ReviewLimits.wordDiffMaxLine` or the pair is too different.
    public static func ranges(old: String, new: String) -> (old: [Range<Int>], new: [Range<Int>])? {
        let limit = ReviewLimits.wordDiffMaxLine
        guard old.utf8.count <= limit * 3, new.utf8.count <= limit * 3 else { return nil }
        let a = Array(old.utf16)
        let b = Array(new.utf16)
        guard a.count <= limit, b.count <= limit else { return nil }
        let longer = max(a.count, b.count)
        if longer == 0 { return ([], []) }

        let oldTokens = tokens(a)
        let newTokens = tokens(b)
        guard let matches = match(oldTokens, a, newTokens, b) else { return nil }

        var unchanged = 0
        for index in 0..<oldTokens.count where matches.old[index] {
            unchanged += oldTokens[index].range.count
        }
        guard Double(unchanged) >= minimumUnchanged * Double(longer) else { return nil }
        return (merged(oldTokens, matches.old), merged(newTokens, matches.new))
    }

    /// Pairs the n-th removed line with the n-th added line in every block of changes and
    /// records their changed ranges. A pair whose text matches but whose line ending does not
    /// is marked `lineEndingChanged` instead. Lines left unpaired keep no ranges.
    public static func mark(_ hunk: inout DiffHunk) {
        var removed: [Int] = []
        var added: [Int] = []

        func flush() {
            for (left, right) in zip(removed, added) {
                let old = hunk.lines[left], new = hunk.lines[right]
                if old.endsWithCR != new.endsWithCR, old.text == new.text, old.clipped == new.clipped {
                    hunk.lines[left].lineEndingChanged = true
                    hunk.lines[right].lineEndingChanged = true
                    continue
                }
                guard let found = ranges(old: hunk.lines[left].text, new: hunk.lines[right].text) else { continue }
                hunk.lines[left].changes = found.old
                hunk.lines[right].changes = found.new
            }
            removed.removeAll(keepingCapacity: true)
            added.removeAll(keepingCapacity: true)
        }

        for index in hunk.lines.indices {
            switch hunk.lines[index].kind {
            case .removed: removed.append(index)
            case .added: added.append(index)
            case .context: flush()
            case .noNewline: break
            }
        }
        flush()
    }

    // MARK: - Tokens

    private enum Class: Equatable { case word, space, other }

    struct Token {
        var range: Range<Int>
        /// Equal tokens have equal hashes; unequal hashes skip the unit comparison.
        var hash: Int
    }

    /// Runs of letters, digits and underscores; runs of whitespace; any other character alone.
    static func tokens(_ units: [UInt16]) -> [Token] {
        var result: [Token] = []
        result.reserveCapacity(units.count / 3 + 1)
        units.withUnsafeBufferPointer { buffer in
            var start = 0
            var hash = 0
            var current = Class.other
            var index = 0
            while index < buffer.count {
                let unit = buffer[index]
                var width = 1
                let kind: Class
                if unit < 0x80 {
                    kind = asciiClass(unit)
                } else if UTF16.isLeadSurrogate(unit), index + 1 < buffer.count,
                          UTF16.isTrailSurrogate(buffer[index + 1]) {
                    width = 2
                    let value = 0x10000 + ((UInt32(unit) - 0xD800) << 10) + (UInt32(buffer[index + 1]) - 0xDC00)
                    kind = Unicode.Scalar(value).map(classify) ?? .other
                } else {
                    kind = Unicode.Scalar(unit).map(classify) ?? .other
                }
                if index > 0, kind != current || kind == .other {
                    result.append(Token(range: start..<index, hash: hash))
                    start = index
                    hash = 0
                }
                hash = hash &* 31 &+ Int(unit)
                if width == 2 { hash = hash &* 31 &+ Int(buffer[index + 1]) }
                current = kind
                index += width
            }
            if index > 0 { result.append(Token(range: start..<index, hash: hash)) }
        }
        return result
    }

    private static func asciiClass(_ unit: UInt16) -> Class {
        switch unit {
        case 0x30...0x39, 0x41...0x5A, 0x61...0x7A, 0x5F: .word
        case 0x09...0x0D, 0x20: .space
        default: .other
        }
    }

    private static func classify(_ scalar: Unicode.Scalar) -> Class {
        if scalar.isASCII { return asciiClass(UInt16(scalar.value)) }
        let properties = scalar.properties
        if properties.isWhitespace { return .space }
        if properties.isAlphabetic || properties.numericType != nil { return .word }
        return .other
    }

    private static func merged(_ tokens: [Token], _ matched: [Bool]) -> [Range<Int>] {
        var result: [Range<Int>] = []
        var start = -1
        var end = -1
        for index in 0..<tokens.count where !matched[index] {
            let token = tokens[index].range
            if token.lowerBound == end {
                end = token.upperBound
                continue
            }
            if start >= 0 { result.append(start..<end) }
            start = token.lowerBound
            end = token.upperBound
        }
        if start >= 0 { result.append(start..<end) }
        return result
    }

    // MARK: - Matching

    /// Marks the tokens on each side that belong to a longest common subsequence, using
    /// Myers' algorithm after trimming the shared prefix and suffix.
    private static func match(
        _ oldTokens: [Token], _ oldUnits: [UInt16], _ newTokens: [Token], _ newUnits: [UInt16]
    ) -> (old: [Bool], new: [Bool])? {
        oldTokens.withUnsafeBufferPointer { x in
            newTokens.withUnsafeBufferPointer { y in
                oldUnits.withUnsafeBufferPointer { a in
                    newUnits.withUnsafeBufferPointer { b in
                        match(Side(tokens: x, units: a), Side(tokens: y, units: b))
                    }
                }
            }
        }
    }

    private struct Side {
        var tokens: UnsafeBufferPointer<Token>
        var units: UnsafeBufferPointer<UInt16>
        var count: Int { tokens.count }
    }

    private static func match(_ x: Side, _ y: Side) -> (old: [Bool], new: [Bool])? {
        func same(_ i: Int, _ j: Int) -> Bool {
            let left = x.tokens[i], right = y.tokens[j]
            guard left.hash == right.hash, left.range.count == right.range.count else { return false }
            var offset = 0
            let first = left.range.lowerBound, second = right.range.lowerBound
            while offset < left.range.count {
                if x.units[first + offset] != y.units[second + offset] { return false }
                offset += 1
            }
            return true
        }

        var old = [Bool](repeating: false, count: x.count)
        var new = [Bool](repeating: false, count: y.count)
        var prefix = 0
        while prefix < x.count, prefix < y.count, same(prefix, prefix) {
            old[prefix] = true
            new[prefix] = true
            prefix += 1
        }
        var suffix = 0
        while suffix < x.count - prefix, suffix < y.count - prefix,
              same(x.count - 1 - suffix, y.count - 1 - suffix) {
            old[x.count - 1 - suffix] = true
            new[y.count - 1 - suffix] = true
            suffix += 1
        }

        let n = x.count - prefix - suffix
        let m = y.count - prefix - suffix
        if n == 0 || m == 0 { return (old, new) }
        let bound = min(n + m, maxEdits)
        let offset = bound + 1
        var v = [Int](repeating: 0, count: 2 * bound + 3)
        var trace: [[Int]] = []

        for d in 0...bound {
            trace.append(v)
            for k in stride(from: -d, through: d, by: 2) {
                var col = k == -d || (k != d && v[offset + k - 1] < v[offset + k + 1])
                    ? v[offset + k + 1] : v[offset + k - 1] + 1
                var row = col - k
                while col < n, row < m, same(prefix + col, prefix + row) {
                    col += 1
                    row += 1
                }
                v[offset + k] = col
                guard col >= n, row >= m else { continue }

                var i = n, j = m
                for step in stride(from: d, to: 0, by: -1) {
                    let previous = trace[step]
                    let diagonal = i - j
                    let down = diagonal == -step
                        || (diagonal != step && previous[offset + diagonal - 1] < previous[offset + diagonal + 1])
                    let fromK = down ? diagonal + 1 : diagonal - 1
                    let fromI = previous[offset + fromK]
                    let fromJ = fromI - fromK
                    while i > fromI, j > fromJ {
                        i -= 1
                        j -= 1
                        old[prefix + i] = true
                        new[prefix + j] = true
                    }
                    i = fromI
                    j = fromJ
                }
                while i > 0, j > 0 {
                    i -= 1
                    j -= 1
                    old[prefix + i] = true
                    new[prefix + j] = true
                }
                return (old, new)
            }
        }
        return nil
    }
}
