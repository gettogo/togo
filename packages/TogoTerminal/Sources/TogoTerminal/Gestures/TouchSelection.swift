import UIKit

/// Where a touch selection's words start and end, and which end a finger has taken. Cells are
/// in buffer rows (the scrollback's, not the screen's), so a selection stays put while the
/// history scrolls under it. Ends are exclusive.
enum TouchSelectionLogic {
    enum End: Equatable, Sendable { case start, end }

    /// The cells of the word under `col`: a run of characters up to a space, a bracket, a quote
    /// or a box-drawing line, so a path, a URL or a flag is one word. Nil on a blank cell.
    ///
    /// `widths` are the row's cell widths: a wide glyph takes a 2 and a 0, and `text` holds one
    /// character per cell that is not a 0. A row's text may stop before its cells do.
    static func word(text: String, widths: [Int], at col: Int) -> Range<Int>? {
        guard widths.indices.contains(col) else { return nil }
        var characters = text.makeIterator()
        let cells: [Character?] = widths.map { width in width == 0 ? nil : (characters.next() ?? " ") }
        func isWord(_ index: Int) -> Bool {
            guard let character = cells[index] else { return true }
            return !separates(character)
        }
        var anchor = col
        // The second half of a wide glyph belongs to the glyph.
        while anchor > 0, cells[anchor] == nil { anchor -= 1 }
        guard cells[anchor] != nil, isWord(anchor) else { return nil }
        var lower = anchor
        while lower > 0, isWord(lower - 1) { lower -= 1 }
        var upper = anchor + 1
        while upper < cells.count, isWord(upper) { upper += 1 }
        return lower..<upper
    }

    static func separates(_ character: Character) -> Bool {
        if character.isWhitespace { return true }
        if "\"'`()[]{}<>,;|".contains(character) { return true }
        // Box drawing and block elements: the frames a TUI draws around its text.
        return character.unicodeScalars.contains { (0x2500...0x259F).contains($0.value) }
    }

    /// The end a finger at `cell` takes: within two columns and a row of its edge (the start's
    /// left, the end's right). The nearer one when both are close, as on a one-letter selection.
    static func grabbed(_ cell: GridCell, start: GridCell, end: GridCell) -> End? {
        func distance(_ to: GridCell) -> Int? {
            let rows = abs(cell.row - to.row)
            let cols = abs(cell.col - to.col)
            return rows <= 1 && cols <= 2 ? rows * 100 + cols : nil
        }
        switch (distance(start), distance(end)) {
        case let (a?, b?): return a < b ? .start : .end
        case (_?, nil): return .start
        case (nil, _?): return .end
        case (nil, nil): return nil
        }
    }

    /// The cell that stays while `moving` is dragged: the first selected cell, or the last.
    static func pivot(start: GridCell, end: GridCell, moving: End) -> GridCell {
        moving == .end ? start : GridCell(col: max(start.col, end.col - 1), row: end.row)
    }

    /// The selection from the pivot to the finger's cell, both included, whichever comes first.
    static func range(pivot: GridCell, finger: GridCell) -> (start: GridCell, end: GridCell) {
        if (finger.row, finger.col) >= (pivot.row, pivot.col) {
            return (pivot, GridCell(col: finger.col + 1, row: finger.row))
        }
        return (finger, GridCell(col: pivot.col + 1, row: pivot.row))
    }
}

/// Copied terminal text as it reads: SwiftTerm keeps the spaces a program printed to pad a
/// row (a TUI fills every row to the edge), so each line's trailing spaces and tabs go. Rows
/// the emulator wrapped are already one line, so a space at a wrap stays inside it.
enum CopyText {
    static func trimmingLineEnds(_ text: String) -> String {
        text.split(separator: "\n", omittingEmptySubsequences: false)
            .map { line in
                var end = line.endIndex
                while end > line.startIndex, line[line.index(before: end)] == " " || line[line.index(before: end)] == "\t" {
                    end = line.index(before: end)
                }
                return line[..<end]
            }
            .joined(separator: "\n")
    }
}

/// What touch selection needs from an engine, beyond `GestureTarget`.
@MainActor protocol TouchSelectionTarget: GestureTarget {
    /// The buffer row at the top of the screen.
    var viewportTop: Int { get }
    var lineHeight: CGFloat { get }
    /// A screen row's text and cell widths.
    func rowCells(_ row: Int) -> (text: String, widths: [Int])?
    /// Selects from `start` up to, not including, `end`, in buffer rows.
    func selectCells(from start: GridCell, to end: GridCell)
    func showEditMenu(at point: CGPoint)
    func scroll(by distance: CGFloat, at point: CGPoint)
}

