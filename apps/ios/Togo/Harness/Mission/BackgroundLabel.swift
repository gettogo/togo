import Foundation
import TogoKit

/// What a background job's row says. The title is a short name: the shell's own description
/// when the adapter gave one, otherwise the command cut down to its first meaningful words. The
/// subtitle is what the job printed last, with how long it has run.
enum BackgroundLabel {
    /// The longest a shortened command runs before it stops at a word.
    static let maxTitle = 40

    static func title(_ row: HarnessRow) -> String {
        let title = row.title.trimmingCharacters(in: .whitespacesAndNewlines)
        let command = (row.subtitle ?? "").trimmingCharacters(in: .whitespacesAndNewlines)
        if command.isEmpty {
            return looksLikeCommand(title) ? shorten(title) : title
        }
        if title.isEmpty || isCommand(title, of: command) { return shorten(command) }
        return title
    }

    /// The last line the job printed and, while it runs, how long it has: "tick 4/12 · 0:20".
    static func subtitle(_ row: HarnessRow, now: Date) -> String {
        [lastLine(row), clock(row, now: now)].compactMap(\.self).joined(separator: " · ")
    }

    /// The newest non-empty output line; "No output yet" while a silent job runs.
    static func lastLine(_ row: HarnessRow) -> String? {
        let last = (row.tail ?? []).last { !$0.trimmingCharacters(in: .whitespaces).isEmpty }?
            .trimmingCharacters(in: .whitespaces)
        return last ?? (row.state.isLive ? MissionCopy.noOutputYet : nil)
    }

    /// How long a running job has run; nil once it ended.
    static func clock(_ row: HarnessRow, now: Date) -> String? {
        guard row.state.isLive, let start = row.startedAt?.date else { return nil }
        return timer(max(0, now.timeIntervalSince(start)))
    }

    /// "0:20", "12:05", "1:02:03", as the row's timers read.
    static func timer(_ seconds: TimeInterval) -> String {
        let total = Int(seconds.rounded(.down))
        let h = total / 3600, m = (total % 3600) / 60, s = total % 60
        return h > 0 ? String(format: "%d:%02d:%02d", h, m, s) : String(format: "%d:%02d", m, s)
    }

    /// The title is the command, or its first line, rather than a name for it.
    static func isCommand(_ title: String, of command: String) -> Bool {
        title == command || command.hasPrefix(title) || title.hasPrefix(command)
    }

    static func looksLikeCommand(_ text: String) -> Bool {
        text.contains { ";|&$<>`".contains($0) } || text.count > maxTitle
    }

    // MARK: Shortening

    /// Words that only set the scene for the real command.
    private static let wrappers: Set<String> = ["sudo", "nohup", "exec", "time", "env", "command", "nice", "caffeinate"]
    /// Segments that do not name the work: moving, exporting, sourcing, shell options.
    private static let setup: Set<String> = ["cd", "pushd", "export", "source", ".", "set", "unset", "true", ":"]
    private static let loops: Set<String> = ["for", "while", "until"]
    private static let loopNoise: Set<String> = ["do", "done", "then", "fi", "else"]

    /// "npm run dev" stays; "cd app && FOO=1 npm run build -- --watch" becomes "npm run build";
    /// a loop becomes "Loop: echo, sleep".
    static func shorten(_ command: String) -> String {
        let segments = segments(of: command)
        guard !segments.isEmpty else { return command.trimmingCharacters(in: .whitespacesAndNewlines) }
        if let first = segments.first?.first, loops.contains(first) {
            let body = segments.dropFirst()
                .compactMap { programAndArgs(Array($0.drop { loopNoise.contains($0) }))?.first }
                .filter { !loops.contains($0) }
            var names: [String] = []
            for name in body where !names.contains(name) { names.append(name) }
            if !names.isEmpty { return MissionCopy.loop(names.prefix(3).joined(separator: ", ")) }
        }
        let meaningful = segments.compactMap(programAndArgs).first { !setup.contains($0[0]) }
            ?? segments.compactMap(programAndArgs).first
        guard let words = meaningful else { return clip(command) }
        var out: [String] = []
        for word in words {
            if word == "--" { break }
            if out.count >= 2, word.hasPrefix("-") { break }
            let next = (out + [word]).joined(separator: " ")
            if next.count > maxTitle, !out.isEmpty { break }
            out.append(word)
            if out.count == 4 { break }
        }
        return clip(out.joined(separator: " "))
    }

    /// One segment's words with environment assignments and wrappers taken off the front; quoted
    /// words keep their text without the quotes.
    private static func programAndArgs(_ words: [String]) -> [String]? {
        // "/usr/bin/env" reads as "env", "/usr/bin/python3" as "python3".
        func base(_ word: String) -> String {
            word.contains("/") && !word.hasSuffix("/") ? String(word.split(separator: "/").last ?? Substring(word)) : word
        }
        var rest = words[...]
        while let first = rest.first, isAssignment(first) || wrappers.contains(base(first)) { rest = rest.dropFirst() }
        if let first = rest.first, base(first) == "timeout", rest.count > 2 { rest = rest.dropFirst(2) }
        guard let program = rest.first else { return nil }
        return [base(program)] + rest.dropFirst()
    }

    private static func isAssignment(_ word: String) -> Bool {
        guard let eq = word.firstIndex(of: "="), eq != word.startIndex else { return false }
        return word[..<eq].allSatisfy { $0.isLetter || $0.isNumber || $0 == "_" }
    }

    private static func clip(_ text: String) -> String {
        guard text.count > maxTitle else { return text }
        let cut = text.prefix(maxTitle)
        let word = cut.lastIndex(of: " ").map { cut[..<$0] } ?? cut
        return String(word) + "…"
    }

    /// Splits a command line into its segments at `;`, `&&`, `||`, `|`, `&` and newlines outside
    /// quotes and `$( )`, each a list of words.
    static func segments(of command: String) -> [[String]] {
        var segments: [[String]] = []
        var words: [String] = []
        var word = ""
        var quote: Character?
        var depth = 0
        var hadWord = false
        func endWord() {
            if hadWord { words.append(word) }
            word = ""
            hadWord = false
        }
        func endSegment() {
            endWord()
            if !words.isEmpty { segments.append(words) }
            words = []
        }
        for ch in command {
            if let q = quote {
                if ch == q { quote = nil } else { word.append(ch) }
                continue
            }
            if depth > 0 {
                word.append(ch)
                if ch == "(" { depth += 1 } else if ch == ")" { depth -= 1 }
                continue
            }
            switch ch {
            case "\"", "'":
                quote = ch
                hadWord = true
            case "(" where word.hasSuffix("$"):
                word.append(ch)
                depth = 1
            case ";", "|", "&", "\n":
                endSegment()
            case " ", "\t":
                endWord()
            default:
                word.append(ch)
                hadWord = true
            }
        }
        endSegment()
        return segments
    }
}
