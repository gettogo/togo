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
    /// What to run on a machine installed with Homebrew.
    public static let brewCommand = "brew upgrade togo"

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
    public mutating func record(_ check: HelperUpdateCheck?, for id: UUID, at now: Date) {
        var state = machines[id] ?? HelperUpdateState(checkedAt: now)
        state.checkedAt = now
        switch check {
        case .available(let latest, let installed):
            state.latest = latest
            state.installed = installed
            state.homebrew = false
        case .upToDate(let installed):
            state.latest = nil
            state.installed = installed
            state.homebrew = false
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
