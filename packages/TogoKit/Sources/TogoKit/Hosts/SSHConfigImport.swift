import Foundation

public struct ImportedJump: Hashable, Sendable {
    public var host: String
    public var user: String?
    public var port: Int?
    public var alias: String?

    public init(host: String, user: String? = nil, port: Int? = nil, alias: String? = nil) {
        self.host = host
        self.user = user
        self.port = port
        self.alias = alias
    }
}

public struct ImportedHost: Hashable, Sendable {
    public var alias: String
    public var hostName: String
    public var user: String?
    public var port: Int?
    public var jump: ImportedJump?

    public init(alias: String, hostName: String, user: String? = nil, port: Int? = nil, jump: ImportedJump? = nil) {
        self.alias = alias
        self.hostName = hostName
        self.user = user
        self.port = port
        self.jump = jump
    }
}

public struct ImportResult: Hashable, Sendable {
    public struct Skipped: Hashable, Sendable {
        public var line: Int
        public var keyword: String
        public var reason: String

        public init(line: Int, keyword: String, reason: String) {
            self.line = line
            self.keyword = keyword
            self.reason = reason
        }
    }

    public var hosts: [ImportedHost]
    public var skipped: [Skipped]

    public init(hosts: [ImportedHost] = [], skipped: [Skipped] = []) {
        self.hosts = hosts
        self.skipped = skipped
    }
}

public enum SSHConfigImportError: Error, Equatable {
    case tooLarge
}

/// Reads OpenSSH client config text and lists the hosts it defines, applying wildcard blocks as
/// defaults with OpenSSH's first-value-wins rule. Anything it cannot honour is reported, not guessed.
public enum SSHConfigImport {
    public static let maxBytes = 262_144
    /// Longest Host pattern or alias, in characters, that is read.
    public static let maxNameLength = 255
    /// Most Host patterns read from one file.
    public static let maxPatterns = 1_000
    /// Total glob comparison steps allowed per file, so no input can make matching slow.
    private static let matchBudget = 250_000

    public static func parse(_ text: String) throws -> ImportResult {
        guard text.utf8.count <= maxBytes else { throw SSHConfigImportError.tooLarge }

        var blocks: [Block] = [Block(patterns: ["*"])]
        var aliases: [String] = []
        var seen = Set<String>()
        var skipped: [ImportResult.Skipped] = []
        var inMatch = false
        var patternCount = 0
        var patternsCapped = false

        for (index, rawLine) in physicalLines(text.hasPrefix("\u{FEFF}") ? String(text.dropFirst()) : text).enumerated() {
            let line = index + 1
            guard let (keyword, value) = splitLine(rawLine) else { continue }
            let lower = keyword.lowercased()
            if value.isEmpty, lower != "host" { continue }

            if lower == "host" {
                inMatch = false
                var patterns: [String] = []
                for pattern in tokens(value) ?? [] where !pattern.isEmpty {
                    if pattern.count > maxNameLength {
                        skipped.append(.init(line: line, keyword: keyword, reason: "Name is too long."))
                    } else if patternCount >= maxPatterns {
                        if !patternsCapped {
                            patternsCapped = true
                            skipped.append(.init(line: line, keyword: keyword, reason: "Too many hosts; the rest were skipped."))
                        }
                    } else {
                        patternCount += 1
                        patterns.append(pattern)
                    }
                }
                blocks.append(Block(patterns: patterns))
                for pattern in patterns where isLiteral(pattern) && seen.insert(pattern).inserted {
                    aliases.append(pattern)
                }
                continue
            }
            if lower == "match" {
                inMatch = true
                skipped.append(.init(line: line, keyword: keyword, reason: "Match blocks aren't supported."))
                continue
            }
            if inMatch { continue }

            if let reason = skipReasons[lower] {
                skipped.append(.init(line: line, keyword: keyword, reason: reason))
                continue
            }
            if let arg = firstArgument(value) {
                blocks[blocks.count - 1].settings.append((lower, arg))
            }
        }

        var budget = matchBudget
        var resolved: [String: Resolved] = [:]
        for alias in aliases {
            resolved[alias] = resolve(alias, blocks: blocks, budget: &budget)
        }

        var byName: [String: (alias: String, item: Resolved)] = [:]
        for alias in aliases where byName[alias.lowercased()] == nil {
            byName[alias.lowercased()] = (alias, resolved[alias] ?? Resolved())
        }

        let hosts = aliases.map { alias -> ImportedHost in
            let item = resolved[alias] ?? Resolved()
            let jump = item.jump.map { makeJump($0, byName: byName) }
            return ImportedHost(alias: alias, hostName: item.hostName ?? alias, user: item.user, port: item.port, jump: jump)
        }
        if budget <= 0 {
            skipped.append(.init(line: 0, keyword: "Host", reason: "Some patterns were too complex to match."))
        }
        return ImportResult(hosts: hosts, skipped: skipped)
    }

