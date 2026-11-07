internal import SwiftTerm
import Foundation

extension SwiftTermEngine {
    /// Every web link on the screen and in the last `scrollback` rows above it, newest first,
    /// with URLs the program broke across rows joined again.
    public func linksOnScreen(scrollback: Int = 500) -> [URL] {
        guard let snapshot = terminalView.terminalContentSnapshot(region: .history(maximumScrollbackRows: scrollback)) else {
            return []
        }
        let rows = snapshot.rows.map { LinkScan.Row($0.text, wrapped: $0.isWrapped) }
        return LinkScan.links(in: rows, columns: snapshot.inputState.dimensions.cols)
    }
}
