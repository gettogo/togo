import Foundation
import TogoKit

/// Characters in code that draw nothing or reorder the text around them, made visible. A diff
/// written with direction overrides can read differently on screen from how it runs; here each
/// such character shows as its code, such as `⟨U+202E⟩`, in the comment colour, and the rows get
/// a banner that says so. The markers are for the eye only: `raw` gives the line back as it is.
enum HiddenCharacters {
    static let directionBanner = String(
        localized: "review.code.hiddenDirection", defaultValue: "This file contains hidden direction characters."
    )
    static let banner = String(localized: "review.code.hidden", defaultValue: "This file contains hidden characters.")

    /// `rows` with every hidden character in their text replaced by its marker, token and change
    /// ranges moved to match, and a banner first when any was found.
    static func reveal(_ rows: [CodeRow]) -> [CodeRow] {
        var found = Found()
        let shown = rows.map { reveal($0, &found) }
        guard found.any else { return rows }
        return [.notice(found.direction ? directionBanner : banner)] + shown
    }

    /// The line as the file has it: each `.hidden` token's marker back to its character.
    static func raw(_ text: String, tokens: [TokenSpan]) -> String {
        let markers = tokens.filter { $0.kind == .hidden }.sorted { $0.range.lowerBound < $1.range.lowerBound }
        guard !markers.isEmpty else { return text }
        let units = Array(text.utf16)
        var raw = String.UnicodeScalarView()
        var at = 0
        func append(_ range: Range<Int>) {
            raw.append(contentsOf: String(decoding: units[range], as: UTF16.self).unicodeScalars)
        }
        for marker in markers where marker.range.lowerBound >= at && marker.range.upperBound <= units.count {
            append(at..<marker.range.lowerBound)
            let code = String(decoding: units[marker.range], as: UTF16.self).dropFirst(3).dropLast()
            if let value = UInt32(code, radix: 16), let scalar = Unicode.Scalar(value) {
                raw.append(scalar)
            } else {
                append(marker.range)
            }
            at = marker.range.upperBound
        }
        append(at..<units.count)
        return String(raw)
    }

    /// VoiceOver reads a marker as the character's name.
    static func spoken(_ text: String) -> String {
        guard text.contains("⟨U+") else { return text }
        var result = ""
        var rest = Substring(text)
        while let open = rest.range(of: "⟨U+") {
            result += rest[..<open.lowerBound]
            let after = rest[open.upperBound...]
            guard let close = after.firstIndex(of: "⟩"), let value = UInt32(after[..<close], radix: 16),
                  let scalar = Unicode.Scalar(value) else {
                result += rest[open]
                rest = after
                continue
            }
            result += " \(DisplayText.spoken(scalar)) "
            rest = after[after.index(after: close)...]
        }
        return result + rest
    }

    private struct Found {
        var any = false
        var direction = false
    }

    private static func reveal(_ row: CodeRow, _ found: inout Found) -> CodeRow {
        switch row {
        case let .line(number, oldNumber, text, kind, tokens, changes, clipped):
            guard let shown = reveal(text, &found) else { return row }
            return .line(
                number: number, oldNumber: oldNumber, text: shown.text, kind: kind, tokens: shown.tokens(tokens),
                changes: changes.map(shown.move), clipped: clipped
            )
        case let .split(left, right):
            return .split(left: left.map { reveal($0, &found) }, right: right.map { reveal($0, &found) })
        case .hunk, .notice:
            return row
        }
    }

    private static func reveal(_ side: CodeRowSide, _ found: inout Found) -> CodeRowSide {
        guard let shown = reveal(side.text, &found) else { return side }
        var side = side
        side.text = shown.text
        side.tokens = shown.tokens(side.tokens)
        side.changes = side.changes.map(shown.move)
        return side
    }

    /// One line with its markers: the new text, where each old UTF-16 offset went, and the
    /// markers' ranges.
    private struct Shown {
        var text: String
        var map: [Int]
        var markers: [Range<Int>]

        func move(_ range: Range<Int>) -> Range<Int> {
            let lower = map[min(range.lowerBound, map.count - 1)]
            return lower..<max(lower, map[min(range.upperBound, map.count - 1)])
        }

        /// Token colours stay on the text they covered; each marker draws in the comment colour
        /// over whatever token it sits in.
        func tokens(_ tokens: [TokenSpan]) -> [TokenSpan] {
            tokens.map { TokenSpan(range: move($0.range), kind: $0.kind) } + markers.map { TokenSpan(range: $0, kind: .hidden) }
        }
    }

    private static func reveal(_ text: String, _ found: inout Found) -> Shown? {
        // Printable ASCII and tabs, the common case, hold nothing hidden.
        if text.utf8.allSatisfy({ ($0 >= 0x20 && $0 < 0x7F) || $0 == 0x09 }) { return nil }
        let scalars = Array(text.unicodeScalars)
        guard scalars.indices.contains(where: { hides(scalars, at: $0) }) else { return nil }
        var shown = String.UnicodeScalarView()
        var map: [Int] = []
        var markers: [Range<Int>] = []
        var offset = 0
        for index in scalars.indices {
            let scalar = scalars[index]
            let width = scalar.utf16.count
            map.append(contentsOf: repeatElement(offset, count: width))
            if hides(scalars, at: index) {
                let marker = DisplayText.marker(scalar)
                shown.append(contentsOf: marker.unicodeScalars)
                markers.append(offset..<(offset + marker.utf16.count))
                offset += marker.utf16.count
                found.any = true
                if scalar.properties.isBidiControl { found.direction = true }
            } else {
                shown.append(scalar)
                offset += width
            }
        }
        map.append(offset)
        return Shown(text: String(shown), map: map, markers: markers)
    }

    /// Tabs are code's own; every other hidden character is marked.
    private static func hides(_ scalars: [Unicode.Scalar], at index: Int) -> Bool {
        scalars[index] != "\t" && DisplayText.isHidden(scalars[index])
    }
}
