import Foundation

public extension Language {
    /// Picks a language from the file name, then from a `#!` first line.
    /// Returns `.plain` when neither says anything.
    static func detect(path: String, firstLine: String?) -> Language {
        let name = lastComponent(of: path)
        if let known = byName[name.lowercased()] { return known }
        if let dot = name.lastIndex(of: "."), dot != name.startIndex {
            let ext = name[name.index(after: dot)...].lowercased()
            if let known = byExtension[ext] { return known }
        }
        if let firstLine, let known = fromShebang(firstLine) { return known }
        return .plain
    }

    private static func lastComponent(of path: String) -> String {
        guard let slash = path.lastIndex(of: "/") else { return path }
        return String(path[path.index(after: slash)...])
    }

    private static func fromShebang(_ line: String) -> Language? {
        guard line.hasPrefix("#!") else { return nil }
        let words = line.dropFirst(2).split(whereSeparator: { $0 == " " || $0 == "\t" })
        guard var program = words.first.map({ lastComponent(of: String($0)) }) else { return nil }
        if program == "env" {
            guard let next = words.dropFirst().first(where: { !$0.hasPrefix("-") && !$0.contains("=") }) else {
                return nil
            }
            program = lastComponent(of: String(next))
        }
        return byInterpreter[stripVersion(program.lowercased())]
    }

    /// `python3.11` -> `python`.
    private static func stripVersion(_ name: String) -> String {
        var end = name.endIndex
        while end > name.startIndex {
            let prev = name.index(before: end)
            guard name[prev].isNumber || name[prev] == "." else { break }
            end = prev
        }
        return end == name.startIndex ? name : String(name[..<end])
    }

    private static let byName: [String: Language] = [
        "makefile": .shell, "gnumakefile": .shell, "dockerfile": .shell,
        ".bashrc": .shell, ".zshrc": .shell, ".profile": .shell, ".bash_profile": .shell,
        ".zprofile": .shell, ".envrc": .shell,
        "gemfile": .ruby, "rakefile": .ruby, "podfile": .ruby,
    ]

    private static let byExtension: [String: Language] = [
        "swift": .swift, "go": .go,
        "ts": .typescript, "tsx": .typescript, "mts": .typescript, "cts": .typescript,
        "js": .javascript, "jsx": .javascript, "mjs": .javascript, "cjs": .javascript,
        "py": .python, "pyi": .python, "rs": .rust,
        "rb": .ruby, "rake": .ruby, "gemspec": .ruby,
        "sh": .shell, "bash": .shell, "zsh": .shell, "ksh": .shell, "mk": .shell,
        "json": .json, "jsonc": .json, "yml": .yaml, "yaml": .yaml, "toml": .toml,
        "md": .markdown, "markdown": .markdown,
        "html": .html, "htm": .html, "xhtml": .html, "css": .css,
        "c": .c, "h": .c, "cc": .cpp, "cpp": .cpp, "cxx": .cpp, "hpp": .cpp, "hh": .cpp, "hxx": .cpp,
        "m": .objc, "mm": .objc, "java": .java, "kt": .kotlin, "kts": .kotlin, "sql": .sql,
    ]

    private static let byInterpreter: [String: Language] = [
        "python": .python, "bash": .shell, "sh": .shell, "zsh": .shell, "dash": .shell,
        "ksh": .shell, "ash": .shell, "node": .javascript, "nodejs": .javascript,
        "deno": .typescript, "bun": .javascript, "tsx": .typescript, "ts-node": .typescript,
        "ruby": .ruby,
    ]
}
