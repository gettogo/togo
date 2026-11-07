internal import SwiftTerm
import UIKit

extension SwiftTermEngine: GestureTarget {
    var gestureView: UIView { terminalView }
    var tracksMouse: Bool { mouseReporting }
    var hasKeyboardFocus: Bool { terminalView.isFirstResponder }
    var fontSize: CGFloat { terminalView.font.pointSize }

    func setFontSize(_ size: CGFloat) {
        var next = settings
        next.fontSize = size
        apply(next)
    }

    /// The advance of "W", the glyph SwiftTerm sizes its cells by.
    func charWidth(forFontSize size: CGFloat) -> CGFloat {
        var next = settings
        next.fontSize = size
        let font = Self.font(for: next) as CTFont
        var character: UniChar = 0x57
        var glyph: CGGlyph = 0
        guard CTFontGetGlyphsForCharacters(font, &character, &glyph, 1) else { return size * 0.6 }
        var advance = CGSize.zero
        CTFontGetAdvancesForGlyphs(font, .horizontal, &glyph, &advance, 1)
        return advance.width
    }

    func sendBytes(_ bytes: [UInt8]) {
        write(bytes)
    }

    var scrollPan: UIGestureRecognizer { terminalView.panGestureRecognizer }

    var mouseTracking: MouseTracking {
        switch terminalView.terminalInputStateSnapshot()?.mouseMode ?? .off {
        case .off: .off
        case .x10: .presses
        case .vt200: .clicks
        case .buttonEventTracking: .drags
        case .anyEvent: .anyMotion
        }
    }

    /// SwiftTerm encodes whatever it is handed, whatever the mode, so the mode is checked here.
    func sendMouse(button: Int, release: Bool, motion: Bool, at cell: GridCell) {
        guard mouseTracking.reports(release: release, motion: motion) else { return }
        terminalView.sendMouseEvent(button: button, release: release, motion: motion, col: cell.col, row: cell.row)
    }

    func open(_ url: URL) {
        guard LinkPolicy.allows(url) else { return }
        onOpenURL(url)
    }

    func copyToClipboard(_ text: String) {
        onClipboardWrite(text)
    }
}
