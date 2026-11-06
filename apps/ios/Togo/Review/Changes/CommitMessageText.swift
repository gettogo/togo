import Foundation

enum CommitMessageText {
    /// Undoes smart punctuation the keyboard inserted while typing: curly quotes become straight
    /// ones, and a lone en or em dash that replaced `--` goes back to two hyphens. A paste (more
    /// than one new character) keeps its dashes, since the writer chose them; quotes are always
    /// straightened because they carry no meaning in a commit message.
    static func straightened(_ text: String, previous: String) -> String {
        var result = text
            .replacingOccurrences(of: "\u{201C}", with: "\"")
            .replacingOccurrences(of: "\u{201D}", with: "\"")
            .replacingOccurrences(of: "\u{2018}", with: "'")
            .replacingOccurrences(of: "\u{2019}", with: "'")
        let typed = text.count <= previous.count + 1
        if typed, !previous.contains("\u{2014}"), !previous.contains("\u{2013}") {
            result = result.replacingOccurrences(of: "\u{2014}", with: "--").replacingOccurrences(of: "\u{2013}", with: "--")
        }
        return result
    }
}
