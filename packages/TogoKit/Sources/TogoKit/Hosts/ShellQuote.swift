/// Quotes argv for a shell, per `session.attachCommand` in protocol/v1/schema.json.
///
/// Safe in sh, bash, zsh and fish, which disagree about backslashes inside single quotes
/// (POSIX keeps them literal, fish treats `\\` and `\'` as escapes). So single quotes only
/// ever wrap runs with no `'` and no `\`; each of those two is written outside the quotes
/// as `\'` or `\\`, which every one of these shells reads as the literal character.
/// An empty element becomes `''`.
///
/// The scan works on Unicode scalars, never Characters: a `'` followed by a combining
/// mark or joiner is one grapheme cluster that does not compare equal to `"'"`, so a
/// Character-level scan would leave that quote unescaped and let the rest of the
/// element run as shell code.
///
/// Known limit: csh and tcsh still expand `!` (even inside quotes) and reject a newline, so
/// an element with either fails to run there; it fails closed rather than running other code.
public func shellQuote(_ argv: [String]) -> String {
    argv.map(quoteElement).joined(separator: " ")
}

private func quoteElement(_ element: String) -> String {
    if element.isEmpty { return "''" }
    var out = String.UnicodeScalarView()
    var inRun = false
    for scalar in element.unicodeScalars {
        if scalar == "'" || scalar == "\\" {
            if inRun {
                out.append("'")
                inRun = false
            }
            out.append("\\")
            out.append(scalar)
        } else {
            if !inRun {
                out.append("'")
                inRun = true
            }
            out.append(scalar)
        }
    }
    if inRun { out.append("'") }
    return String(out)
}

/// Runs `script` under `/bin/sh` whatever the account's login shell is, so a fish,
/// nushell or csh login shell only has to parse one single-quoted word.
public func shellCommand(_ script: String) -> String {
    "/bin/sh -c " + shellQuote([script])
}