    private static let skipReasons: [String: String] = [
        "include": "Include isn't supported; paste the included file too.",
        "identityfile": "Keys stay on this iPhone; pick one after import.",
        "proxycommand": "ProxyCommand isn't supported; use ProxyJump.",
    ]

    private struct Pattern {
        var negated: Bool
        var folded: String
        var chars: [Character]
        var wild: Bool

        var matchesAll: Bool { chars.count == 1 && chars[0] == "*" }

        init(_ text: String) {
            negated = text.hasPrefix("!")
            folded = (negated ? String(text.dropFirst()) : text).lowercased()
            chars = Array(folded)
            wild = chars.contains { $0 == "*" || $0 == "?" }
        }
    }

    private struct Name {
        var folded: String
    }

    private struct Block {
        var patterns: [Pattern]
        /// Whether every pattern is a glob, so the block cannot apply once the budget is spent.
        var onlyGlobs: Bool
        var settings: [(key: String, value: String)] = []

        init(patterns: [String]) {
            self.patterns = patterns.map(Pattern.init)
            onlyGlobs = !self.patterns.isEmpty && self.patterns.allSatisfy { $0.wild && !$0.matchesAll }
        }
    }

    private struct Resolved {
        var hostName: String?
        var user: String?
        var port: Int?
        var jump: String?
        var jumpSet = false
    }

    private static func resolve(_ alias: String, blocks: [Block], budget: inout Int) -> Resolved {
        var result = Resolved()
        let name = Name(folded: alias.lowercased())
        for index in blocks.indices {
            if budget <= 0, blocks[index].onlyGlobs { continue }
            guard applies(blocks[index].patterns, to: name, budget: &budget) else { continue }
            let block = blocks[index]
            for (key, value) in block.settings {
                switch key {
                case "hostname":
                    if result.hostName == nil, let name = expand(value, alias: alias) { result.hostName = name }
                case "user":
                    if result.user == nil, !value.isEmpty { result.user = value }
                case "port":
                    if result.port == nil, let port = validPort(value) { result.port = port }
                case "proxyjump":
                    let first = value.split(separator: ",", maxSplits: 1, omittingEmptySubsequences: false)
                        .first.map { $0.trimmingCharacters(in: .whitespaces) } ?? ""
                    guard !result.jumpSet, let hop = expand(first, alias: alias), !hop.isEmpty else { break }
                    result.jumpSet = true
                    result.jump = hop.lowercased() == "none" ? nil : hop
                default:
                    break
                }
            }
        }
        return result
    }

    private static func makeJump(_ hop: String, byName: [String: (alias: String, item: Resolved)]) -> ImportedJump {
        var rest = hop
        if rest.lowercased().hasPrefix("ssh://") { rest = String(rest.dropFirst(6)) }
        var user: String?
        if let at = rest.lastIndex(of: "@") {
            let name = String(rest[..<at])
            user = name.isEmpty ? nil : name
            rest = String(rest[rest.index(after: at)...])
        }
        let (host, port) = splitPort(rest)
        if let target = byName[host.lowercased()] {
            return ImportedJump(host: target.item.hostName ?? target.alias, user: user ?? target.item.user,
                                port: port ?? target.item.port, alias: target.alias)
        }
        return ImportedJump(host: host, user: user, port: port, alias: nil)
    }

    /// Splits a trailing `:port` off a host, honouring `[v6]:port` and leaving bare IPv6 alone.
    private static func splitPort(_ text: String) -> (host: String, port: Int?) {
        if text.hasPrefix("["), let close = text.firstIndex(of: "]") {
            let host = String(text[text.index(after: text.startIndex)..<close])
            let tail = text[text.index(after: close)...]
            guard tail.hasPrefix(":") else { return (host, nil) }
            return (host, validPort(String(tail.dropFirst())))
        }
        guard text.filter({ $0 == ":" }).count == 1, let colon = text.lastIndex(of: ":"),
              let port = validPort(String(text[text.index(after: colon)...])) else { return (text, nil) }
        return (String(text[..<colon]), port)
    }

