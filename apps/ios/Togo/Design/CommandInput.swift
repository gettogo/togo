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
        let characters = Array(text)
        var result = ""
        var wordStart = true
        for (index, character) in characters.enumerated() {
            var replacement = String(character)
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

    /// Smart quotes and dashes undone as they land, keeping the field's other traits (the chat
    /// composer keeps autocorrect for prose).
    func straightPunctuation(_ text: Binding<String>) -> some View {
        onChange(of: text.wrappedValue) { _, now in
            let plain = CommandInput.straight(now)
            if plain != now { text.wrappedValue = plain }
        }
    }
}
