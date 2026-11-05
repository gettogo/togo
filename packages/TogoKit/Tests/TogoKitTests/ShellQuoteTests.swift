import Foundation
import Testing
import TogoKit

struct ShellQuoteTests {
    static let hostile = ["it's", "", "a b", "$(x)", "a\nb", "émoji 🐕", ";", "`id`", "\"q\"", "back\\slash", "*", "\\'", "tab\there", "x\\", "\\\\'"]

    /// A `'` followed by a combining mark or joiner forms one grapheme cluster with it, so a
    /// Character-level replace misses the quote and the shell sees it unescaped.
    static let graphemeHostile = [
        "'\u{301}; echo INJECTED #",
        "'\u{301}",
        "a'\u{301}\u{302}b",
        "'\u{200D}🐕; echo INJECTED #",
        "👨\u{200D}👩\u{200D}👧'; echo INJECTED #",
        "🐕'\u{FE0F}$(echo INJECTED)",
    ]

    @Test func shellQuoteHostile() throws {
        #expect(shellQuote(["it's"]) == #"'it'\''s'"#)
        #expect(shellQuote(["\\"]) == #"\\"#)
        #expect(shellQuote(["a\\'b"]) == #"'a'\\\''b'"#)
        #expect(shellQuote([""]) == "''")
        #expect(shellQuote(["a b", "$(x)", "a\nb", "émoji 🐕", ";"]) == "'a b' '$(x)' 'a\nb' 'émoji 🐕' ';'")

        let argv = Self.hostile + Self.graphemeHostile
        let quoted = shellQuote(argv)
        #expect(try run("/bin/sh", script: #"printf '%s\0' "# + quoted) == argv.map { Data($0.utf8) })
    }

    @Test(arguments: ["/bin/sh", "/bin/bash", "/bin/zsh"])
    func shellQuoteRoundTripsThroughShell(shell: String) throws {
        try #require(FileManager.default.isExecutableFile(atPath: shell))
        let argv = Self.hostile + Self.graphemeHostile
        #expect(try run(shell, script: #"printf '%s\0' "# + shellQuote(argv)) == argv.map { Data($0.utf8) })
    }

    @Test func shellQuoteEscapesEveryQuoteAndBackslash() {
        for element in Self.graphemeHostile + ["a\\b", "\\'"] {
            let quoted = Array(shellQuote([element]).unicodeScalars)
            // No single-quoted run holds a backslash or a quote.
            var inRun = false
            var escaped = false
            for scalar in quoted {
                if escaped { escaped = false; continue }
                if !inRun, scalar == "\\" { escaped = true; continue }
                if scalar == "'" { inRun.toggle(); continue }
                if inRun { #expect(scalar != "\\", "\(element.debugDescription)") }
            }
        }
    }

    @Test func shellQuoteHostileThroughFish() throws {
        guard let fish = Self.executableOnPath("fish") else {
            print("shellQuoteHostileThroughFish skipped: fish is not on PATH")
            return
        }
        let argv = Self.hostile + Self.graphemeHostile
        #expect(try run(fish, script: #"printf '%s\0' "# + shellQuote(argv)) == argv.map { Data($0.utf8) })
    }

    @Test func shellCommandWrapsScript() {
        #expect(shellCommand("echo 'x'") == #"/bin/sh -c 'echo '\''x'\'"#)
    }

    @Test func shellCommandRunsScriptUnderSh() throws {
        let script = #"printf '%s\0' 'it'\''s' "a  b" $((1 + 1))"#
        #expect(try run("/bin/sh", script: shellCommand(script)) == ["it's", "a  b", "2"].map { Data($0.utf8) })
    }

    /// Returns the NUL-separated fields as raw bytes: comparing Strings would treat
    /// canonically equivalent but byte-different output as equal.
    private func run(_ shell: String, script: String) throws -> [Data] {
        let process = Process()
        let output = Pipe()
        process.executableURL = URL(filePath: shell)
        // The script goes in on stdin: Process converts arguments to NFD, which would
        // rewrite precomposed characters before the shell ever saw them.
        let input = Pipe()
        process.standardInput = input
        process.standardOutput = output
        try process.run()
        input.fileHandleForWriting.write(Data(script.utf8))
        try input.fileHandleForWriting.close()
        let data = output.fileHandleForReading.readDataToEndOfFile()
        process.waitUntilExit()
        #expect(process.terminationStatus == 0)
        var fields = data.split(separator: 0, omittingEmptySubsequences: false).map { Data($0) }
        if fields.last == Data() { fields.removeLast() }
        return fields
    }

    private static func executableOnPath(_ name: String) -> String? {
        let path = ProcessInfo.processInfo.environment["PATH"] ?? ""
        return path.split(separator: ":")
            .map { "\($0)/\(name)" }
            .first { FileManager.default.isExecutableFile(atPath: $0) }
    }
}