/// Selecting terminal text by touch, the way iOS text selects: a long press picks the word under
/// the finger (`selectWord(at:)`) and shows Copy; its handles then drag either end, across
/// lines, and on past the top or bottom edge, where the history scrolls to meet the finger.
/// A tap anywhere else ends it (SwiftTerm's own tap).
@MainActor final class TouchSelection: NSObject {
    let pan: UIPanGestureRecognizer
    private weak var target: (any TouchSelectionTarget)?
    /// The touch selection, in buffer rows; nil once anything else selected or cleared.
    private var range: (start: GridCell, end: GridCell)?
    private var pivot: GridCell?
    private var lastPoint: CGPoint = .zero
    private var autoscroll: Timer?
    /// Which way the history moves while the finger is past an edge.
    private var autoscrollUp = false

    init(target: any TouchSelectionTarget) {
        self.target = target
        pan = UIPanGestureRecognizer()
        pan.maximumNumberOfTouches = 1
        super.init()
        pan.addTarget(self, action: #selector(dragged(_:)))
    }

    /// Selects the word at the point and shows the edit menu; false on a blank.
    @discardableResult
    func selectWord(at point: CGPoint) -> Bool {
        guard let target else { return false }
        let cell = target.cell(at: point)
        guard let row = target.rowCells(cell.row),
              let word = TouchSelectionLogic.word(text: row.text, widths: row.widths, at: cell.col) else { return false }
        let top = target.viewportTop
        let selection = (GridCell(col: word.lowerBound, row: cell.row + top), GridCell(col: word.upperBound, row: cell.row + top))
        range = selection
        target.selectCells(from: selection.0, to: selection.1)
        target.showEditMenu(at: point)
        return true
    }

    /// The edit menu with nothing selected: Paste, Select and Select All.
    func showMenu(at point: CGPoint) {
        target?.showEditMenu(at: point)
    }

    /// The drag begins only on one of the selection's ends.
    func shouldBegin() -> Bool {
        guard let target, target.isSelecting else {
            range = nil
            return false
        }
        guard let range else { return false }
        let point = pan.location(in: target.gestureView)
        guard let moving = TouchSelectionLogic.grabbed(bufferCell(at: point, in: target), start: range.start, end: range.end) else {
            return false
        }
        pivot = TouchSelectionLogic.pivot(start: range.start, end: range.end, moving: moving)
        return true
    }

    @objc private func dragged(_ pan: UIPanGestureRecognizer) {
        guard let target else { return }
        let point = pan.location(in: target.gestureView)
        switch pan.state {
        case .began, .changed:
            lastPoint = point
            extend(to: point, in: target)
            updateAutoscroll(at: point, in: target)
        case .ended:
            stopAutoscroll()
            target.showEditMenu(at: point)
        default:
            stopAutoscroll()
        }
    }

    private func extend(to point: CGPoint, in target: any TouchSelectionTarget) {
        guard let pivot else { return }
        let next = TouchSelectionLogic.range(pivot: pivot, finger: bufferCell(at: point, in: target))
        range = next
        target.selectCells(from: next.start, to: next.end)
    }

    private func bufferCell(at point: CGPoint, in target: any TouchSelectionTarget) -> GridCell {
        let cell = target.cell(at: point)
        return GridCell(col: cell.col, row: cell.row + target.viewportTop)
    }

    /// Past the top or bottom edge the history moves a line at a time toward the finger. Not
    /// while the remote takes the mouse: there a scroll would reach it as the wheel.
    private func updateAutoscroll(at point: CGPoint, in target: any TouchSelectionTarget) {
        let bounds = target.gestureView.bounds
        let outside = point.y < bounds.minY || point.y > bounds.maxY
        guard outside, !target.tracksMouse else { return stopAutoscroll() }
        autoscrollUp = point.y < bounds.minY
        guard autoscroll == nil else { return }
        autoscroll = Timer.scheduledTimer(withTimeInterval: 0.06, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated { self?.stepAutoscroll() }
        }
    }

    private func stepAutoscroll() {
        guard let target else { return stopAutoscroll() }
        // A positive distance shows earlier lines, as a finger dragging the history down does.
        let line = target.lineHeight
        target.scroll(by: autoscrollUp ? line : -line, at: CGPoint(x: lastPoint.x, y: target.gestureView.bounds.midY))
        // The selection follows to the edge row now showing.
        let bounds = target.gestureView.bounds
        extend(to: CGPoint(x: lastPoint.x, y: autoscrollUp ? bounds.minY + 1 : bounds.maxY - 1), in: target)
    }

    private func stopAutoscroll() {
        autoscroll?.invalidate()
        autoscroll = nil
    }
}
