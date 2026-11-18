import UIKit

extension TogoTerminalView {
    /// Display widths of a visible row's cells: 2 and 0 for the halves of a wide glyph.
    func cellWidths(row: Int) -> [Int] {
        terminalStateSnapshot().visibleRows.dropFirst(row).first?.cellWidths ?? []
    }

    func showEditMenu(at point: CGPoint) {
        showStandardContextMenu(at: point)
    }

    @objc func performShortcut(_ command: UIKeyCommand) {
        guard let action = ShortcutAction(command: command) else { return }
        onShortcut?(action)
    }

}
