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

    /// A paste as the pty should get it: line endings as LF (CRLF and a lone CR each become one
    /// LF, never two), the spaces web pages and chat apps put in copied commands as plain
    /// spaces, characters nobody can see (zero-width space, word joiner, byte order mark, soft
    /// hyphen) dropped, then `sanitize`. Nothing is ever added: no quotes are curled or
    /// straightened, tabs stay tabs, and the text keeps its own trailing newline or lack of one.
    public static func terminal(_ text: String) -> String {
        var scalars = String.UnicodeScalarView()
        var afterCR = false
        for scalar in text.unicodeScalars {
            defer { afterCR = scalar == "\r" }
            switch scalar.value {
            case 0x0D:
                scalars.append("\n")
            case 0x0A where afterCR:
                continue
            case 0x00A0, 0x2007, 0x202F:
                scalars.append(" ")
            case 0x200B, 0x2060, 0xFEFF, 0x00AD:
                continue
            default:
                if isAllowed(scalar) { scalars.append(scalar) }
            }
        }
        return String(scalars)
    }

    /// The bytes of a paste: `terminal(text)`, inside the bracketed-paste markers when the
    /// remote enabled mode 2004. Empty when nothing survives, so an empty paste sends nothing.
    public static func payload(_ text: String, bracketed: Bool) -> [UInt8] {
        let body = terminal(text)
        guard !body.isEmpty else { return [] }
        return bracketed ? Array("\u{1b}[200~\(body)\u{1b}[201~".utf8) : Array(body.utf8)
    }

    /// The most one write carries. A long log or file pasted whole goes out as several writes in
    /// order, so no single transport frame grows with the paste.
    public static let chunkSize = 4096

    /// `bytes` in pieces of at most `size`, each ending on a character boundary: a multi-byte
    /// UTF-8 character is never split between two writes.
    public static func chunks(_ bytes: [UInt8], size: Int = chunkSize) -> [ArraySlice<UInt8>] {
        precondition(size >= 4, "a chunk must hold the longest UTF-8 character")
        var out: [ArraySlice<UInt8>] = []
        var start = 0
        while start < bytes.count {
            var end = min(start + size, bytes.count)
            // Back off continuation bytes (10xxxxxx) so the piece ends where a character does.
            while end < bytes.count, end > start, bytes[end] & 0xC0 == 0x80 { end -= 1 }
            out.append(bytes[start..<end])
            start = end
        }
        return out
    }

    private static func isAllowed(_ scalar: Unicode.Scalar) -> Bool {
        switch scalar.value {
        case 0x09, 0x0A, 0x0D: true
        case 0x00...0x1F, 0x7F...0x9F: false
        default: true
        }
    }
}
