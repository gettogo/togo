import Foundation

/// What `togo update --check` said about a machine's helper.
public enum HelperUpdateCheck: Sendable, Equatable {
    /// A newer release exists. Both versions are as the helper printed them, such as "v1.0.1".
    case available(latest: String, installed: String)
    case upToDate(installed: String)
    /// The helper was installed with Homebrew, which owns its updates.
    case homebrew
}

/// How a `togo update` run ended.
public enum HelperUpdateOutcome: Sendable, Equatable {
    /// The new binary is in place; `version` is nil when the helper did not name it.
    case updated(version: String?)
    case upToDate
    case homebrew
    case failed(String)
}

/// What the app last learned about one machine's helper release, kept on this device.
public struct HelperUpdateState: Codable, Sendable, Equatable {
    public var checkedAt: Date
    /// The release that is available, or nil when the helper is current or could not say.
    public var latest: String?
    /// The version the helper reported at the check.
    public var installed: String?
    public var homebrew: Bool

    public init(checkedAt: Date, latest: String? = nil, installed: String? = nil, homebrew: Bool = false) {
        self.checkedAt = checkedAt
        self.latest = latest
        self.installed = installed
        self.homebrew = homebrew
    }
}

/// Checking a machine's helper for a newer release, and updating it, by running the helper's own
/// `update` command on the machine. The machine contacts the release server; the phone never does.
public enum HelperUpdate {
    /// How long a Homebrew step may take: `brew update` and `brew upgrade` fetch and build.
    public static let brewTimeout = Duration.seconds(300)

    /// Where Homebrew puts its binary when the helper's own path does not say, in order.
    static let defaultBrewPaths = ["/opt/homebrew/bin/brew", "/usr/local/bin/brew"]

    /// The exec command for `togo update --check`.
    public static func checkCommand(helperPath: String?) -> String {
        command(helperPath: helperPath, arguments: ["update", "--check"])
    }

    /// The exec command for `togo update`.
    public static func updateCommand(helperPath: String?) -> String {
        command(helperPath: helperPath, arguments: ["update"])
    }

    /// The saved helper path, quoted word by word; without one, the places `HelperCommand.serveStdio`
    /// tries. `arguments` are fixed words, never user input.
    static func command(helperPath: String?, arguments: [String]) -> String {
        if let helperPath { return shellQuote([helperPath] + arguments) }
        let words = arguments.joined(separator: " ")
        return shellCommand(
            #"for t in "$HOME/.togo/bin/togo" /opt/homebrew/bin/togo /usr/local/bin/togo; do "#
                + #"if [ -x "$t" ]; then exec "$t" "# + words + #"; fi; done; exec togo "# + words
        )
    }

    /// The `brew` that owns a helper at `<prefix>/Cellar/togo/<version>/bin/togo`, else nil.
    public static func brewPath(helperPath: String?) -> String? {
        guard let helperPath, let range = helperPath.range(of: "/Cellar/togo/") else { return nil }
        let prefix = helperPath[..<range.lowerBound]
        return prefix.isEmpty ? nil : prefix + "/bin/brew"
    }