    /// Substitutes `%h` and `%%`; any other `%` token makes the value unusable, so it is dropped.
    private static func expand(_ value: String, alias: String) -> String? {
        guard value.contains("%") else { return value.isEmpty ? nil : value }
        var out = ""
        var iterator = value.makeIterator()
        while let char = iterator.next() {
            guard char == "%" else { out.append(char); continue }
            switch iterator.next() {
            case "h": out += alias
            case "%": out.append("%")
            default: return nil
            }
        }
        return out.isEmpty ? nil : out
    }

    private static func validPort(_ text: String) -> Int? {
        guard !text.isEmpty, text.allSatisfy(\.isASCIIDigit), let port = Int(text), (1...65535).contains(port) else {
            return nil
        }
        return port
    }

    private static func isLiteral(_ pattern: String) -> Bool {
        !pattern.contains { $0 == "*" || $0 == "?" || $0 == "!" }
    }

    private static func applies(_ patterns: [Pattern], to text: Name, budget: inout Int) -> Bool {
        var positive = false
        for pattern in patterns {
            if pattern.negated {
                if matches(pattern, text, budget: &budget) { return false }
            } else if matches(pattern, text, budget: &budget) {
                positive = true
            }
        }
        return positive
    }

    private static func matches(_ pattern: Pattern, _ name: Name, budget: inout Int) -> Bool {
        let p = pattern.chars
        if !pattern.wild { return pattern.folded == name.folded }
        if pattern.matchesAll { return true }
        guard budget > 0 else { return false }
        let t = Array(name.folded)
        var pi = 0, ti = 0
        var star = -1, retry = 0
        while ti < t.count {
            budget -= 1
            if budget <= 0 { return false }
            if pi < p.count, p[pi] == "*" {
                star = pi
                retry = ti
                pi += 1
            } else if pi < p.count, p[pi] == "?" || p[pi] == t[ti] {
                pi += 1
                ti += 1
            } else if star >= 0 {
                retry += 1
                ti = retry
                pi = star + 1
            } else {
                return false
            }
        }
        while pi < p.count, p[pi] == "*" { pi += 1 }
        return pi == p.count
    }

    /// Splits a line into keyword and value; nil for blanks, comments and keywords without a value.
    private static func splitLine(_ raw: String) -> (String, String)? {
        let trimmed = raw.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty, !trimmed.hasPrefix("#") else { return nil }

        let keyEnd = trimmed.firstIndex { $0 == " " || $0 == "\t" || $0 == "=" } ?? trimmed.endIndex
        let keyword = String(trimmed[..<keyEnd])
        var rest = trimmed[keyEnd...].drop { $0 == " " || $0 == "\t" }
        if rest.first == "=" { rest = rest.dropFirst().drop { $0 == " " || $0 == "\t" } }
        guard !keyword.isEmpty else { return nil }
        return (keyword, String(rest))
    }

    /// Splits on LF at the scalar level, since CR LF is a single Character, and drops a trailing CR.
    private static func physicalLines(_ text: String) -> [String] {
        text.unicodeScalars.split(separator: "\n", omittingEmptySubsequences: false).map { scalars in
            var view = scalars
            if view.last == "\r" { view = view.dropLast() }
            return String(String.UnicodeScalarView(view))
        }
    }

    /// The first argument of a value, ignoring a trailing comment; nil when empty or malformed.
    private static func firstArgument(_ value: String) -> String? {
        guard let first = tokens(value)?.first, !first.isEmpty else { return nil }
        return first
    }

    /// Whitespace-separated tokens where double quotes group words and are removed.
    /// Stops at an unquoted `#` comment; nil when a quote is left open.
    private static func tokens(_ text: String) -> [String]? {
        var result: [String] = []
        var current = ""
        var started = false
        var quoted = false
        for char in text {
            if char == "\"" {
                quoted.toggle()
                started = true
            } else if !quoted, char == "#", !started {
                break
            } else if !quoted, char == " " || char == "\t" {
                if started { result.append(current) }
                current = ""
                started = false
            } else {
                current.append(char)
                started = true
            }
        }
        if quoted { return nil }
        if started { result.append(current) }
        return result
    }
}

private extension Character {
    var isASCIIDigit: Bool { self >= "0" && self <= "9" }
}
