import Foundation
import TogoTerminal

/// Where "Show in terminal" lands, worked out from the helper's answer (`linesFromBottom`, how
/// far above the cursor line the item happened) and the phone's own scrollback.
///
/// Over SSH the phone holds the pane's history, so the terminal scrolls there and a band marks
/// the line. When the phone holds fewer lines than that, as over Mosh or right after an attach,
/// the capture sheet shows the pane's history from the machine instead.
enum JumpLanding: Equatable {
    /// Scroll so `top` is the first visible line, and mark screen row `row`.
    case scroll(top: Int, row: Int)
    /// The line is not on the phone: open the capture sheet at the needle.
    case capture

    /// How far down the screen the marked line sits after the scroll: a third of the way, so
    /// what led up to it shows above and what followed shows below.
    static let placement = 3

    /// Where a jump lands. `frame` is the scrollback as it stands, nil when the terminal has no
    /// grid yet.
    static func resolve(linesFromBottom: Int, frame: ScrollbackFrame?) -> JumpLanding {
        guard let frame, frame.rows > 0, linesFromBottom >= 0 else { return .capture }
        // Lines from the oldest line of history to the cursor line.
        let cursorLine = frame.base + min(max(0, frame.cursorRow), frame.rows - 1)
        let line = cursorLine - linesFromBottom
        guard line >= 0 else { return .capture }
        let top = min(max(0, line - frame.rows / placement), frame.base)
        return .scroll(top: top, row: line - top)
    }

    /// The screen row that holds `needle`, nearest to `expected`; `expected` itself when no row
    /// near it does. The helper's line and the phone's can differ by a row or two (a wrapped
    /// prompt, a redraw), and the needle settles it.
    static func refine(row expected: Int, needle: String?, rows: [String], within reach: Int = 4) -> Int {
        let clamped = min(max(0, expected), max(0, rows.count - 1))
        guard let probe = probe(needle), !rows.isEmpty else { return clamped }
        let candidates = (0...reach).flatMap { offset in offset == 0 ? [clamped] : [clamped - offset, clamped + offset] }
        for row in candidates where rows.indices.contains(row) {
            if probe.matches(rows[row]) { return row }
        }
        return clamped
    }

    /// The line of `lines` the capture sheet opens at: the last one holding the needle, as the
    /// helper's own search does, or the end.
    static func captureLine(needle: String?, lines: [String]) -> Int? {
        guard !lines.isEmpty else { return nil }
        guard let probe = probe(needle) else { return lines.count - 1 }
        return lines.lastIndex { probe.matches($0) } ?? lines.count - 1
    }

    /// A needle ready to match rows: whitespace squeezed. A row holds it when it contains the
    /// needle's first 40 characters, or, for a needle wider than the phone's row, when the row
    /// is a long enough start of it.
    struct Probe {
        let text: String

        func matches(_ row: String) -> Bool {
            let squeezed = JumpLanding.squeeze(row)
            guard !squeezed.isEmpty else { return false }
            if squeezed.contains(text.prefix(40)) { return true }
            return squeezed.count >= 8 && text.hasPrefix(squeezed)
        }
    }

    static func probe(_ needle: String?) -> Probe? {
        guard let needle else { return nil }
        let squeezed = squeeze(needle)
        guard squeezed.count >= 2 else { return nil }
        return Probe(text: squeezed)
    }

    fileprivate static func squeeze(_ text: String) -> String {
        text.split(whereSeparator: \.isWhitespace).joined(separator: " ")
    }
}
