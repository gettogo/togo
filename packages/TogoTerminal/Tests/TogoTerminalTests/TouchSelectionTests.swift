import Foundation
import Testing
import UIKit
@testable import TogoTerminal

/// A long press selects a word, never the whole buffer; its ends drag across rows; Copy takes
/// what is selected with wrapped rows joined and no trailing spaces.
@MainActor
@Suite struct TouchSelectionTests {
    // MARK: Words

    private func word(_ text: String, at col: Int, widths: [Int]? = nil) -> String? {
        let widths = widths ?? Array(repeating: 1, count: text.count + 4)
        guard let range = TouchSelectionLogic.word(text: text, widths: widths, at: col) else { return nil }
        let cells = Array(text) + Array(repeating: " ", count: max(0, widths.count - text.count))
        return String(cells[range])
    }

    @Test func aWordStopsAtSpacesAndBrackets() {
        #expect(word("run make test now", at: 5) == "make")
        #expect(word("open (src/app.ts:12) now", at: 8) == "src/app.ts:12")
        #expect(word("--verbose", at: 0) == "--verbose")
        #expect(word("see https://example.com/a?b=1 ok", at: 10) == "https://example.com/a?b=1")
        #expect(word("say \"hi\"", at: 5) == "hi")
    }

    @Test func aBlankOrAFrameSelectsNothing() {
        #expect(word("a   b", at: 2) == nil)
        #expect(word("│ text │", at: 0) == nil)
        #expect(word("│ text │", at: 3) == "text")
        // Past the row's text: the cells are blank.
        #expect(word("ab", at: 4) == nil)
        #expect(TouchSelectionLogic.word(text: "ab", widths: [1, 1], at: 9) == nil)
    }

    @Test func aWideGlyphIsOneWordWithBothItsCells() {
        // "日本 x": each glyph a 2 and a 0.
        let widths = [2, 0, 2, 0, 1, 1]
        #expect(TouchSelectionLogic.word(text: "日本 x", widths: widths, at: 1) == 0..<4)
        #expect(TouchSelectionLogic.word(text: "日本 x", widths: widths, at: 3) == 0..<4)
        #expect(TouchSelectionLogic.word(text: "日本 x", widths: widths, at: 5) == 5..<6)
    }

    // MARK: Handles

    @Test func aFingerTakesTheNearerEnd() {
        let start = GridCell(col: 4, row: 10)
        let end = GridCell(col: 9, row: 12)
        #expect(TouchSelectionLogic.grabbed(GridCell(col: 5, row: 10), start: start, end: end) == .start)
        #expect(TouchSelectionLogic.grabbed(GridCell(col: 8, row: 13), start: start, end: end) == .end)
        #expect(TouchSelectionLogic.grabbed(GridCell(col: 20, row: 11), start: start, end: end) == nil)
        // A one-letter selection: the side the finger is on.
        let one = (GridCell(col: 3, row: 0), GridCell(col: 4, row: 0))
        #expect(TouchSelectionLogic.grabbed(GridCell(col: 1, row: 0), start: one.0, end: one.1) == .start)
    }

    @Test func draggingAnEndPastThePivotFlipsTheRange() {
        let start = GridCell(col: 4, row: 2)
        let end = GridCell(col: 8, row: 2)
        let endPivot = TouchSelectionLogic.pivot(start: start, end: end, moving: .end)
        #expect(endPivot == start)
        let forward = TouchSelectionLogic.range(pivot: endPivot, finger: GridCell(col: 1, row: 5))
        #expect(forward.start == start && forward.end == GridCell(col: 2, row: 5))
        let startPivot = TouchSelectionLogic.pivot(start: start, end: end, moving: .start)
        #expect(startPivot == GridCell(col: 7, row: 2))
        let back = TouchSelectionLogic.range(pivot: startPivot, finger: GridCell(col: 0, row: 0))
        #expect(back.start == GridCell(col: 0, row: 0) && back.end == end)
        let past = TouchSelectionLogic.range(pivot: startPivot, finger: GridCell(col: 10, row: 2))
        #expect(past.start == startPivot && past.end == GridCell(col: 11, row: 2))
    }

    @Test func copiedLinesLoseOnlyTheirPadding() {
        #expect(CopyText.trimmingLineEnds("pad   \n  indented \t\nlast  ") == "pad\n  indented\nlast")
        #expect(CopyText.trimmingLineEnds("a b\n\nc") == "a b\n\nc")
    }

    // MARK: On the engine

    private func point(_ h: EngineHarness, col: Int, row: Int) -> CGPoint {
        let cell = h.engine.cellSize
        return CGPoint(x: (CGFloat(col) + 0.5) * cell.width, y: h.engine.gestureView.bounds.minY + (CGFloat(row) + 0.5) * cell.height)
    }

    @Test func aLongPressSelectsTheWordNotTheBuffer() async {
        let h = EngineHarness()
        h.feed("first line\r\nrun make test now\r\nlast")
        let selection = TouchSelection(target: h.engine)
        #expect(selection.selectWord(at: point(h, col: 5, row: 1)))
        #expect(h.engine.isSelecting)
        h.engine.copySelection()
        #expect(h.clipboardWrites == ["make"])
    }

    @Test func aBlankPressSelectsNothing() {
        let h = EngineHarness()
        h.feed("a     b")
        let selection = TouchSelection(target: h.engine)
        #expect(!selection.selectWord(at: point(h, col: 3, row: 0)))
        #expect(!h.engine.isSelecting)
    }

    @Test func anExtendedSelectionJoinsWrappedRowsAndDropsTrailingSpaces() {
        let h = EngineHarness()
        let cols = h.engine.size.cols
        let long = String(repeating: "a", count: cols) + "bbb"
        h.feed("pad   \r\n\(long)\r\nnext")
        let top = h.engine.viewportTop
        // From "pad" through the wrapped row and into "next", as two handle drags leave it.
        let range = TouchSelectionLogic.range(pivot: GridCell(col: 0, row: top), finger: GridCell(col: 3, row: top + 3))
        h.engine.selectCells(from: range.start, to: range.end)
        h.engine.copySelection()
        #expect(h.clipboardWrites == ["pad\n\(long)\nnext"])
    }

    @Test func aSelectionToTheRowsEndCopiesNoExtraBreak() {
        let h = EngineHarness()
        let cols = h.engine.size.cols
        h.feed("one two\r\nthree")
        let top = h.engine.viewportTop
        let range = TouchSelectionLogic.range(pivot: GridCell(col: 4, row: top), finger: GridCell(col: cols - 1, row: top))
        h.engine.selectCells(from: range.start, to: range.end)
        h.engine.copySelection()
        #expect(h.clipboardWrites == ["two"])
    }
}
