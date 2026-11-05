import Foundation

/// What may reach a terminal as pasted text.
///
/// Filtering by character class rather than removing known sequences: no ESC means no
/// sequence can be formed at all, so text can neither close a bracketed paste nor
/// reassemble an end marker from pieces. C1 controls go too, since U+009B is an 8-bit CSI.
/// Tab and line breaks stay; a multi-line message is the point of pasting.
public enum PasteText {
    public static func sanitize(_ text: String) -> String {
        var scalars = String.UnicodeScalarView()
        for scalar in text.unicodeScalars where isAllowed(scalar) {
            scalars.append(scalar)
        }
        return String(scalars)
    }

    private static func isAllowed(_ scalar: Unicode.Scalar) -> Bool {
        switch scalar.value {
        case 0x09, 0x0A, 0x0D: true
        case 0x00...0x1F, 0x7F...0x9F: false
        default: true
        }
    }
}
