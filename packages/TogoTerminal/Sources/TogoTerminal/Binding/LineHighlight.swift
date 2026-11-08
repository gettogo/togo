internal import SwiftTerm
import UIKit

/// Where the terminal's scrollback stands, for scrolling to a line the helper located:
/// `base` lines of history sit above the live screen of `rows` rows, and the cursor is on
/// `cursorRow` of that screen.
public struct ScrollbackFrame: Equatable, Sendable {
    /// Lines of history above the live screen. Zero without scrollback, as over Mosh, on the
    /// alternate screen, or right after an attach.
    public var base: Int
    public var rows: Int
    /// The cursor's row on the live screen, from 0 at the top.
    public var cursorRow: Int
    /// The first line shown now, counted from the oldest line of history.
    public var top: Int

    public init(base: Int, rows: Int, cursorRow: Int, top: Int) {
        self.base = base
        self.rows = rows
        self.cursorRow = cursorRow
        self.top = top
    }
}

/// Scrolling to a line and finding it on screen, for "Show in terminal". The terminal reports
/// its scrollback, scrolls to a first visible line, reads the rows on screen, and says where a
/// row is drawn; the app works out the line and draws the band over it.
extension SwiftTermEngine {
    /// The scrollback as it stands, read from the live screen: the view goes back to the bottom
    /// first, since history depth can only be read there. Nil before the grid has a size.
    public func settleScrollback() -> ScrollbackFrame? {
        guard let input = terminalView.terminalInputStateSnapshot() else { return nil }
        let rows = input.dimensions.rows
        guard rows > 0 else { return nil }
        let scrollable = !input.isAlternateBuffer && terminalView.canScroll
        if scrollable, terminalView.scrollPosition < 1 {
            terminalView.scroll(toPosition: 1)
        }
        let state = terminalView.terminalStateSnapshot()
        let base = scrollable ? max(0, state.viewportRow) : 0
        return ScrollbackFrame(base: base, rows: rows, cursorRow: state.cursor.row, top: base)
    }

    /// Shows `line` (counted from the oldest line of history) as the first visible row,
    /// clamped to what the buffer holds.
    public func scroll(toTop line: Int) {
        terminalView.scrollTo(row: max(0, line))
    }

    /// The text of the rows on screen now, from the top.
    public func visibleRows() -> [String] {
        guard let input = terminalView.terminalInputStateSnapshot() else { return [] }
        return terminalView.visibleRowsText(0..<input.dimensions.rows)
    }

    /// Where row `row` of the screen is drawn, full width, in `space`'s coordinates.
    public func rowFrame(_ row: Int, in space: UIView) -> CGRect {
        let cell = cellSize
        guard cell.height > 0 else { return .null }
        let bounds = terminalView.bounds
        let rect = CGRect(x: bounds.minX, y: bounds.minY + CGFloat(row) * cell.height, width: bounds.width, height: cell.height)
        return terminalView.convert(rect, to: space)
    }

    /// True while another layer covers the terminal, as the chat does: the terminal lets the
    /// keyboard go and may not take it, and VoiceOver passes it by. Uncovering it does not
    /// raise the keyboard; the next tap on the terminal does.
    ///
    /// While covered it keeps parsing output but stops drawing: once the chat has faded in over
    /// it, the view is hidden, so an agent's spinner does not keep the GPU busy under an opaque
    /// chat. Uncovering shows it at once and redraws it whole.
    public var isCovered: Bool {
        get { terminalView.covered }
        set {
            guard newValue != terminalView.covered else { return }
            let view = terminalView
            view.covered = newValue
            view.accessibilityElementsHidden = newValue
            view.wantsFocus = false
            view.coverGeneration += 1
            if newValue {
                _ = view.resignFirstResponder()
                let generation = view.coverGeneration
                Task { @MainActor [weak view] in
                    try? await Task.sleep(for: Self.hideAfterCover)
                    guard let view, view.covered, view.coverGeneration == generation else { return }
                    view.isHidden = true
                }
            } else if view.isHidden {
                view.isHidden = false
                view.setNeedsDisplay()
            }
        }
    }

    /// Longer than the chat's fade over the terminal, so it never vanishes under a chat that is
    /// still see-through.
    static let hideAfterCover: Duration = .milliseconds(700)
}
