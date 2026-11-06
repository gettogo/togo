import Foundation

/// Removes what could identify a person or a machine from text before it is kept or exported:
/// addresses, host names, user names, paths, keys and long secrets.
public enum Redactor {
    private struct Rule: @unchecked Sendable {
        let expression: NSRegularExpression
        let replacement: String
        /// Called with each match; false leaves the text as it was.
        let accepts: @Sendable (String) -> Bool

        init(_ pattern: String, _ replacement: String, accepts: @escaping @Sendable (String) -> Bool = { _ in true }) {
            // The patterns are literals in this file.
            // swiftlint:disable:next force_try
            expression = try! NSRegularExpression(pattern: pattern, options: [])
            self.replacement = replacement
            self.accepts = accepts
        }
    }

    private static let word = "[\\p{L}\\p{N}_-]"
    private static let pathChar = #"[^\s'"<>,;()\[\]{}]"#
    /// A path segment may hold spaces when a slash follows it.
    private static let homePath = #"(?:[^/\n'"<>]+/)*"#

    /// Applied in order. Keys and paths go first because they contain text the later rules
    /// would otherwise take apart.
    private static let rules: [Rule] = [
        Rule(#"-----BEGIN [A-Z0-9 ]+-----[\s\S]*?(?:-----END [A-Z0-9 ]+-----|\z)"#, "<key>"),
        Rule(#"(?:sk-)?(?:ssh|ecdsa)-[A-Za-z0-9@.-]+[ \t]+AAAA[A-Za-z0-9+/]{4,}={0,3}(?:[ \t]+[^\s]+)?"#, "<key>"),
        Rule(#"\b[a-zA-Z][a-zA-Z0-9+.-]*://[^\s'"<>]+"#, "<url>"),
        // The name before an @, the host or bracketed address after it, and an scp-style path.
        Rule(
            #"[A-Za-z0-9._%+-]+@(?:\[[^\]\s]*\]|[A-Za-z0-9._-]+)(?::[^\s'"<>,;]+)?"#, "<user>@<host>"
        ),
        Rule("(?:/Users|/home)/\(homePath)\(pathChar)*", "<path>"),
        Rule("~[\\w.-]*/\(homePath)\(pathChar)*", "<path>"),
        Rule(#"\b[A-Za-z]:\\(?:[^\\\n'"<>]+\\)*[^\s'"<>]*"#, "<path>"),
        Rule(#"\\\\[^\s'"<>]+"#, "<path>"),
        Rule("(?<![\\w<>:/.~-])/(?:[\\w.@+-]+/)+[\\w.@+-]*", "<path>"),
        Rule("(?<![\\w<>/.~-])[\\w.@+-]+(?:/[\\w.@+-]+){2,}", "<path>"),
        Rule(#"(?:[0-9A-Fa-f]{2}:){7,}[0-9A-Fa-f]{2}"#, "<secret>"),
        Rule(
            #"(?<![\w:])(?:[0-9A-Fa-f]{0,4}:){2,7}(?:(?:\d{1,3}\.){3}\d{1,3}|[0-9A-Fa-f]{1,4})?(?:%[A-Za-z0-9._-]+)?"#,
            "<address>", accepts: { $0.contains("::") || $0.filter { $0 == ":" }.count >= 7 }
        ),
        Rule(#"(?<![\w.])(?:\d{1,3}\.){3}\d{1,3}(?![\w])(?::\d{1,5})?"#, "<address>"),
        Rule(#"<address>(?:\.\d{1,3})+"#, "<address>"),
        Rule(#"eyJ[\w-]+\.[\w-]+\.[\w-]*"#, "<secret>"),
        Rule(#"[0-9A-Fa-f]{16,}"#, "<secret>"),
        Rule(#"[A-Za-z0-9+/_=-]{20,}"#, "<secret>", accepts: looksRandom),
        Rule(
            "(?<![\\p{L}\\p{N}_<-])\(word)+(?:\\.\(word)+)+(?::\\d{1,5})?", "<host>",
            // The last label starts with a letter, which keeps "2.5s" and "1.2.3-beta" as they are.
            accepts: { token in
                let name = token.split(separator: ":").first.map(String.init) ?? token
                return name.split(separator: ".").last?.first?.isLetter == true
            }
        ),
    ]

    /// Text with everything identifying replaced by a placeholder.
    public static func scrub(_ text: String) -> String {
        scrub(text, known: [])
    }

    /// Like `scrub`, and also replaces each of `known` (host names, users, session names the
    /// app has seen) wherever it appears, ignoring case. Names under three characters are skipped.
    public static func scrub(_ text: String, known: [String]) -> String {
        var result = text
        for expression in knownExpressions(known) {
            let range = NSRange(result.startIndex..., in: result)
            result = expression.stringByReplacingMatches(in: result, options: [], range: range, withTemplate: "<redacted>")
        }
        for rule in rules {
            result = apply(rule, to: result)
        }
        return result
    }

    /// Whether `text` mentions any of `known`, with the same leniency `scrub` has.
    public static func mentions(_ text: String, known: [String]) -> Bool {
        let range = NSRange(text.startIndex..., in: text)
        return knownExpressions(known).contains { $0.firstMatch(in: text, options: [], range: range) != nil }
    }

    private static let separators = #"[-_.\s'\x{2019}\x{2018}`]"#

    /// One pattern per name. Case, separators (hyphen, underscore, dot, space) and apostrophes
    /// do not matter, so `deploy-prod-db` also finds `deploy prod db` and `Jamie's` finds `Jamie’s`.
    private static func knownExpressions(_ known: [String]) -> [NSRegularExpression] {
        known.compactMap { name in
            let parts = name.split(whereSeparator: { "-_. \t\n'\u{2019}\u{2018}`".contains($0) }).map(String.init)
            guard parts.joined().count >= 3 else { return nil }
            let pattern = parts.map(NSRegularExpression.escapedPattern(for:)).joined(separator: separators + "*")
            return try? NSRegularExpression(pattern: pattern, options: .caseInsensitive)
        }
    }

    private static func apply(_ rule: Rule, to text: String) -> String {
        let whole = NSRange(text.startIndex..., in: text)
        let matches = rule.expression.matches(in: text, options: [], range: whole)
        guard !matches.isEmpty else { return text }
        var result = text
        for match in matches.reversed() {
            guard let range = Range(match.range, in: result), rule.accepts(String(result[range])) else { continue }
            result.replaceSubrange(range, with: rule.replacement)
        }
        return result
    }

    /// Long runs that read as words, such as `connection-refused-after-three-attempts`, stay.
    private static let looksRandom: @Sendable (String) -> Bool = { token in
        token.contains { $0.isNumber } || (token.contains { $0.isUppercase } && token.contains { $0.isLowercase })
            // A long run of plain letters is not a sentence.
            || (token.count >= 32 && !token.contains("-") && !token.contains("_"))
    }
}
