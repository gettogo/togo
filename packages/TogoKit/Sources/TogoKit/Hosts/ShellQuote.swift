/// Quotes argv for a POSIX shell, per `session.attachCommand` in protocol/v1/schema.json:
/// each element wrapped in `'`, each `'` inside replaced by `'\''`, joined with single spaces.
///
/// Inside single quotes a POSIX shell interprets nothing, so every byte arrives as sent.
///
/// The replace works on Unicode scalars, never Characters: a `'` followed by a combining
/// mark or joiner is one grapheme cluster that does not compare equal to `"'"`, so a
/// Character-level replace would leave that quote unescaped and let the rest of the
/// element run as shell code. U+0027 is the only scalar that encodes to byte 0x27.
public func shellQuote(_ argv: [String]) -> String {
    argv.map(quoteElement).joined(separator: " ")
}

private func quoteElement(_ element: String) -> String {
    var scalars = String.UnicodeScalarView()
    scalars.append("'")
    for scalar in element.unicodeScalars {
        if scalar == "'" {
            scalars.append(contentsOf: #"'\''"#.unicodeScalars)
        } else {
            scalars.append(scalar)
        }
    }
    scalars.append("'")
    return String(scalars)
}

/// Runs `script` under `/bin/sh` whatever the account's login shell is, so a fish,
/// nushell or csh login shell only has to parse one single-quoted word.
public func shellCommand(_ script: String) -> String {
    "/bin/sh -c " + shellQuote([script])
}
