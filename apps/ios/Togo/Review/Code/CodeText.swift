import CoreText
import TogoKit
import UIKit

/// The text a row shows, prepared once per row: tabs expanded to the next stop and a clipped
/// line's marker appended. Layout and drawing both measure and break this same string.
struct CodeText: Sendable {
    static let marker = "  … \(CodeCopy.clipped)"

    let string: String
    /// UTF-16 length of `string`.
    let length: Int
    /// UTF-16 length of the source part, before any marker.
    let sourceLength: Int
    /// Only printable ASCII, so every character is one advance wide.
    let plain: Bool
    /// Banners break between words; code breaks anywhere.
    let words: Bool
    /// Source UTF-16 offset to offset in `string`, when tabs moved them.
    private let map: [Int]?

    init(_ source: String, clipped: Bool = false, words: Bool = false) {
        let (expanded, map) = Self.expandTabs(source, tabWidth: ReviewLimits.tabWidth)
        sourceLength = expanded.utf16.count
        string = clipped ? expanded + Self.marker : expanded
        length = string.utf16.count
        plain = string.utf8.allSatisfy { $0 >= 0x20 && $0 < 0x7F }
        self.words = words
        self.map = map
    }

    /// Where a source offset landed after tab expansion, clamped to the source part.
    func position(_ offset: Int) -> Int {
        let offset = max(offset, 0)
        guard let map else { return min(offset, sourceLength) }
        return min(map[min(offset, map.count - 1)], sourceLength)
    }

    static func expandTabs(_ text: String, tabWidth: Int) -> (String, [Int]?) {
        guard text.utf8.contains(9) else { return (text, nil) }
        var out = ""
        var map: [Int] = []
        var column = 0
        var offset = 0
        for character in text {
            let width = CodeLinesLayout.columns(of: character, at: column, tabWidth: tabWidth)
            let units = character.utf16.count
            map.append(contentsOf: repeatElement(offset, count: units))
            if character == "\t" {
                out += String(repeating: " ", count: width)
                offset += width
            } else {
                out.append(character)
                offset += units
            }
            column += width
        }
        map.append(offset)
        return (out, map)
    }
}

/// Widths of characters in the code font, measured with Core Text once per character. Emoji,
/// CJK and rare wide glyphs do not fill whole columns, so lines that hold them are measured
/// rather than counted.
@MainActor final class CodeMeasure {
    let font: UIFont
    let advance: CGFloat
    private var widths: [Character: CGFloat] = [:]

    init(font: UIFont) {
        self.font = font
        advance = ("0" as NSString).size(withAttributes: [.font: font]).width
    }

    func width(of character: Character) -> CGFloat {
        if let known = widths[character] { return known }
        let line = CTLineCreateWithAttributedString(NSAttributedString(string: String(character), attributes: [.font: font]))
        let width = CGFloat(CTLineGetTypographicBounds(line, nil, nil, nil))
        widths[character] = width
        return width
    }
}

/// Line breaking shared by `CodeLinesLayout` (row heights, content width) and `CodeRenderer`
/// (the lines it draws), so a row is always exactly as tall as what is drawn in it.
@MainActor enum CodeFlow {
    /// Width of the whole text on one line.
    static func width(_ text: CodeText, advance: CGFloat, measure: CodeMeasure?) -> CGFloat {
        if text.plain { return CGFloat(text.length) * advance }
        var total: CGFloat = 0
        var column = 0
        for character in text.string {
            total += width(of: character, column: &column, advance: advance, measure: measure)
        }
        return total
    }

    static func lineCount(_ text: CodeText, limit: CGFloat, advance: CGFloat, measure: CodeMeasure?) -> Int {
        if text.plain, !text.words {
            let each = perLine(limit: limit, advance: advance)
            return max(1, (text.length + each - 1) / each)
        }
        return breaks(text, limit: limit, advance: advance, measure: measure).count
    }

    /// UTF-16 offsets where each visual line starts. A line holds as many characters as fit in
    /// `limit` points; a single character wider than that gets a line of its own.
    static func breaks(_ text: CodeText, limit: CGFloat, advance: CGFloat, measure: CodeMeasure?) -> [Int] {
        if text.plain, !text.words {
            let each = perLine(limit: limit, advance: advance)
            return Array(stride(from: 0, to: max(text.length, 1), by: each))
        }
        var starts = [0]
        var x: CGFloat = 0
        var offset = 0
        var column = 0
        /// The offset just after the last space on this line, and the width up to it.
        var space: (offset: Int, x: CGFloat)?
        for character in text.string {
            let width = width(of: character, column: &column, advance: advance, measure: measure)
            if x > 0, x + width > limit + 0.01 {
                if let found = space, found.offset > starts[starts.count - 1], x - found.x + width <= limit + 0.01 {
                    starts.append(found.offset)
                    x -= found.x
                } else {
                    starts.append(offset)
                    x = 0
                }
                space = nil
            }
            x += width
            offset += character.utf16.count
            if text.words, character == " " { space = (offset, x) }
        }
        return starts
    }

    /// Whole advances in `limit`, allowing for rounding in `columns * advance`.
    static func perLine(limit: CGFloat, advance: CGFloat) -> Int {
        max(1, Int((limit / advance + 0.001).rounded(.down)))
    }

    private static func width(of character: Character, column: inout Int, advance: CGFloat, measure: CodeMeasure?) -> CGFloat {
        let columns = CodeLinesLayout.columns(of: character, at: column, tabWidth: ReviewLimits.tabWidth)
        column += columns
        if let first = character.utf8.first, character.utf8.count == 1, first >= 0x20, first < 0x7F { return advance }
        return measure?.width(of: character) ?? CGFloat(columns) * advance
    }
}
