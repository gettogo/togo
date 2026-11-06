import Foundation

/// Speaks new terminal output once it settles: after a quiet spell, no more often than
/// `minGap`, and at most `maxCharacters`. Text that has waited `maxWait` is spoken even
/// without quiet, so a row that never stops changing cannot hold everything back. Silent
/// unless `isActive`, which is false until the owner wires it.
///
/// Two ways in:
/// - `waiting(_:)`, used by `OutputFeed`: the full list of text not yet heard, replacing what
///   waited before. The feed decides what is new; only text not already waiting restarts
///   the quiet spell.
/// - `output(newLines:)`, for a caller that only has a stream of rows: they queue, and a row
///   that repeats one waiting or in the previous announcement (spinner churn) is dropped.
///
/// Waiting text is bounded: only the newest rows, up to twice `maxCharacters`, are kept, and
/// an announcement longer than `maxCharacters` keeps its end, where the newest text is,
/// starting at a word.
@MainActor
public final class OutputAnnouncer {
    public var announce: (String) -> Void = { _ in }
    /// Called after each announcement, so a feed can mark the screen as heard.
    public var didAnnounce: () -> Void = {}
    /// Wired by the owner: VoiceOver running, terminal focused, setting on.
    public var isActive: () -> Bool = { false }

    private let quiet: Duration
    private let minGap: Duration
    private let maxWait: Duration
    private let maxCharacters: Int
    private let clock: any Clock<Duration>

    private var pending: [String] = []
    private var pendingSet: Set<String> = []
    private var pendingCharacters = 0
    private var lastAnnounced: Set<String> = []
    /// Time since the waiting text first arrived; nil while nothing waits.
    private var waited: (() -> Duration)?
    private var quietElapsed = false
    private var gapOpen = true
    private var quietTask: Task<Void, Never>?
    private var gapTask: Task<Void, Never>?

    public init(
        quiet: Duration = .milliseconds(1500),
        minGap: Duration = .seconds(3),
        maxWait: Duration = .seconds(6),
        maxCharacters: Int = 200,
        clock: any Clock<Duration>
    ) {
        self.quiet = quiet
        self.minGap = minGap
        self.maxWait = maxWait
        self.maxCharacters = maxCharacters
        self.clock = clock
    }

    /// Rows waiting to be spoken; bounded by twice `maxCharacters` of text.
    var pendingCount: Int { pending.count }

    deinit {
        quietTask?.cancel()
        gapTask?.cancel()
    }

    public func output(newLines: [String]) {
        guard isActive() else { return }
        var accepted = false
        for raw in newLines {
            let line = trimmed(raw)
            // A row that only differs by spinner glyphs cleans to text already heard.
            guard !line.isEmpty, !lastAnnounced.contains(line), !pendingSet.contains(line) else { continue }
            pending.append(line)
            pendingSet.insert(line)
            pendingCharacters += line.count + 1
            while pending.count > 1, pendingCharacters > 2 * maxCharacters {
                let dropped = pending.removeFirst()
                pendingSet.remove(dropped)
                pendingCharacters -= dropped.count + 1
            }
            accepted = true
        }
        guard accepted else { return }
        restartQuiet()
    }

    /// Replaces the waiting text with `lines`. A row that grows in place replaces its shorter
    /// self instead of queueing beside it; text that leaves the list is no longer waited for.
    public func waiting(_ lines: [String]) {
        guard isActive() else { return }
        var kept: [String] = []
        var characters = 0
        for raw in lines.reversed() {
            let line = trimmed(raw)
            guard !line.isEmpty else { continue }
            if !kept.isEmpty, characters + line.count + 1 > 2 * maxCharacters { break }
            kept.append(line)
            characters += line.count + 1
        }
        kept.reverse()
        guard kept != pending else { return }
        let arrived = kept.contains { !pendingSet.contains($0) }
        pending = kept
        pendingSet = Set(kept)
        pendingCharacters = characters
        if kept.isEmpty {
            quietTask?.cancel()
            waited = nil
        } else if arrived {
            restartQuiet()
        }
    }

    /// Cleaned, and cut to its last `maxCharacters` characters (whole graphemes).
    private func trimmed(_ raw: String) -> String {
        let line = TerminalAccessibility.clean(raw)
        return line.count > maxCharacters ? String(line.suffix(maxCharacters)) : line
    }

    private func restartQuiet() {
        if waited == nil { waited = stopwatch(clock) }
        let left = maxWait - (waited?() ?? .zero)
        let sleep = min(quiet, max(left, .zero))
        quietElapsed = false
        quietTask?.cancel()
        quietTask = Task { [weak self, clock] in
            guard (try? await clock.sleep(for: sleep)) != nil else { return }
            self?.quietPassed()
        }
    }

    private func quietPassed() {
        quietElapsed = true
        flush()
    }

    private func gapPassed() {
        gapOpen = true
        flush()
    }

    private func flush() {
        guard quietElapsed, gapOpen, !pending.isEmpty else { return }
        let lines = pending
        pending = []
        pendingSet = []
        pendingCharacters = 0
        waited = nil
        guard isActive() else { return }
        lastAnnounced = Set(lines)
        gapOpen = false
        gapTask?.cancel()
        gapTask = Task { [weak self, clock, minGap] in
            guard (try? await clock.sleep(for: minGap)) != nil else { return }
            self?.gapPassed()
        }
        announce(spoken(lines))
        didAnnounce()
    }

    /// The newest `maxCharacters` of the text. When the cut lands inside a word, the rest of
    /// that word goes, so speech starts on a whole word.
    private func spoken(_ lines: [String]) -> String {
        let joined = lines.joined(separator: " ")
        guard joined.count > maxCharacters else { return joined }
        let tail = joined.suffix(maxCharacters)
        let cutInWord = joined.dropLast(maxCharacters).last.map { !$0.isWhitespace } ?? false
        guard cutInWord, let space = tail.prefix(30).firstIndex(of: " ") else { return String(tail) }
        return String(tail[tail.index(after: space)...])
    }
}

/// Reads how long has passed since it was made, on any clock measured in `Duration`.
func stopwatch(_ clock: any Clock<Duration>) -> () -> Duration {
    func started<C: Clock<Duration>>(_ clock: C) -> () -> Duration {
        let start = clock.now
        return { start.duration(to: clock.now) }
    }
    return started(clock)
}
