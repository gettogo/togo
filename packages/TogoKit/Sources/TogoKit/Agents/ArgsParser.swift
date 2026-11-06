import Foundation

public enum ArgsError: Error, Equatable, Sendable {
    /// A quote or a trailing backslash is not closed.
    case unbalancedQuote
    /// A control character, such as a newline or tab, which the helper refuses in arguments.
    case controlCharacter
    /// More arguments, or longer ones, than the helper accepts.
    case tooLong
}

/// Turns a line of flags into an argv list and back, by POSIX word-splitting rules for quotes
/// and backslashes only. Nothing is ever expanded: `$`, `~`, globs and backticks stay as typed,
/// because the list goes to the program as separate arguments and no shell reads it.
public enum ArgsParser {
    /// What `session.create` accepts.
    public static let maxCount = 64
    public static let maxBytes = 4096
    public static let maxTotalBytes = 16_384

    /// Splits on spaces. Single quotes keep everything literally; double quotes keep everything
    /// except that a backslash escapes `\`, `"`, `$` and a backtick; outside quotes a backslash
    /// escapes any character.
    public static func split(_ line: String) throws(ArgsError) -> [String] {
        var words: [String] = []
        var word = String.UnicodeScalarView()
        var inWord = false
        var scalars = line.unicodeScalars.makeIterator()

        while let scalar = scalars.next() {
            if scalar.properties.generalCategory == .control { throw .controlCharacter }
            switch scalar {
            case " ":
                if inWord {
                    words.append(String(word))
                    word.removeAll()
                    inWord = false
                }
            case "'":
                inWord = true
                while true {
                    guard let next = scalars.next() else { throw .unbalancedQuote }
                    if next.properties.generalCategory == .control { throw .controlCharacter }
                    if next == "'" { break }
                    word.append(next)
                }
            case "\"":
                inWord = true
                while true {
                    guard let next = scalars.next() else { throw .unbalancedQuote }
                    if next.properties.generalCategory == .control { throw .controlCharacter }
                    if next == "\"" { break }
                    if next == "\\" {
                        guard let escaped = scalars.next() else { throw .unbalancedQuote }
                        if escaped.properties.generalCategory == .control { throw .controlCharacter }
                        if !"\\\"$`".unicodeScalars.contains(escaped) { word.append(next) }
                        word.append(escaped)
                    } else {
                        word.append(next)
                    }
                }
            case "\\":
                guard let escaped = scalars.next() else { throw .unbalancedQuote }
                if escaped.properties.generalCategory == .control { throw .controlCharacter }
                inWord = true
                word.append(escaped)
            default:
                inWord = true
                word.append(scalar)
            }
        }
        if inWord { words.append(String(word)) }
        try checkLimits(words)
        return words
    }

    /// One line that `split` turns back into `args`. Arguments made only of characters no
    /// shell treats specially are left bare; every other one is single-quoted.
    public static func join(_ args: [String]) -> String {
        args.map(quoted).joined(separator: " ")
    }

    private static let bare = Set("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-".unicodeScalars)

    private static func quoted(_ arg: String) -> String {
        if !arg.isEmpty, arg.unicodeScalars.allSatisfy(bare.contains) { return arg }
        return "'" + arg.replacingOccurrences(of: "'", with: #"'\''"#) + "'"
    }

    private static func checkLimits(_ words: [String]) throws(ArgsError) {
        guard words.count <= maxCount else { throw .tooLong }
        var total = 0
        for word in words {
            let bytes = word.utf8.count
            guard bytes <= maxBytes else { throw .tooLong }
            total += bytes
        }
        guard total <= maxTotalBytes else { throw .tooLong }
    }
}
