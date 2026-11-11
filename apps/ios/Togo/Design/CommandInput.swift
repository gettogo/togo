import SwiftUI

/// Fields whose text is run or typed exactly (terminal input, a reply into a session, a program,
/// a model id or path, a chat prompt that may hold `--flags`): no autocorrect, no capitals, and
/// the keyboard's smart punctuation undone as it lands. SwiftUI has no switch for smart quotes
/// and dashes, so the text is straightened instead.
enum CommandInput {
    /// A dash that starts a word turns back into hyphens (an em or en dash into `--`), curly
    /// double quotes into straight ones, and curly single quotes into straight ones unless they
    /// sit between two letters, as an apostrophe does. With `flattenLines`, line breaks and tabs
    /// become spaces. Everything else stays exactly as typed.
    static func straight(_ text: String, flattenLines: Bool = false) -> String {
        straight(Array(text), flattenLines: flattenLines, only: nil)
    }

    /// The most characters one keystroke changes: a curled quote, or an em dash put in place of
    /// the hyphen before it. A longer change is a paste, a dictated phrase or text the app set.
    static let keystroke = 2

    /// What a field holds after `new` replaced `old`. A keystroke's smart punctuation is
    /// undone; a paste keeps every character it was copied with (a pasted em dash or curly
    /// quote is the text, not the keyboard's doing), with only its line endings made LF. What
    /// was there before the change is never touched.
    static func settled(from old: String, to new: String) -> String {
        let before = Array(old)
        let after = Array(new)
        var head = 0
        while head < before.count, head < after.count, before[head] == after[head] { head += 1 }
        var tail = 0
        while tail < before.count - head, tail < after.count - head,
              before[before.count - 1 - tail] == after[after.count - 1 - tail] { tail += 1 }
        let inserted = head..<(after.count - tail)
        guard !inserted.isEmpty else { return new }
        if inserted.count <= keystroke { return straight(after, flattenLines: false, only: inserted) }
        let piece = String(after[inserted])
        let lines = lineFeeds(piece)
        return lines == piece ? new : String(after[..<head]) + lines + String(after[inserted.upperBound...])
    }

    /// CRLF and lone CR as LF, one each.
    static func lineFeeds(_ text: String) -> String {
        guard text.contains(where: { $0 == "\r" || $0 == "\r\n" }) else { return text }
        return text.replacingOccurrences(of: "\r\n", with: "\n").replacingOccurrences(of: "\r", with: "\n")
    }

    /// `straight` over `characters`, changing only those in `only` (all when nil); the others
    /// still give the context, such as whether a dash starts a word.
    private static func straight(_ characters: [Character], flattenLines: Bool, only: Range<Int>?) -> String {
        var result = ""
        var wordStart = true
        for (index, character) in characters.enumerated() {
            var replacement = String(character)
            if only?.contains(index) ?? true {
                switch character {
                case "\n", "\r", "\r\n", "\t":
                    if flattenLines { replacement = " " }
                case "\u{201C}", "\u{201D}":
                    replacement = "\""
                case "\u{2018}", "\u{2019}":
                    let before = index > 0 ? characters[index - 1] : nil
                    let after = index + 1 < characters.count ? characters[index + 1] : nil
                    if before?.isLetter != true || after?.isLetter != true { replacement = "'" }
                case "\u{2014}", "\u{2013}":
                    if wordStart { replacement = "--" }
                case "\u{2010}", "\u{2011}", "\u{2212}":
                    if wordStart { replacement = "-" }
                default:
                    break
                }
            }
            result += replacement
            // A run of dashes at the start of a word is still the start of that word.
            wordStart = replacement.allSatisfy(\.isWhitespace) || (wordStart && replacement.allSatisfy { $0 == "-" })
        }
        return result
    }
}

extension View {
    /// Autocorrect and capitals off, smart punctuation undone: for text that runs as typed.
    func commandInput(_ text: Binding<String>) -> some View {
        textInputAutocapitalization(.never)
            .autocorrectionDisabled()
            .straightPunctuation(text)
    }

    /// Smart quotes and dashes undone as they are typed, keeping the field's other traits (the
    /// chat composer keeps autocorrect for prose). Pasted text keeps its characters
    /// (`CommandInput.settled`).
    func straightPunctuation(_ text: Binding<String>) -> some View {
        onChange(of: text.wrappedValue) { old, now in
            let plain = CommandInput.settled(from: old, to: now)
            if plain != now { text.wrappedValue = plain }
        }
    }
}