    /// The exec command that asks Homebrew whether `togo` is outdated, for a machine whose helper
    /// refused `update --check`. Prints `brew outdated --json=v2 togo`; read it with `parseBrewOutdated`.
    public static func brewCheckCommand(helperPath: String?) -> String {
        brewScript(
            helperPath: helperPath,
            steps: #""$b" update --quiet >/dev/null 2>&1; exec "$b" outdated --json=v2 togo"#)
    }

    /// The exec command that upgrades `togo` with Homebrew, asks the daemon to restart once idle,
    /// and prints the installed version last. Read it with `parseBrewUpgrade`.
    public static func brewUpgradeCommand(helperPath: String?) -> String {
        brewScript(
            helperPath: helperPath,
            steps: #""$b" upgrade togo || exit $?; p=$("$b" --prefix) || exit $?; "#
                + #""$p/bin/togo" daemon restart --when-idle >/dev/null 2>&1; "$p/bin/togo" version; exit 0"#,
            extraEnvironment: ["HOMEBREW_NO_INSTALL_CLEANUP=1"])
    }

    /// One `/bin/sh` script: finds the first `brew` that exists, then runs `steps` with `$b` set.
    /// `steps` are fixed words; the only outside text, the helper path, goes through `shellQuote`.
    private static func brewScript(helperPath: String?, steps: String, extraEnvironment: [String] = []) -> String {
        let candidates = brewPath(helperPath: helperPath).map { [$0] } ?? defaultBrewPaths
        let environment = (["NONINTERACTIVE=1", "HOMEBREW_NO_ENV_HINTS=1"] + extraEnvironment).joined(separator: " ")
        return shellCommand(
            "export \(environment); for b in \(shellQuote(candidates)); do if [ -x \"$b\" ]; then \(steps); fi; done; "
                + #"echo "togo: Homebrew was not found on this machine" >&2; exit 127"#)
    }

    /// Reads `brew outdated --json=v2 togo`: an empty list means current. The up-to-date case has
    /// no version to report, so its `installed` is empty. Nil for anything that is not that JSON.
    public static func parseBrewOutdated(stdout: String) -> HelperUpdateCheck? {
        struct Report: Decodable {
            struct Formula: Decodable {
                let installedVersions: [String]
                let currentVersion: String
            }
            let formulae: [Formula]
        }
        guard let start = stdout.firstIndex(of: "{") else { return nil }
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        guard let report = try? decoder.decode(Report.self, from: Data(stdout[start...].utf8)) else { return nil }
        guard let formula = report.formulae.first else { return .upToDate(installed: "") }
        guard !formula.currentVersion.isEmpty else { return nil }
        return .available(latest: withV(formula.currentVersion), installed: formula.installedVersions.last.map(withV) ?? "")
    }

    /// Reads the output of `brewUpgradeCommand`: the last `togo <version>` line is the version now installed.
    public static func parseBrewUpgrade(stdout: String, stderr: String, status: Int32) -> HelperUpdateOutcome {
        guard status == 0 else { return .failed(errorText(stderr: stderr, stdout: stdout, status: status)) }
        var version: String?
        for line in stdout.split(whereSeparator: \.isNewline) {
            let words = line.split(separator: " ").map(String.init)
            if words.count >= 2, words[0] == "togo", isVersion(words[1]) { version = words[1] }
        }
        return .updated(version: version)
    }

    private static func withV(_ version: String) -> String { version.hasPrefix("v") ? version : "v" + version }

    /// Whether a path is plainly inside a Homebrew prefix. A bare `/usr/local/bin/togo` could be
    /// either, so the helper's own refusal decides for that.
    public static func isHomebrewPath(_ path: String?) -> Bool {
        guard let path else { return false }
        return path.contains("/Cellar/") || path.hasPrefix("/opt/homebrew/") || path.contains("/linuxbrew/")
            || path.contains("/.linuxbrew/")
    }

    /// Reads the output of `update --check`. Nil for anything unrecognised, such as a helper too old
    /// to know `--check`, which prints a usage error.
    public static func parseCheck(stdout: String, stderr: String, status: Int32) -> HelperUpdateCheck? {
        if isHomebrewRefusal(stderr) || isHomebrewRefusal(stdout) { return .homebrew }
        guard status == 0 else { return nil }
        for line in stdout.split(whereSeparator: \.isNewline) {
            let words = line.split(separator: " ").map(String.init)
            guard words.count == 6, words[0] == "togo", isVersion(words[1]) else { continue }
            // "togo v1.0.1 is available (installed: v1.0.0)"
            if words[2] == "is", words[3] == "available", words[4] == "(installed:", words[5].hasSuffix(")") {
                let installed = String(words[5].dropLast())
                if isVersion(installed) { return .available(latest: words[1], installed: installed) }
            }
            // "togo v1.0.0 is up to date"
            if Array(words[2...]) == ["is", "up", "to", "date"] { return .upToDate(installed: words[1]) }
        }
        return nil
    }

    /// Reads how `update` ended.
    public static func parseUpdate(stdout: String, stderr: String, status: Int32) -> HelperUpdateOutcome {
        if isHomebrewRefusal(stderr) || isHomebrewRefusal(stdout) { return .homebrew }
        guard status == 0 else { return .failed(errorText(stderr: stderr, stdout: stdout, status: status)) }
        for line in stdout.split(whereSeparator: \.isNewline) {
            let words = line.split(separator: " ").map(String.init)
            // "Installed togo v1.0.1 at /path (previous binary kept at /path.previous)"
            if words.count >= 3, words[0] == "Installed", words[1] == "togo" {
                return .updated(version: isVersion(words[2]) ? words[2] : nil)
            }
            if words.count == 6, words[0] == "togo", Array(words[2...]) == ["is", "up", "to", "date"] {
                return .upToDate
            }
        }
        return .updated(version: nil)
    }

    /// Whether a check is due: never done, a day or more ago, or dated in the future.
    public static func isDue(lastChecked: Date?, now: Date) -> Bool {
        guard let lastChecked else { return true }
        let elapsed = now.timeIntervalSince(lastChecked)
        return elapsed < 0 || elapsed >= 24 * 60 * 60
    }

    /// Whether `latest` is a later release than `installed`; both may carry a leading "v".
    public static func isNewer(_ latest: String, than installed: String) -> Bool {
        let a = numbers(latest), b = numbers(installed)
        guard !a.isEmpty, !b.isEmpty else { return false }
        for index in 0..<max(a.count, b.count) {
            let x = index < a.count ? a[index] : 0, y = index < b.count ? b[index] : 0
            if x != y { return x > y }
        }
        return false
    }

    /// A version for display: "v1.0.1" reads "1.0.1".
    public static func display(_ version: String) -> String {
        version.hasPrefix("v") ? String(version.dropFirst()) : version
    }

    // MARK: Parsing helpers

    /// Whether the helper said Homebrew owns its updates (a helper before 1.0.2).
    public static func isHomebrewRefusal(stdout: String, stderr: String) -> Bool {
        isHomebrewRefusal(stderr) || isHomebrewRefusal(stdout)
    }

    private static func isHomebrewRefusal(_ text: String) -> Bool {
        text.contains("Installed with Homebrew")
    }

    private static func isVersion(_ word: String) -> Bool {
        let digits = word.hasPrefix("v") ? word.dropFirst() : Substring(word)
        return digits.first?.isNumber == true && digits.allSatisfy { $0.isNumber || $0 == "." || $0 == "-" || $0.isLetter }
    }

    private static func numbers(_ version: String) -> [Int] {
        let core = display(version).split(whereSeparator: { $0 == "-" || $0 == "+" }).first ?? ""
        let parts = core.split(separator: ".").map { Int($0) }
        return parts.contains(where: { $0 == nil }) ? [] : parts.compactMap { $0 }
    }

    /// The helper prints "togo: message" on one line; the message is what a person needs.
    private static func errorText(stderr: String, stdout: String, status: Int32) -> String {
        let source = stderr.isEmpty ? stdout : stderr
        let line = source.split(whereSeparator: \.isNewline)
            .map { $0.trimmingCharacters(in: .whitespaces) }.last { !$0.isEmpty }
        guard var text = line else { return "The update failed (exit \(status))." }
        if text.hasPrefix("togo: ") { text = String(text.dropFirst(6)) }
        return text
    }
}

/// The per-machine results, as saved on the device.
public struct HelperUpdateLedger: Codable, Sendable, Equatable {
    public var machines: [UUID: HelperUpdateState]

    public init(machines: [UUID: HelperUpdateState] = [:]) {
        self.machines = machines
    }

    /// Records a check. A check that could not be read keeps what was known and only moves the
    /// time, so it is tried again tomorrow.
    /// `viaHomebrew` marks an answer the app got from Homebrew because the helper could not give one.
    public mutating func record(_ check: HelperUpdateCheck?, for id: UUID, at now: Date, viaHomebrew: Bool = false) {
        var state = machines[id] ?? HelperUpdateState(checkedAt: now)
        state.checkedAt = now
        switch check {
        case .available(let latest, let installed):
            state.latest = latest
            if !installed.isEmpty { state.installed = installed }
            state.homebrew = viaHomebrew
        case .upToDate(let installed):
            state.latest = nil
            if !installed.isEmpty { state.installed = installed }
            state.homebrew = viaHomebrew
        case .homebrew:
            state.latest = nil
            state.homebrew = true
        case nil:
            break
        }
        machines[id] = state
    }

    /// Clears the available release after an update.
    public mutating func updated(_ id: UUID, to version: String?, at now: Date) {
        var state = machines[id] ?? HelperUpdateState(checkedAt: now)
        state.checkedAt = now
        state.installed = version ?? state.latest ?? state.installed
        state.latest = nil
        machines[id] = state
    }

    public mutating func forget(_ id: UUID) {
        machines[id] = nil
    }

    public func isDue(_ id: UUID, now: Date) -> Bool {
        HelperUpdate.isDue(lastChecked: machines[id]?.checkedAt, now: now)
    }

    /// The release to offer, only while it is later than the version the helper reports now.
    public func available(for id: UUID, current: String?) -> String? {
        guard let latest = machines[id]?.latest else { return nil }
        if let current, !HelperUpdate.isNewer(latest, than: current) { return nil }
        return latest
    }
}
