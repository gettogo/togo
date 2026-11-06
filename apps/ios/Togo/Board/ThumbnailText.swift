import Foundation
import TogoTerminal
import UIKit

/// A session's screen as a card shows it: plain text, the last lines only, never wider than a
/// terminal's 80 columns and never anything a terminal would act on.
enum ThumbnailText {
    static let columns = 80

    /// Drops escape sequences whole, so no colour codes or titles are left behind as text, then
    /// keeps only what `PasteText` lets through, which takes any stray control with it.
    static func clean(_ raw: String, lines: Int) -> String {
        // "\r\n" is one Character in Swift, so line ends are made plain before splitting.
        let plain = PasteText.sanitize(stripSequences(raw))
            .replacingOccurrences(of: "\r\n", with: "\n")
            .replacingOccurrences(of: "\r", with: "")
        var kept = plain
            .split(separator: "\n", omittingEmptySubsequences: false)
            .map { line in
                let text = line.replacingOccurrences(of: "\t", with: "    ")
                return String(text.prefix(columns)).trimmingTrailingSpaces()
            }
        while kept.last?.isEmpty == true {
            kept.removeLast()
        }
        return kept.suffix(lines).joined(separator: "\n")
    }

    /// The point size of SF Mono at which `columns` characters fill `width`.
    static func fontSize(fitting width: CGFloat) -> CGFloat {
        let probe: CGFloat = 100
        let font = UIFont.monospacedSystemFont(ofSize: probe, weight: .regular)
        let advance = ("M" as NSString).size(withAttributes: [.font: font]).width / probe
        return max(1, (width / (CGFloat(columns) * advance) * 10).rounded(.down) / 10)
    }

    /// Removes CSI (`ESC [`, or 8-bit `U+009B`), OSC, DCS and other string sequences, and
    /// two-character escapes.
    private static func stripSequences(_ text: String) -> String {
        var out = String.UnicodeScalarView()
        var scalars = text.unicodeScalars.makeIterator()
        while let scalar = scalars.next() {
            switch scalar.value {
            case 0x1B:
                guard let next = scalars.next() else { return String(out) }
                switch next {
                case "[": skipControlSequence(&scalars)
                case "]", "P", "X", "^", "_": skipString(&scalars)
                default: break
                }
            case 0x9B:
                skipControlSequence(&scalars)
            case 0x9D, 0x90, 0x98, 0x9E, 0x9F:
                skipString(&scalars)
            default:
                out.append(scalar)
            }
        }
        return String(out)
    }

    /// Parameters and intermediates up to the final byte, 0x40 to 0x7E.
    private static func skipControlSequence(_ scalars: inout String.UnicodeScalarView.Iterator) {
        while let scalar = scalars.next() {
            if (0x40...0x7E).contains(scalar.value) { return }
        }
    }

    /// Up to BEL, ST (`ESC \`) or the 8-bit ST.
    private static func skipString(_ scalars: inout String.UnicodeScalarView.Iterator) {
        while let scalar = scalars.next() {
            switch scalar.value {
            case 0x07, 0x9C: return
            case 0x1B:
                _ = scalars.next()
                return
            default: continue
            }
        }
    }
}

private extension String {
    func trimmingTrailingSpaces() -> String {
        var text = self
        while text.last == " " {
            text.removeLast()
        }
        return text
    }
}
