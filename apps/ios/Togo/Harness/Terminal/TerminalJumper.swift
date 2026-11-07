import TogoTerminal
import UIKit

/// Carries out a jump on a live terminal: reads its scrollback, scrolls so the line sits a
/// third of the way down, settles the row by its needle and marks it.
@MainActor enum TerminalJumper {
    /// False when the phone does not hold the line, so the capture sheet should show it.
    static func perform(_ jump: TerminalJump, engine: SwiftTermEngine, container: TerminalContainer) -> Bool {
        let frame = engine.settleScrollback()
        switch JumpLanding.resolve(linesFromBottom: jump.linesFromBottom, frame: frame) {
        case .capture:
            return false
        case .scroll(let top, let row):
            engine.scroll(toTop: top)
            let target = JumpLanding.refine(row: row, needle: jump.needle, rows: engine.visibleRows())
            container.view.layoutIfNeeded()
            container.showBand(engine.rowFrame(target, in: container.view))
            UIAccessibility.post(notification: .layoutChanged, argument: nil)
            return true
        }
    }
}
